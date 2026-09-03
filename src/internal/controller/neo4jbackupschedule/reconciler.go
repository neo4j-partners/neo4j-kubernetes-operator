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

// Package neo4jbackupschedule reconciles a Neo4jBackupSchedule into emitted Neo4jBackup objects
// on independent full/incremental cron cadences (BDR-014 §10). It is the CronJob→Job mapping,
// doubled: each full anchors a new chain (status.currentChain), incrementals attach to it. Emitted
// backups are named by their scheduled minute so a re-reconcile is idempotent (AlreadyExists →
// skip). It also enforces full.retention by pruning whole expired chains (PVC artifacts via an
// owned Job, then the records — BDR-014 §10). incremental.retention (aggregate compaction) and
// object-store pruning (ADR-016) are later increments.
package neo4jbackupschedule

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/robfig/cron/v3"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderbackup "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/backup"
)

// Labels stamped on emitted Neo4jBackup objects so restore points are discoverable (BDR-014 §13)
// and a later retention increment can select a schedule's backups and group them by chain.
const (
	LabelSchedule   = "neo4j.com/schedule"
	LabelChain      = "neo4j.com/chain"
	LabelBackupType = "neo4j.com/type"
	LabelDatabase   = "neo4j.com/database"
)

// ScheduleReconciler emits Neo4jBackup objects on cron cadences.
type ScheduleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	// Now is injected by tests; nil → time.Now.
	Now func() time.Time
}

func NewReconciler(mgr ctrl.Manager) *ScheduleReconciler {
	return &ScheduleReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("neo4jbackupschedule-controller"),
	}
}

// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jbackupschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jbackupschedules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4js,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

func (r *ScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithName("neo4jbackupschedule")

	var sched neo4jv1beta1.Neo4jBackupSchedule
	if err := r.Get(ctx, req.NamespacedName, &sched); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Suspend short-circuits before any target work; emission resumes when it is cleared.
	if sched.Spec.Suspend {
		sched.Status.Suspended = true
		r.setCondition(&sched, metav1.ConditionFalse, oracle.ReasonScheduleSuspended, "schedule is suspended (spec.suspend=true)")
		return ctrl.Result{}, r.writeStatus(ctx, &sched)
	}
	sched.Status.Suspended = false

	// Parse crons up front — a bad expression is terminal until the spec is fixed (no requeue;
	// the spec-change watch re-triggers).
	fullSched, err := cron.ParseStandard(sched.Spec.Full.Schedule)
	if err != nil {
		return r.fail(ctx, &sched, oracle.ReasonScheduleInvalidCron, "full schedule: "+err.Error())
	}
	var incSched cron.Schedule
	if sched.Spec.Incremental != nil {
		if incSched, err = cron.ParseStandard(sched.Spec.Incremental.Schedule); err != nil {
			return r.fail(ctx, &sched, oracle.ReasonScheduleInvalidCron, "incremental schedule: "+err.Error())
		}
	}

	var neo4j neo4jv1beta1.Neo4j
	if err := r.Get(ctx, types.NamespacedName{Name: sched.Spec.Neo4jRef.Name, Namespace: sched.Namespace}, &neo4j); err != nil {
		if apierrors.IsNotFound(err) {
			return r.retryable(ctx, &sched, oracle.ReasonScheduleTargetNotFound,
				"waiting for Neo4j "+sched.Spec.Neo4jRef.Name+" to exist")
		}
		return ctrl.Result{}, err
	}
	if neo4j.Spec.Edition != neo4jv1beta1.EditionEnterprise {
		return r.fail(ctx, &sched, oracle.ReasonScheduleEditionUnsupported,
			"backup requires Enterprise edition; target is "+string(neo4j.Spec.Edition))
	}

	now := r.now()

	// FULL cadence — a full anchors a new chain.
	if fire, scheduled := due(fullSched, baseTime(sched.Status.LastFullTime, sched.CreationTimestamp), now); fire {
		chain := chainID(&sched, scheduled)
		name, err := r.emit(ctx, &sched, &neo4j, neo4jv1beta1.BackupTypeFull, chain, scheduled)
		if err != nil {
			return ctrl.Result{}, err
		}
		log.Info("emitted full backup", "backup", name, "chain", chain)
		t := metav1.NewTime(scheduled)
		sched.Status.LastFullTime = &t
		sched.Status.CurrentChain = chain
		sched.Status.LastBackup = name
	}

	// INCREMENTAL cadence — attaches to the current chain. A differential can only be taken once
	// the chain's full has *succeeded*: emitting one earlier makes neo4j-admin fail ("no full
	// backup found"), which the Job then burns retries on — leaving Error pods behind that trip
	// pod-failure alerts. So we (a) skip the tick that coincides with the chain's own full (the
	// full already covers that instant, and a differential right after it would be empty), and
	// (b) otherwise hold the differential until the full is Succeeded, retrying on the next tick
	// or when the owned full's status change re-triggers us. LastIncrementalTime is only advanced
	// once we actually emit or deliberately skip, so a held tick is re-evaluated, never lost.
	if incSched != nil && sched.Status.CurrentChain != "" {
		if fire, scheduled := due(incSched, baseTime(sched.Status.LastIncrementalTime, sched.CreationTimestamp), now); fire {
			switch {
			case chainID(&sched, scheduled) == sched.Status.CurrentChain:
				t := metav1.NewTime(scheduled)
				sched.Status.LastIncrementalTime = &t
			default:
				ready, err := r.chainFullReady(ctx, sched.Namespace, sched.Status.CurrentChain)
				if err != nil {
					return ctrl.Result{}, err
				}
				if ready {
					name, err := r.emit(ctx, &sched, &neo4j, neo4jv1beta1.BackupTypeIncremental, sched.Status.CurrentChain, scheduled)
					if err != nil {
						return ctrl.Result{}, err
					}
					log.Info("emitted incremental backup", "backup", name, "chain", sched.Status.CurrentChain)
					t := metav1.NewTime(scheduled)
					sched.Status.LastIncrementalTime = &t
					sched.Status.LastBackup = name
				}
			}
		}
	}

	r.setCondition(&sched, metav1.ConditionTrue, oracle.ReasonScheduleActive, "schedule active")
	if err := r.writeStatus(ctx, &sched); err != nil {
		return ctrl.Result{}, err
	}

	// Retention: prune whole expired chains (full.retention). Drains one chain per reconcile and
	// returns a short requeue while a prune Job is in flight.
	pruneRequeue, err := r.pruneExpiredChains(ctx, &sched, &neo4j)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Requeue at the earliest of the next cron tick or a pending prune step, so neither emission
	// nor pruning depends on the resync period.
	next := fullSched.Next(now)
	if incSched != nil && sched.Status.CurrentChain != "" {
		if n := incSched.Next(now); n.Before(next) {
			next = n
		}
	}
	requeue := requeueDelay(next, now)
	if pruneRequeue > 0 && pruneRequeue < requeue {
		requeue = pruneRequeue
	}
	return ctrl.Result{RequeueAfter: requeue}, nil
}

// emit creates one owned Neo4jBackup for a cadence tick, named by the scheduled minute so a
// re-reconcile of the same tick is a no-op (AlreadyExists → skip). Returns the backup name.
func (r *ScheduleReconciler) emit(ctx context.Context, sched *neo4jv1beta1.Neo4jBackupSchedule, neo4j *neo4jv1beta1.Neo4j, typ neo4jv1beta1.BackupType, chain string, scheduled time.Time) (string, error) {
	tmpl := sched.Spec.BackupTemplate
	name := backupName(sched, typ, scheduled)
	b := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sched.Namespace,
			Labels:    emitLabels(sched, typ, chain, tmpl.Databases),
		},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Neo4jRef:    sched.Spec.Neo4jRef,
			Databases:   tmpl.Databases,
			Destination: tmpl.Destination,
			Type:        typ,
			Options:     tmpl.Options,
		},
	}
	if err := controllerutil.SetControllerReference(sched, b, r.Scheme); err != nil {
		return "", err
	}
	if err := r.Create(ctx, b); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return name, nil
		}
		return "", err
	}
	if r.Recorder != nil {
		r.Recorder.Event(sched, corev1.EventTypeNormal, oracle.ReasonScheduleBackupEmitted.String(),
			fmt.Sprintf("emitted %s backup %s (chain %s)", typ, name, chain))
	}
	return name, nil
}

// chainFullReady reports whether a chain's anchoring full backup exists and has Succeeded. A
// differential emitted before that makes neo4j-admin fail ("no full backup found in <dir>"), so
// the schedule holds incrementals until it is true. The full's object name is deterministic —
// <chain>-f — so this is a single Get, not a list.
func (r *ScheduleReconciler) chainFullReady(ctx context.Context, ns, chain string) (bool, error) {
	var full neo4jv1beta1.Neo4jBackup
	switch err := r.Get(ctx, types.NamespacedName{Name: chain + "-f", Namespace: ns}, &full); {
	case apierrors.IsNotFound(err):
		return false, nil
	case err != nil:
		return false, err
	default:
		return full.Status.Phase == neo4jv1beta1.RunPhaseSucceeded, nil
	}
}

func emitLabels(sched *neo4jv1beta1.Neo4jBackupSchedule, typ neo4jv1beta1.BackupType, chain string, dbs []string) map[string]string {
	l := map[string]string{
		render.LabelManagedBy: render.ManagedByValue,
		LabelSchedule:         sched.Name,
		LabelChain:            chain,
		LabelBackupType:       string(typ),
	}
	// A single-database backup gets the discoverability label (BDR-014 §13); "*" or a multi-db
	// list has no single value to put here.
	if len(dbs) == 1 && dbs[0] != "*" {
		l[LabelDatabase] = dbs[0]
	}
	return l
}

// due reports whether a cadence owes a run — its next activation after base has already passed —
// and the most recent such activation time. Missed ticks collapse to one (we never backfill a
// burst); the returned time stamps the emitted backup's name so re-reconciles stay idempotent.
func due(sched cron.Schedule, base, now time.Time) (bool, time.Time) {
	next := sched.Next(base)
	if next.After(now) {
		return false, time.Time{}
	}
	scheduled := next
	for t := sched.Next(next); !t.After(now); t = sched.Next(t) {
		scheduled = t
	}
	return true, scheduled
}

// baseTime is the point a cadence measures its next tick from: the last emission, or the
// schedule's creation for a cold start.
func baseTime(last *metav1.Time, creation metav1.Time) time.Time {
	if last != nil {
		return last.Time
	}
	return creation.Time
}

// requeueDelay floors the wait at 1s so a tick due "now" doesn't hot-loop.
func requeueDelay(next, now time.Time) time.Duration {
	if d := next.Sub(now); d > time.Second {
		return d
	}
	return time.Second
}

// chainID names the chain a full anchors: <schedule>-<UTC minute>. Incrementals reuse it.
func chainID(sched *neo4jv1beta1.Neo4jBackupSchedule, scheduled time.Time) string {
	return sched.Name + "-" + scheduled.UTC().Format("20060102-1504")
}

// backupName is the deterministic per-tick object name (idempotency key). Minute granularity
// matches cron's finest resolution.
func backupName(sched *neo4jv1beta1.Neo4jBackupSchedule, typ neo4jv1beta1.BackupType, scheduled time.Time) string {
	suffix := "f"
	if typ == neo4jv1beta1.BackupTypeIncremental {
		suffix = "i"
	}
	return sched.Name + "-" + scheduled.UTC().Format("20060102-1504") + "-" + suffix
}

// pruneExpiredChains enforces full.retention by removing whole expired chains (BDR-014 §10): a
// chain's PVC artifacts (via an owned prune Job) and then its Neo4jBackup records. It drains the
// oldest expired chain per reconcile (retention is not latency-sensitive) and returns a requeue
// delay > 0 while a prune Job is in flight or more chains remain. incremental.retention is realized
// by the aggregate cadence, not here — individual mid-chain links are never deleted.
func (r *ScheduleReconciler) pruneExpiredChains(ctx context.Context, sched *neo4jv1beta1.Neo4jBackupSchedule, neo4j *neo4jv1beta1.Neo4j) (time.Duration, error) {
	if sched.Spec.Full.Retention == nil {
		return 0, nil
	}

	var backups neo4jv1beta1.Neo4jBackupList
	if err := r.List(ctx, &backups, client.InNamespace(sched.Namespace), client.MatchingLabels{LabelSchedule: sched.Name}); err != nil {
		return 0, err
	}

	chains := groupChains(backups.Items)
	expired := expiredChains(chains, sched.Spec.Full.Retention, sched.Status.CurrentChain, r.now())
	if len(expired) == 0 {
		return 0, nil
	}

	// Drain the oldest expired chain first; later reconciles take the next.
	chain := expired[0]
	items := chains[chain]

	// Never prune a chain that still has a run in flight — wait for it to finish.
	for _, b := range items {
		if b.Status.Phase != neo4jv1beta1.RunPhaseSucceeded && b.Status.Phase != neo4jv1beta1.RunPhaseFailed {
			return 0, nil
		}
	}

	claim, files, objectStore := pvcArtifacts(items)
	if objectStore {
		// ponytail: object-store pruning needs a provider SDK / bucket lifecycle rules (ADR-016).
		// Until then keep the chain rather than orphan its objects, and say so once.
		if r.Recorder != nil {
			r.Recorder.Event(sched, corev1.EventTypeWarning, oracle.ReasonSchedulePruneUnsupported.String(),
				"chain "+chain+" is expired but its destination is object storage; pruning it is not yet supported (ADR-016)")
		}
		return 0, nil
	}

	// PVC chain: delete the artifact files via an owned Job before deleting the records, so a
	// crash never leaves an orphaned (un-catalogued, un-prunable) file behind.
	if len(files) > 0 {
		job, err := renderbackup.PruneJob(neo4j, renderbackup.PruneJobName(chain), claim, files)
		if err != nil {
			return 0, err
		}
		if err := shared.Apply(ctx, r.Client, r.Scheme, sched, job, func() error { return nil }); err != nil {
			return 0, err
		}
		var owned batchv1.Job
		if err := r.Get(ctx, types.NamespacedName{Name: renderbackup.PruneJobName(chain), Namespace: sched.Namespace}, &owned); err != nil {
			if apierrors.IsNotFound(err) {
				return 15 * time.Second, nil
			}
			return 0, err
		}
		complete, failed, jmsg := shared.JobTerminal(&owned)
		switch {
		case failed:
			detail := jmsg
			if podMsg := shared.JobPodTerminationMessage(ctx, r.Client, owned.Namespace, owned.Name); podMsg != "" {
				detail = podMsg
			}
			if r.Recorder != nil {
				r.Recorder.Event(sched, corev1.EventTypeWarning, oracle.ReasonSchedulePruneFailed.String(),
					"prune of chain "+chain+" failed: "+detail)
			}
			return 15 * time.Second, nil
		case !complete:
			return 15 * time.Second, nil
		}
		// complete → the files are gone; delete the records below.
	}

	deleted := 0
	for _, b := range items {
		if err := r.Delete(ctx, b); err != nil && !apierrors.IsNotFound(err) {
			return 0, err
		}
		deleted++
	}
	if r.Recorder != nil {
		r.Recorder.Event(sched, corev1.EventTypeNormal, oracle.ReasonSchedulePruned.String(),
			fmt.Sprintf("pruned expired chain %s (%d backups, %d artifacts)", chain, deleted, len(files)))
	}
	// More expired chains may remain — come back promptly to drain them.
	if len(expired) > 1 {
		return time.Second, nil
	}
	return 0, nil
}

// groupChains buckets a schedule's backups by their chain label. Backups without one (a hand-made
// Neo4jBackup that happens to carry the schedule label) are ignored.
func groupChains(items []neo4jv1beta1.Neo4jBackup) map[string][]*neo4jv1beta1.Neo4jBackup {
	chains := map[string][]*neo4jv1beta1.Neo4jBackup{}
	for i := range items {
		if chain := items[i].Labels[LabelChain]; chain != "" {
			chains[chain] = append(chains[chain], &items[i])
		}
	}
	return chains
}

// expiredChains returns the chain ids that fall outside full.retention, oldest-first, never
// including the active chain. keepLast counts whole chains (the active one included in the budget);
// keepDays keeps chains whose anchoring full is younger than the window.
func expiredChains(chains map[string][]*neo4jv1beta1.Neo4jBackup, ret *neo4jv1beta1.BackupRetention, current string, now time.Time) []string {
	type chainInfo struct {
		id string
		at time.Time
	}
	infos := make([]chainInfo, 0, len(chains))
	for id, items := range chains {
		if id == current {
			continue // never prune the chain the schedule is still writing to
		}
		infos = append(infos, chainInfo{id: id, at: chainTime(items)})
	}
	// Newest first, id as a stable tie-break.
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].at.Equal(infos[j].at) {
			return infos[i].id > infos[j].id
		}
		return infos[i].at.After(infos[j].at)
	})

	prune := map[string]bool{}
	switch {
	case ret.KeepLast != nil:
		// The active chain (excluded above) still counts toward the budget, so we may keep N-1
		// other chains; the rest expire.
		keepOthers := int(*ret.KeepLast) - 1
		if keepOthers < 0 {
			keepOthers = 0
		}
		for i, info := range infos {
			if i >= keepOthers {
				prune[info.id] = true
			}
		}
	case ret.KeepDays != nil:
		cutoff := now.Add(-time.Duration(*ret.KeepDays) * 24 * time.Hour)
		for _, info := range infos {
			if info.at.Before(cutoff) {
				prune[info.id] = true
			}
		}
	}

	// Oldest-first so callers drain the oldest chain first.
	out := make([]string, 0, len(prune))
	for i := len(infos) - 1; i >= 0; i-- {
		if prune[infos[i].id] {
			out = append(out, infos[i].id)
		}
	}
	return out
}

// chainTime is the chain's anchoring point: the earliest creation among its backups (the full).
func chainTime(items []*neo4jv1beta1.Neo4jBackup) time.Time {
	var t time.Time
	for _, b := range items {
		ct := b.CreationTimestamp.Time
		if t.IsZero() || ct.Before(t) {
			t = ct
		}
	}
	return t
}

// pvcArtifacts collects the recorded artifact filenames and the claim for a chain. objectStore is
// true if any backup targets object storage (which the operator cannot prune yet) — a chain shares
// one destination, so this is all-or-nothing in practice.
func pvcArtifacts(items []*neo4jv1beta1.Neo4jBackup) (claim string, files []string, objectStore bool) {
	for _, b := range items {
		if b.Spec.Destination.Type != neo4jv1beta1.BackupDestinationPVC {
			return "", nil, true
		}
		if b.Spec.Destination.PVC != nil && b.Spec.Destination.PVC.ClaimName != "" {
			claim = b.Spec.Destination.PVC.ClaimName
		}
		for _, a := range b.Status.Artifacts {
			if a.Path != "" {
				files = append(files, a.Path)
			}
		}
	}
	return claim, files, false
}

func (r *ScheduleReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *ScheduleReconciler) fail(ctx context.Context, sched *neo4jv1beta1.Neo4jBackupSchedule, reason oracle.Reason, msg string) (ctrl.Result, error) {
	r.setCondition(sched, metav1.ConditionFalse, reason, msg)
	if r.Recorder != nil {
		r.Recorder.Event(sched, corev1.EventTypeWarning, reason.String(), msg)
	}
	return ctrl.Result{}, r.writeStatus(ctx, sched)
}

func (r *ScheduleReconciler) retryable(ctx context.Context, sched *neo4jv1beta1.Neo4jBackupSchedule, reason oracle.Reason, msg string) (ctrl.Result, error) {
	r.setCondition(sched, metav1.ConditionFalse, reason, msg)
	if err := r.writeStatus(ctx, sched); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *ScheduleReconciler) setCondition(sched *neo4jv1beta1.Neo4jBackupSchedule, status metav1.ConditionStatus, reason oracle.Reason, msg string) {
	meta.SetStatusCondition(&sched.Status.Conditions, metav1.Condition{
		Type:               oracle.ConditionScheduleReady.String(),
		Status:             status,
		Reason:             reason.String(),
		Message:            msg,
		ObservedGeneration: sched.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (r *ScheduleReconciler) writeStatus(ctx context.Context, sched *neo4jv1beta1.Neo4jBackupSchedule) error {
	sched.Status.ObservedGeneration = sched.Generation
	if err := r.Status().Update(ctx, sched); err != nil {
		if apierrors.IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}

func (r *ScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jBackupSchedule{}).
		Owns(&neo4jv1beta1.Neo4jBackup{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
