package neo4j

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/connectivity"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/persistence"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/serverconfig"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/trust"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/workload"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/status"
)

const FinalizerName = "neo4j.com/finalizer"

// Neo4jReconciler reconciles Neo4j custom resources (ADR-003 pipeline).
type Neo4jReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	Persistence   *persistence.Reconciler
	Trust         *trust.Reconciler
	ServerConfig  *serverconfig.Reconciler
	Workload      *workload.Reconciler
	Connectivity  *connectivity.Reconciler
	StatusWriter  *status.Writer
}

// +kubebuilder:rbac:groups=neo4j.com,resources=neo4js,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4js/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4js/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;secrets;configmaps;serviceaccounts;persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *Neo4jReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var neo4j neo4jv1beta1.Neo4j
	if err := r.Get(ctx, req.NamespacedName, &neo4j); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !neo4j.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &neo4j)
	}

	if !controllerutil.ContainsFinalizer(&neo4j, FinalizerName) {
		controllerutil.AddFinalizer(&neo4j, FinalizerName)
		if err := r.Update(ctx, &neo4j); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	result, err := r.runPipeline(ctx, &neo4j)
	if err != nil {
		r.StatusWriter.MarkPipelineError(&neo4j, err)
		neo4j.Status.ObservedGeneration = neo4j.Generation
		_ = r.Client.Status().Update(ctx, &neo4j)
		return result, err
	}

	if err := r.StatusWriter.ObserveAndWrite(ctx, &neo4j); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if !status.IsReady(&neo4j) {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *Neo4jReconciler) runPipeline(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) (ctrl.Result, error) {
	steps := []shared.Reconciler{
		r.Persistence,
		r.Trust,
		r.ServerConfig,
		r.Workload,
		r.Connectivity,
	}
	for _, step := range steps {
		out := step.Reconcile(ctx, neo4j)
		if out.Err != nil {
			return out.Result, out.Err
		}
		if out.Result.Requeue || out.Result.RequeueAfter > 0 {
			return out.Result, nil
		}
	}
	return ctrl.Result{}, nil
}

func (r *Neo4jReconciler) reconcileDelete(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(neo4j, FinalizerName) {
		return ctrl.Result{}, nil
	}
	// V1: preserve PVCs — child objects are garbage-collected via owner references.
	controllerutil.RemoveFinalizer(neo4j, FinalizerName)
	if err := r.Update(ctx, neo4j); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	return ctrl.Result{}, nil
}

func (r *Neo4jReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).For(&neo4jv1beta1.Neo4j{})
	for _, obj := range serverconfig.OwnedTypes() {
		builder = builder.Owns(obj)
	}
	for _, obj := range workload.OwnedTypes() {
		builder = builder.Owns(obj)
	}
	for _, obj := range connectivity.OwnedTypes() {
		builder = builder.Owns(obj)
	}
	return builder.Complete(r)
}

func NewReconciler(mgr ctrl.Manager) *Neo4jReconciler {
	c := mgr.GetClient()
	scheme := mgr.GetScheme()
	return &Neo4jReconciler{
		Client:       c,
		Scheme:       scheme,
		Persistence:  persistence.New(),
		Trust:        trust.New(),
		ServerConfig: serverconfig.New(c, scheme),
		Workload:     workload.New(c, scheme),
		Connectivity: connectivity.New(c, scheme),
		StatusWriter: status.NewWriter(c),
	}
}
