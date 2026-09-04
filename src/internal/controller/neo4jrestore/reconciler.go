/*
Copyright Neo4j.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package neo4jrestore reconciles Neo4jRestore records by seeding databases from a URI over
// Bolt (ADR-015). Restore is not a Job: the DBMS reads the seed, so the operator only drives
// CREATE/REPLACE DATABASE … OPTIONS {seedURI} against the target's system database and polls
// SHOW DATABASES until each database is online.
package neo4jrestore

import (
	"context"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/formation"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderbackup "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/backup"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
)

// seedProviderSchemes are the URI schemes a Neo4j server can read as a seed. pvc:// is
// deliberately absent — a PVC-backed artifact must first be exposed at a file: path on the
// servers (RWX backups volume), which is a later increment (ReasonRestoreSourceUnsupported).
var seedProviderSchemes = []string{"file:", "server:", "s3:", "gs:", "azb:", "http:", "https:", "ftp:"}

// RestoreReconciler drives a Neo4jRestore to Succeeded/Failed over Bolt.
type RestoreReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	// Connect builds an admin Bolt session to the target; nil → formation.Dial (tests inject a fake).
	Connect func(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) (intneo4j.Admin, error)
}

func NewReconciler(mgr ctrl.Manager) *RestoreReconciler {
	return &RestoreReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("neo4jrestore-controller"),
	}
}

// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jrestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jrestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jbackups,verbs=get;list;watch
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4js,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

func (r *RestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var restore neo4jv1beta1.Neo4jRestore
	if err := r.Get(ctx, req.NamespacedName, &restore); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Immutable record: a terminal run never re-seeds (GitOps re-apply safe).
	if restore.Status.Phase == neo4jv1beta1.RunPhaseSucceeded || restore.Status.Phase == neo4jv1beta1.RunPhaseFailed {
		return ctrl.Result{}, nil
	}

	var neo4j neo4jv1beta1.Neo4j
	if err := r.Get(ctx, types.NamespacedName{Name: restore.Spec.Neo4jRef.Name, Namespace: restore.Namespace}, &neo4j); err != nil {
		if apierrors.IsNotFound(err) {
			return r.retryable(ctx, &restore, oracle.ReasonRestoreTargetNotFound,
				"waiting for Neo4j "+restore.Spec.Neo4jRef.Name+" to exist")
		}
		return ctrl.Result{}, err
	}

	if neo4j.Spec.Edition != neo4jv1beta1.EditionEnterprise {
		return r.fail(ctx, &restore, oracle.ReasonRestoreEditionUnsupported,
			"restore requires Enterprise edition; target is "+string(neo4j.Spec.Edition))
	}

	// Formation gate (ADR-015 §3): never seed over an incomplete server set. Ready is the
	// conservative signal that works for both standalone and cluster.
	// ponytail: ADR-015 names ClusterFormed specifically; Ready is a safe superset here and
	// avoids blocking standalone (which need not publish ClusterFormed). Upgrade to a
	// topology-aware gate if Ready proves too strict for quorum-only restores.
	if !meta.IsStatusConditionTrue(neo4j.Status.Conditions, oracle.ConditionReady.String()) {
		return r.retryable(ctx, &restore, oracle.ReasonRestoreBeforeFormation,
			"target Neo4j is not Ready yet; waiting before seeding")
	}

	connect := r.Connect
	if connect == nil {
		connect = func(ctx context.Context, n *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return formation.Dial(ctx, r.Client, r.Recorder, n)
		}
	}

	// Already seeding — only poll for online, never re-issue.
	if restore.Status.Phase == neo4jv1beta1.RunPhaseRunning {
		admin, err := connect(ctx, &neo4j)
		if err != nil {
			return r.retryable(ctx, &restore, oracle.ReasonRestoreBoltUnavailable, err.Error())
		}
		defer func() { _ = admin.Close(ctx) }()
		return r.poll(ctx, &restore, &neo4j, admin)
	}

	// Resolve the per-database seed URIs. With source.aggregate, a pre-seed Job first collapses
	// the chain into a recovered full and we seed from that; otherwise we seed the recorded
	// artifacts directly (chain-seed). Aggregate is a Job, so it runs before we need Bolt.
	var seedFor map[string]string
	if restore.Spec.Source.Aggregate {
		seeds, ready, res, err := r.ensureAggregate(ctx, &restore, &neo4j)
		if !ready {
			return res, err
		}
		seedFor = seeds
	} else {
		seeds, reason, msg := r.resolveSeeds(ctx, &restore, &neo4j)
		if reason != nil {
			return r.fail(ctx, &restore, *reason, msg)
		}
		seedFor = seeds
	}

	admin, err := connect(ctx, &neo4j)
	if err != nil {
		return r.retryable(ctx, &restore, oracle.ReasonRestoreBoltUnavailable, err.Error())
	}
	defer func() { _ = admin.Close(ctx) }()
	return r.issueSeeds(ctx, &restore, &neo4j, admin, seedFor)
}

// issueSeeds runs the existence/overwrite/forceOffline gate and issues one seed-from-URI
// statement per requested database, then flips the record to Running for poll to finish.
func (r *RestoreReconciler) issueSeeds(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, neo4j *neo4jv1beta1.Neo4j, admin intneo4j.Admin, seedFor map[string]string) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithName("neo4jrestore")

	states, err := admin.ShowDatabases(ctx)
	if err != nil {
		return r.retryable(ctx, restore, oracle.ReasonRestoreBoltUnavailable, "SHOW DATABASES: "+err.Error())
	}
	exists := map[string]bool{}
	for _, s := range states {
		exists[s.Name] = true
	}

	primaries, secondaries := topology(neo4j)
	dbStatuses := make([]neo4jv1beta1.RestoreDatabaseStatus, 0, len(restore.Spec.Databases))
	for _, db := range restore.Spec.Databases {
		seedURI := seedFor[db]
		switch {
		case exists[db] && !restore.Spec.Overwrite:
			return r.fail(ctx, restore, oracle.ReasonRestoreDatabaseExists,
				"database "+db+" already exists and overwrite is false")
		case exists[db]:
			if restore.Spec.ForceOffline {
				if err := admin.StopDatabase(ctx, db); err != nil {
					return r.seedErr(ctx, restore, db, err)
				}
			}
			if err := admin.CreateOrReplaceDatabaseWithSeed(ctx, db, seedURI, primaries, secondaries); err != nil {
				return r.seedErr(ctx, restore, db, err)
			}
		default:
			if err := admin.CreateDatabaseWithSeed(ctx, db, seedURI, primaries, secondaries); err != nil {
				return r.seedErr(ctx, restore, db, err)
			}
		}
		dbStatuses = append(dbStatuses, neo4jv1beta1.RestoreDatabaseStatus{
			Name: db, Phase: neo4jv1beta1.RunPhaseRunning, Message: "seeding from " + seedURI,
		})
	}

	log.Info("seed statements issued", "databases", len(dbStatuses))
	restore.Status.Databases = dbStatuses
	restore.Status.Phase = neo4jv1beta1.RunPhaseRunning
	restore.Status.Reason = ""
	restore.Status.Message = ""
	setCondition(restore, oracle.ConditionRestoreReady, metav1.ConditionFalse, oracle.ReasonRestoreInProgress,
		"seeding databases; waiting for them to come online")
	if err := r.writeStatus(ctx, restore); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// poll reads SHOW DATABASES and declares Succeeded once every requested database is online. When
// spec.restoreMetadata is set it first drives a post-seed metadata Job to a terminal state (the
// databases are already online; only users/roles/privileges remain to reapply).
func (r *RestoreReconciler) poll(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, neo4j *neo4jv1beta1.Neo4j, admin intneo4j.Admin) (ctrl.Result, error) {
	states, err := admin.ShowDatabases(ctx)
	if err != nil {
		return r.retryable(ctx, restore, oracle.ReasonRestoreBoltUnavailable, "SHOW DATABASES: "+err.Error())
	}
	online := map[string]bool{}
	for _, s := range states {
		online[s.Name] = s.Online
	}
	allOnline := true
	dbStatuses := make([]neo4jv1beta1.RestoreDatabaseStatus, 0, len(restore.Spec.Databases))
	for _, db := range restore.Spec.Databases {
		phase := neo4jv1beta1.RunPhaseRunning
		msg := "seeding"
		if online[db] {
			phase = neo4jv1beta1.RunPhaseSucceeded
			msg = "online"
		} else {
			allOnline = false
		}
		dbStatuses = append(dbStatuses, neo4jv1beta1.RestoreDatabaseStatus{Name: db, Phase: phase, Message: msg})
	}
	restore.Status.Databases = dbStatuses
	if !allOnline {
		setCondition(restore, oracle.ConditionRestoreReady, metav1.ConditionFalse, oracle.ReasonRestoreInProgress,
			"waiting for databases to come online")
		if err := r.writeStatus(ctx, restore); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	// Databases are online. Reapply users/roles/privileges before declaring success if asked —
	// the metadata Job runs to a terminal state (done=false means it is still running or failed
	// the record terminally, and res/err are authoritative).
	if restore.Spec.RestoreMetadata {
		if done, res, err := r.ensureMetadata(ctx, restore, neo4j); !done {
			return res, err
		}
	}

	restore.Status.Phase = neo4jv1beta1.RunPhaseSucceeded
	restore.Status.Reason = ""
	restore.Status.Message = ""
	setCondition(restore, oracle.ConditionRestoreReady, metav1.ConditionTrue, oracle.ReasonRestoreSucceeded, "all databases online")
	return ctrl.Result{}, r.writeStatus(ctx, restore)
}

// ensureMetadata drives the post-seed metadata Job (spec.restoreMetadata) to a terminal state.
// done=true means metadata was applied (possibly with skipped conflicts, surfaced as a Warning) and
// the caller may declare success. done=false means the caller must return res/err as-is: the Job is
// still running (requeued) or the record was failed terminally. Supported only for a PVC-backed
// source.backupRef; other sources fail with RestoreMetadataFailed.
func (r *RestoreReconciler) ensureMetadata(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, neo4j *neo4jv1beta1.Neo4j) (done bool, res ctrl.Result, err error) {
	if restore.Spec.Source.BackupRef == "" {
		res, err = r.fail(ctx, restore, oracle.ReasonRestoreMetadataFailed,
			"spec.restoreMetadata requires source.backupRef (a PVC-backed backup); a raw source.url carries no metadata script")
		return false, res, err
	}
	var backup neo4jv1beta1.Neo4jBackup
	if e := r.Get(ctx, types.NamespacedName{Name: restore.Spec.Source.BackupRef, Namespace: restore.Namespace}, &backup); e != nil {
		if apierrors.IsNotFound(e) {
			res, err = r.fail(ctx, restore, oracle.ReasonRestoreMetadataFailed, "source.backupRef "+restore.Spec.Source.BackupRef+" not found")
			return false, res, err
		}
		return false, ctrl.Result{}, e
	}

	claim, dbArtifacts, reason, msg := metadataInputs(restore, &backup)
	if reason != nil {
		res, err = r.fail(ctx, restore, *reason, msg)
		return false, res, err
	}

	job, e := renderbackup.MetadataJob(neo4j, renderbackup.MetadataJobName(restore), claim, dbArtifacts)
	if e != nil {
		res, err = r.fail(ctx, restore, oracle.ReasonRestoreMetadataFailed, e.Error())
		return false, res, err
	}
	if e := shared.Apply(ctx, r.Client, r.Scheme, restore, job, func() error { return nil }); e != nil {
		return false, ctrl.Result{}, e
	}

	var owned batchv1.Job
	if e := r.Get(ctx, types.NamespacedName{Name: renderbackup.MetadataJobName(restore), Namespace: restore.Namespace}, &owned); e != nil {
		if apierrors.IsNotFound(e) {
			res, err = r.metaProgress(ctx, restore, "metadata Job starting")
			return false, res, err
		}
		return false, ctrl.Result{}, e
	}
	switch complete, failed, jmsg := shared.JobTerminal(&owned); {
	case failed:
		detail := jmsg
		if podMsg := shared.JobPodTerminationMessage(ctx, r.Client, owned.Namespace, owned.Name); podMsg != "" {
			detail = podMsg
		}
		res, err = r.fail(ctx, restore, oracle.ReasonRestoreMetadataFailed, detail)
		return false, res, err
	case complete:
		podMsg := shared.JobPodTerminationMessage(ctx, r.Client, owned.Namespace, owned.Name)
		if strings.Contains(podMsg, "with-warnings") && r.Recorder != nil {
			r.Recorder.Event(restore, corev1.EventTypeWarning, oracle.ReasonRestoreMetadataConflict.String(), metaDetail(podMsg))
		}
		return true, ctrl.Result{}, nil
	default:
		res, err = r.metaProgress(ctx, restore, "metadata Job running")
		return false, res, err
	}
}

// metadataInputs resolves the backups claim and each database's recorded artifact path (the chain's
// last link) for the metadata Job. Supported only for PVC-backed backups on one claim.
func metadataInputs(restore *neo4jv1beta1.Neo4jRestore, backup *neo4jv1beta1.Neo4jBackup) (claim string, dbArtifacts map[string]string, reason *oracle.Reason, msg string) {
	failR := oracle.ReasonRestoreMetadataFailed
	dbArtifacts = map[string]string{}
	for _, db := range restore.Spec.Databases {
		a, ok := artifactFor(backup, db)
		if !ok {
			return "", nil, &failR, "backupRef " + restore.Spec.Source.BackupRef + " has no artifact for database " + db
		}
		if !strings.HasPrefix(a.URI, "pvc://") {
			return "", nil, &failR, "metadata restore supports only PVC-backed backups; artifact for " + db + " is " + a.URI
		}
		if a.Path == "" {
			return "", nil, &failR, "backup recorded no artifact filename for " + db + "; cannot restore metadata"
		}
		c := strings.TrimPrefix(a.URI, "pvc://")
		if claim == "" {
			claim = c
		} else if claim != c {
			return "", nil, &failR, "metadata restore requires all databases on the same backup claim"
		}
		dbArtifacts[db] = a.Path
	}
	return claim, dbArtifacts, nil, ""
}

// metaProgress records the in-progress metadata condition and requeues without changing phase.
func (r *RestoreReconciler) metaProgress(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, msg string) (ctrl.Result, error) {
	restore.Status.Reason = oracle.ReasonRestoreMetadataApplying.String()
	restore.Status.Message = msg
	setCondition(restore, oracle.ConditionRestoreReady, metav1.ConditionFalse, oracle.ReasonRestoreMetadataApplying, msg)
	if err := r.writeStatus(ctx, restore); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// metaDetail trims the Job's termination message for an Event (keep it readable).
func metaDetail(msg string) string {
	msg = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(msg), "metadata-applied-with-warnings"))
	if len(msg) > 500 {
		msg = msg[:500]
	}
	if msg == "" {
		return "metadata apply skipped one or more statements (already exists)"
	}
	return msg
}

// resolveSeeds maps each requested database to a seedURI (ADR-015 §2 / BDR-014 §13). On a
// terminal problem it returns a catalogued reason + message and a nil map.
func (r *RestoreReconciler) resolveSeeds(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, neo4j *neo4jv1beta1.Neo4j) (map[string]string, *oracle.Reason, string) {
	for _, db := range restore.Spec.Databases {
		if db == "*" {
			reason := oracle.ReasonRestoreSourceUnsupported
			return nil, &reason, "wildcard '*' is not yet supported; list databases explicitly"
		}
	}
	src := restore.Spec.Source

	// Raw url: a single artifact, so it maps to exactly one database.
	if src.URL != "" {
		if len(restore.Spec.Databases) != 1 {
			reason := oracle.ReasonRestoreSourceUnsupported
			return nil, &reason, "source.url addresses a single artifact; list exactly one database"
		}
		if reason, msg := validateSeedURI(src.URL); reason != nil {
			return nil, reason, msg
		}
		return map[string]string{restore.Spec.Databases[0]: src.URL}, nil, ""
	}

	// backupRef: resolve the succeeded Neo4jBackup and take its recorded artifact URIs.
	var backup neo4jv1beta1.Neo4jBackup
	if err := r.Get(ctx, types.NamespacedName{Name: src.BackupRef, Namespace: restore.Namespace}, &backup); err != nil {
		reason := oracle.ReasonRestoreSourceNotFound
		if apierrors.IsNotFound(err) {
			return nil, &reason, "source.backupRef " + src.BackupRef + " not found"
		}
		return nil, &reason, "reading backupRef: " + err.Error()
	}
	if backup.Status.Phase != neo4jv1beta1.RunPhaseSucceeded {
		reason := oracle.ReasonRestoreSourceNotFound
		return nil, &reason, "backupRef " + src.BackupRef + " has not Succeeded (phase " + string(backup.Status.Phase) + ")"
	}
	seedFor := map[string]string{}
	for _, db := range restore.Spec.Databases {
		a, ok := artifactFor(&backup, db)
		if !ok {
			reason := oracle.ReasonRestoreSourceNotFound
			return nil, &reason, "backupRef " + src.BackupRef + " has no artifact for database " + db
		}
		seedURI, reason, msg := seedURIFromArtifact(neo4j, a)
		if reason != nil {
			return nil, reason, "database " + db + ": " + msg
		}
		seedFor[db] = seedURI
	}
	return seedFor, nil, ""
}

// ensureAggregate drives the pre-seed aggregate Job (source.aggregate) to completion and returns
// the seed URIs for the recovered full artifacts. ready=false means the caller should return res
// (and err) as-is: the Job is still running (requeued) or the record was failed terminally. When
// ready=true, seedFor maps each database to file:<backupsMountPath>/<recovered> for issueSeeds.
func (r *RestoreReconciler) ensureAggregate(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, neo4j *neo4jv1beta1.Neo4j) (seedFor map[string]string, ready bool, res ctrl.Result, err error) {
	src := restore.Spec.Source
	if src.BackupRef == "" {
		res, err = r.fail(ctx, restore, oracle.ReasonRestoreSourceUnsupported, "source.aggregate requires source.backupRef (the Job needs the recorded PVC artifacts)")
		return nil, false, res, err
	}
	for _, db := range restore.Spec.Databases {
		if db == "*" {
			res, err = r.fail(ctx, restore, oracle.ReasonRestoreSourceUnsupported, "source.aggregate does not support wildcard '*'; list databases explicitly")
			return nil, false, res, err
		}
	}

	var backup neo4jv1beta1.Neo4jBackup
	if e := r.Get(ctx, types.NamespacedName{Name: src.BackupRef, Namespace: restore.Namespace}, &backup); e != nil {
		if apierrors.IsNotFound(e) {
			res, err = r.fail(ctx, restore, oracle.ReasonRestoreSourceNotFound, "source.backupRef "+src.BackupRef+" not found")
			return nil, false, res, err
		}
		return nil, false, ctrl.Result{}, e
	}
	if backup.Status.Phase != neo4jv1beta1.RunPhaseSucceeded {
		res, err = r.fail(ctx, restore, oracle.ReasonRestoreSourceNotFound, "backupRef "+src.BackupRef+" has not Succeeded (phase "+string(backup.Status.Phase)+")")
		return nil, false, res, err
	}

	claim, dbArtifacts, reason, msg := aggregateInputs(restore, &backup, neo4j)
	if reason != nil {
		res, err = r.fail(ctx, restore, *reason, msg)
		return nil, false, res, err
	}

	job, e := renderbackup.AggregateJob(neo4j, renderbackup.AggregateJobName(restore), claim, dbArtifacts)
	if e != nil {
		res, err = r.fail(ctx, restore, oracle.ReasonRestoreSourceUnsupported, e.Error())
		return nil, false, res, err
	}
	if e := shared.Apply(ctx, r.Client, r.Scheme, restore, job, func() error { return nil }); e != nil {
		return nil, false, ctrl.Result{}, e
	}

	var owned batchv1.Job
	if e := r.Get(ctx, types.NamespacedName{Name: renderbackup.AggregateJobName(restore), Namespace: restore.Namespace}, &owned); e != nil {
		if apierrors.IsNotFound(e) {
			res, err = r.retryable(ctx, restore, oracle.ReasonRestoreAggregating, "aggregate Job starting")
			return nil, false, res, err
		}
		return nil, false, ctrl.Result{}, e
	}
	switch complete, failed, jmsg := shared.JobTerminal(&owned); {
	case failed:
		detail := jmsg
		if podMsg := shared.JobPodTerminationMessage(ctx, r.Client, owned.Namespace, owned.Name); podMsg != "" {
			detail = podMsg
		}
		res, err = r.fail(ctx, restore, oracle.ReasonRestoreAggregateFailed, detail)
		return nil, false, res, err
	case complete:
		names := shared.ParseNamedArtifacts(shared.JobPodTerminationMessage(ctx, r.Client, owned.Namespace, owned.Name))
		seedFor = map[string]string{}
		for _, db := range restore.Spec.Databases {
			n := names[db].Name
			if n == "" {
				res, err = r.fail(ctx, restore, oracle.ReasonRestoreAggregateFailed, "aggregate produced no recovered artifact for database "+db)
				return nil, false, res, err
			}
			seed := "file:" + backupsMountPath + "/" + n
			if rsn, m := validateSeedURI(seed); rsn != nil {
				res, err = r.fail(ctx, restore, *rsn, m)
				return nil, false, res, err
			}
			seedFor[db] = seed
		}
		return seedFor, true, ctrl.Result{}, nil
	default:
		res, err = r.retryable(ctx, restore, oracle.ReasonRestoreAggregating, "aggregate Job running")
		return nil, false, res, err
	}
}

// aggregateInputs validates the referenced backup is PVC-backed and mounted on the target, and
// returns the claim plus each database's latest recorded artifact (the chain's last link) for the
// aggregate Job. All databases must live on the one backups claim the target mounts.
func aggregateInputs(restore *neo4jv1beta1.Neo4jRestore, backup *neo4jv1beta1.Neo4jBackup, neo4j *neo4jv1beta1.Neo4j) (claim string, dbArtifacts map[string]string, reason *oracle.Reason, msg string) {
	unsupported := oracle.ReasonRestoreSourceUnsupported
	notFound := oracle.ReasonRestoreSourceNotFound
	dbArtifacts = map[string]string{}
	for _, db := range restore.Spec.Databases {
		a, ok := artifactFor(backup, db)
		if !ok {
			return "", nil, &notFound, "backupRef " + restore.Spec.Source.BackupRef + " has no artifact for database " + db
		}
		if !strings.HasPrefix(a.URI, "pvc://") {
			return "", nil, &unsupported, "source.aggregate supports only PVC-backed backups; artifact for " + db + " is " + a.URI
		}
		c := strings.TrimPrefix(a.URI, "pvc://")
		if a.Path == "" {
			return "", nil, &unsupported, "backup recorded no artifact filename for " + db + "; cannot aggregate"
		}
		if !mountsBackupsClaim(neo4j, c) {
			return "", nil, &unsupported, "target does not mount backup PVC " + c + " as storage.volumes.backups (Existing); aggregate needs filesystem access at " + backupsMountPath
		}
		if claim == "" {
			claim = c
		} else if claim != c {
			return "", nil, &unsupported, "source.aggregate requires all databases on the same backup claim"
		}
		dbArtifacts[db] = a.Path
	}
	return claim, dbArtifacts, nil, ""
}

// backupsMountPath is where the workload mounts the storage.volumes.backups volume. Single-sourced
// from render/storage so the seed URI restore builds (file:<backupsMountPath>/<artifact>) always
// matches where the servers actually read the claim — a divergence here breaks the round-trip.
const backupsMountPath = storage.BackupsMountPath

// artifactFor finds the recorded artifact for a database (an exact match, or a "*" artifact
// that stands for all databases).
func artifactFor(backup *neo4jv1beta1.Neo4jBackup, db string) (*neo4jv1beta1.BackupArtifact, bool) {
	for i := range backup.Status.Artifacts {
		if a := &backup.Status.Artifacts[i]; a.Database == db || a.Database == "*" {
			return a, true
		}
	}
	return nil, false
}

// seedURIFromArtifact turns a recorded backup artifact into a seedURI the servers can read.
// A PVC-backed artifact becomes file:/backups/<path>, valid only when the target mounts that
// exact claim as its storage.volumes.backups (Existing) volume — that is what puts the artifact
// on the servers' filesystem (ADR-015 round-trip). Object-store URIs pass through unchanged.
func seedURIFromArtifact(neo4j *neo4jv1beta1.Neo4j, a *neo4jv1beta1.BackupArtifact) (string, *oracle.Reason, string) {
	if strings.HasPrefix(a.URI, "pvc://") {
		claim := strings.TrimPrefix(a.URI, "pvc://")
		if a.Path == "" {
			reason := oracle.ReasonRestoreSourceUnsupported
			return "", &reason, "backup recorded no seedable artifact path (wildcard backup, or the Job could not record the artifact filename); restore from an explicit named-database backup"
		}
		if !mountsBackupsClaim(neo4j, claim) {
			reason := oracle.ReasonRestoreSourceUnsupported
			return "", &reason, "target does not mount backup PVC " + claim + " as storage.volumes.backups (Existing); a PVC-backed seed must be readable by the servers at " + backupsMountPath
		}
		seed := "file:" + backupsMountPath + "/" + a.Path
		if reason, msg := validateSeedURI(seed); reason != nil {
			return "", reason, msg
		}
		return seed, nil, ""
	}
	if reason, msg := validateSeedURI(a.URI); reason != nil {
		return "", reason, msg
	}
	return a.URI, nil, ""
}

// mountsBackupsClaim is true when the target mounts claim as its storage.volumes.backups volume.
func mountsBackupsClaim(neo4j *neo4jv1beta1.Neo4j, claim string) bool {
	s := neo4j.Spec.Storage
	if s == nil || s.Volumes == nil || s.Volumes.Backups == nil {
		return false
	}
	b := s.Volumes.Backups
	return b.Mode == neo4jv1beta1.VolumeModeExisting && b.Existing != nil && b.Existing.ClaimName == claim
}

// validateSeedURI rejects schemes a Neo4j server cannot read as a seed (notably pvc://) and any
// value that could break the Cypher OPTIONS literal.
func validateSeedURI(uri string) (*oracle.Reason, string) {
	if err := intneo4j.ValidateSeedURI(uri); err != nil {
		reason := oracle.ReasonRestoreSourceUnsupported
		return &reason, err.Error()
	}
	for _, s := range seedProviderSchemes {
		if strings.HasPrefix(uri, s) {
			return nil, ""
		}
	}
	reason := oracle.ReasonRestoreSourceUnsupported
	return &reason, "seed source " + uri + " is not a readable seed URI (need file:/server:/s3:/gs:/azb:); a PVC-backed backup needs the RWX backups volume path (not yet wired)"
}

// topology returns the CREATE DATABASE TOPOLOGY counts; (0,0) for standalone omits the clause.
func topology(neo4j *neo4jv1beta1.Neo4j) (primaries, secondaries int64) {
	if neo4j.Spec.Topology.Mode != neo4jv1beta1.TopologyModeCluster {
		return 0, 0
	}
	ctx := render.ClientServiceContext(neo4j)
	return int64(ctx.DefaultPrimariesCount()), int64(ctx.DefaultSecondariesCount())
}

// seedErr classifies a Bolt statement failure as retryable (leader moved, etc.) or terminal.
func (r *RestoreReconciler) seedErr(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, db string, err error) (ctrl.Result, error) {
	if isRetryableBolt(err) {
		return r.retryable(ctx, restore, oracle.ReasonRestoreBoltUnavailable, "database "+db+": "+err.Error())
	}
	return r.fail(ctx, restore, oracle.ReasonRestoreSeedFailed, "database "+db+": "+err.Error())
}

func isRetryableBolt(err error) bool {
	s := err.Error()
	return strings.Contains(s, "NotALeader") ||
		strings.Contains(s, "bolt connect") ||
		strings.Contains(s, "TransactionExecutionLimit") ||
		strings.Contains(s, "ForbiddenOnReadOnlyDatabase") ||
		strings.Contains(s, "Unable to reallocate") ||
		strings.Contains(s, "Required topology")
}

func (r *RestoreReconciler) fail(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, reason oracle.Reason, msg string) (ctrl.Result, error) {
	restore.Status.Phase = neo4jv1beta1.RunPhaseFailed
	restore.Status.Reason = reason.String()
	restore.Status.Message = msg
	setCondition(restore, oracle.ConditionRestoreReady, metav1.ConditionFalse, reason, msg)
	if r.Recorder != nil {
		r.Recorder.Event(restore, corev1.EventTypeWarning, reason.String(), msg)
	}
	return ctrl.Result{}, r.writeStatus(ctx, restore)
}

func (r *RestoreReconciler) retryable(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, reason oracle.Reason, msg string) (ctrl.Result, error) {
	if restore.Status.Phase == "" {
		restore.Status.Phase = neo4jv1beta1.RunPhasePending
	}
	restore.Status.Reason = reason.String()
	restore.Status.Message = msg
	setCondition(restore, oracle.ConditionRestoreReady, metav1.ConditionFalse, reason, msg)
	if err := r.writeStatus(ctx, restore); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *RestoreReconciler) writeStatus(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) error {
	restore.Status.ObservedGeneration = restore.Generation
	if err := r.Status().Update(ctx, restore); err != nil {
		if apierrors.IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}

func setCondition(restore *neo4jv1beta1.Neo4jRestore, ctype oracle.Condition, status metav1.ConditionStatus, reason oracle.Reason, message string) {
	meta.SetStatusCondition(&restore.Status.Conditions, metav1.Condition{
		Type:               ctype.String(),
		Status:             status,
		Reason:             reason.String(),
		Message:            message,
		ObservedGeneration: restore.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (r *RestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jRestore{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
