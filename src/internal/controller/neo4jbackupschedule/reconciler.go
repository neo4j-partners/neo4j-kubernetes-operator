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
// skip). Retention pruning and the aggregate cadence are later increments.
package neo4jbackupschedule

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
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
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
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

	// INCREMENTAL cadence — attaches to the current chain; skipped until a full has anchored one.
	if incSched != nil && sched.Status.CurrentChain != "" {
		if fire, scheduled := due(incSched, baseTime(sched.Status.LastIncrementalTime, sched.CreationTimestamp), now); fire {
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

	r.setCondition(&sched, metav1.ConditionTrue, oracle.ReasonScheduleActive, "schedule active")
	if err := r.writeStatus(ctx, &sched); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue at the earliest next tick so emission does not depend on the resync period.
	next := fullSched.Next(now)
	if incSched != nil && sched.Status.CurrentChain != "" {
		if n := incSched.Next(now); n.Before(next) {
			next = n
		}
	}
	return ctrl.Result{RequeueAfter: requeueDelay(next, now)}, nil
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
		Complete(r)
}
