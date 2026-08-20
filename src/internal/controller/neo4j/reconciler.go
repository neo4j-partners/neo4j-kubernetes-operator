package neo4j

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/connectivity"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/formation"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/persistence"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/serverconfig"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/trust"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/workload"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/events"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
	renderconfig "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/serverconfig"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/status"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/validation"
)

const FinalizerName = "neo4j.com/finalizer"

// Neo4jReconciler reconciles Neo4j custom resources (ADR-003 pipeline).
type Neo4jReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	Persistence  *persistence.Reconciler
	Trust        *trust.Reconciler
	ServerConfig *serverconfig.Reconciler
	Workload     *workload.Reconciler
	Connectivity *connectivity.Reconciler
	Formation    *formation.Reconciler
	StatusWriter *status.Writer

	// MaxConcurrentReconciles defaults to 2 so one wedged CR cannot starve others (NEO-014).
	MaxConcurrentReconciles int

	// advisories keeps the spec-derived Events to one per generation. Zero value is usable.
	advisories events.Advisory
}

const (
	DefaultMaxConcurrentReconciles = 2
	MaxConcurrentReconcilesLimit   = 16
)

// NormalizeMaxConcurrentReconciles maps 0 to the default and rejects values above the cap.
func NormalizeMaxConcurrentReconciles(n int) (int, error) {
	if n < 0 {
		return 0, fmt.Errorf("max concurrent reconciles must be >= 0")
	}
	if n == 0 {
		return DefaultMaxConcurrentReconciles, nil
	}
	if n > MaxConcurrentReconcilesLimit {
		return 0, fmt.Errorf("max concurrent reconciles %d exceeds maximum %d (NEO-014)", n, MaxConcurrentReconcilesLimit)
	}
	return n, nil
}

// +kubebuilder:rbac:groups=neo4j.com,resources=neo4js,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4js/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4js/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;secrets;configmaps;serviceaccounts;persistentvolumeclaims;endpoints;pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete

func (r *Neo4jReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithName("neo4j")
	log.V(1).Info("reconcile start")

	var neo4j neo4jv1beta1.Neo4j
	if err := r.Get(ctx, req.NamespacedName, &neo4j); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "get Neo4j failed")
		return ctrl.Result{}, err
	}

	if !neo4j.DeletionTimestamp.IsZero() {
		log.Info("reconcile delete")
		return r.reconcileDelete(ctx, &neo4j)
	}

	if !controllerutil.ContainsFinalizer(&neo4j, FinalizerName) {
		controllerutil.AddFinalizer(&neo4j, FinalizerName)
		if err := r.Update(ctx, &neo4j); err != nil {
			log.Error(err, "add finalizer failed")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	pipeResult, err := r.runPipeline(ctx, &neo4j)
	if err != nil {
		if apierrors.IsConflict(err) {
			// Transient RV conflict (STS/CR updated concurrently) — retry, don't fail status.
			log.V(1).Info("conflict, requeue")
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "pipeline failed")
		r.StatusWriter.MarkPipelineError(&neo4j, err)
		neo4j.Status.ObservedGeneration = neo4j.Generation
		_ = r.Client.Status().Update(ctx, &neo4j)
		return ctrl.Result{}, err
	}

	// Domain asked to requeue — persist in-memory status (e.g. ClusterFormed/BoltUnavailable)
	// but skip ObserveAndWrite so we don't recalculate Ready over a mid-pipeline snapshot
	// (formation already Status().Update'd drainOK when needed).
	if pipeResult.Requeue || pipeResult.RequeueAfter > 0 {
		if err := r.Client.Status().Update(ctx, &neo4j); err != nil {
			if apierrors.IsConflict(err) {
				log.V(1).Info("status conflict on requeue, requeue")
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "status write on requeue failed")
			return ctrl.Result{}, err
		}
		return pipeResult, nil
	}

	if err := r.StatusWriter.ObserveAndWrite(ctx, &neo4j); err != nil {
		if apierrors.IsConflict(err) {
			log.V(1).Info("status conflict, requeue")
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "status write failed")
		return ctrl.Result{}, err
	}

	if status.OfflineMode(&neo4j) {
		// Stable maintenance window — avoid 30s polling while Ready stays false.
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	if !status.IsReady(&neo4j) {
		log.V(1).Info("not ready, requeue", "after", "30s")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *Neo4jReconciler) runPipeline(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) (ctrl.Result, error) {
	if err := validation.ValidateNeo4j(neo4j); err != nil {
		return ctrl.Result{}, err
	}
	if err := rendersecrets.EnsureMountable(ctx, r.Client, neo4j); err != nil {
		// Same reason on the Event as on the Error condition, so `kubectl describe` and the
		// status oracle agree on one identifier.
		if r.Recorder != nil {
			r.Recorder.Event(neo4j, corev1.EventTypeWarning, status.PipelineErrorReason(err), err.Error())
		}
		return ctrl.Result{}, err
	}
	// Which Secrets a spec mounts only changes with the spec, so this reports once per generation
	// instead of once per pass — see internal/events for what the repetition costs.
	for _, name := range rendersecrets.ReferencedMountSecrets(neo4j) {
		r.advisories.Emitf(r.Recorder, neo4j, corev1.EventTypeNormal, "SecretMounted",
			"Mounting Secret %q into Neo4j pods (label %s=%s)", name, rendersecrets.MountableLabel, rendersecrets.MountableLabelValue)
	}

	log := ctrllog.FromContext(ctx).WithName("pipeline")
	reportDuplicateEntries(log, r.Recorder, &r.advisories, neo4j)

	steps := []struct {
		name string
		step shared.Reconciler
	}{
		{"persistence", r.Persistence},
		{"trust", r.Trust},
		{"serverconfig", r.ServerConfig},
		{"workload", r.Workload},
		{"connectivity", r.Connectivity},
		{"formation", r.Formation},
	}
	for _, s := range steps {
		// Named logger + keys so Apply and domain code inherit domain/reconciler.
		stepLog := log.WithName(s.name).WithValues("domain", s.name, "reconciler", s.name)
		stepCtx := ctrllog.IntoContext(ctx, stepLog)
		stepLog.V(1).Info("domain reconcile start")
		out := s.step.Reconcile(stepCtx, neo4j)
		if out.Err != nil {
			stepLog.Error(out.Err, "domain reconcile failed")
			return out.Result, out.Err
		}
		if out.Result.Requeue || out.Result.RequeueAfter > 0 {
			stepLog.Info("domain reconcile requeue", "requeue", out.Result.Requeue, "after", out.Result.RequeueAfter)
			return out.Result, nil
		}
		stepLog.V(1).Info("domain reconcile done")
	}
	return ctrl.Result{}, nil
}

// reportDuplicateEntries surfaces values a render merge dropped on a key collision. Those
// merges are deterministic and often legitimate (a user argument beating a Neo4j default) but
// silent, so an operator learns which value won without diffing the rendered output. ADR-014
// has no warn level, hence Info on the log and a Warning Event carrying the oracle reason.
//
// Field-agnostic on purpose: every source returning render.Duplicate reports the same way.
func reportDuplicateEntries(log logr.Logger, recorder record.EventRecorder, advisories *events.Advisory, neo4j *neo4jv1beta1.Neo4j) {
	for _, d := range renderconfig.Duplicates(neo4j) {
		log.Info("duplicate entry",
			"field", d.Field, "key", d.Key,
			"kept", d.Kept, "keptFrom", d.KeptFrom,
			"dropped", d.Dropped, "droppedFrom", d.DroppedFrom)
		// A collision is a property of the spec: one Event per generation, not per pass.
		advisories.Emit(recorder, neo4j, corev1.EventTypeWarning, status.ReasonDuplicateEntry, d.Message())
	}
}

func (r *Neo4jReconciler) reconcileDelete(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(neo4j, FinalizerName) {
		return ctrl.Result{}, nil
	}
	log := ctrllog.FromContext(ctx).WithName("pipeline").WithName("persistence").
		WithValues("domain", "persistence", "reconciler", "persistence")
	ctx = ctrllog.IntoContext(ctx, log)
	// Default Retain (UNINST-01): GC removes owned objects; Dynamic PVCs stay.
	// whenDeleted=Delete (UNINST-02): wipe STS then Dynamic PVCs before releasing the finalizer.
	if pending, err := persistence.WipeOnUninstall(ctx, r.Client, neo4j); err != nil {
		log.Error(err, "domain reconcile failed", "op", "wipeOnUninstall")
		return ctrl.Result{RequeueAfter: 15 * time.Second}, err
	} else if pending {
		log.V(1).Info("domain reconcile requeue", "op", "wipeOnUninstall")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	controllerutil.RemoveFinalizer(neo4j, FinalizerName)
	if err := r.Update(ctx, neo4j); err != nil {
		log.Error(err, "remove finalizer failed")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	log.Info("domain reconcile done", "op", "delete")
	return ctrl.Result{}, nil
}

func (r *Neo4jReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).For(&neo4jv1beta1.Neo4j{})
	for _, obj := range serverconfig.OwnedTypes() {
		builder = builder.Owns(obj)
	}
	if mgr.GetRESTMapper() != nil {
		for _, obj := range trust.OwnedTypes() {
			// cert-manager is optional: only watch Certificates when the CRD is installed,
			// or the cache would fail to start for every user who does not run cert-manager.
			gvk := obj.GetObjectKind().GroupVersionKind()
			if _, err := mgr.GetRESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
				continue
			}
			builder = builder.Owns(obj)
		}
	}
	for _, obj := range workload.OwnedTypes() {
		builder = builder.Owns(obj)
	}
	for _, obj := range connectivity.OwnedTypes() {
		builder = builder.Owns(obj)
	}
	builder = builder.Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.mapSecretToNeo4j))
	n, err := NormalizeMaxConcurrentReconciles(r.MaxConcurrentReconciles)
	if err != nil {
		return err
	}
	return builder.WithOptions(controller.Options{MaxConcurrentReconciles: n}).Complete(r)
}

func NewReconciler(mgr ctrl.Manager) *Neo4jReconciler {
	c := mgr.GetClient()
	scheme := mgr.GetScheme()
	return &Neo4jReconciler{
		Client:       c,
		Scheme:       scheme,
		Recorder:     mgr.GetEventRecorderFor("neo4j-controller"),
		Persistence:  persistence.New(c),
		Trust:        trust.New(c, scheme),
		ServerConfig: serverconfig.New(c, scheme),
		Workload:     workload.New(c, scheme),
		Connectivity: connectivity.New(c, scheme),
		Formation:    formation.New(c, scheme, mgr.GetEventRecorderFor("neo4j-controller")),
		StatusWriter: status.NewWriter(c),
	}
}
