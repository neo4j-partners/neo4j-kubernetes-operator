package status

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
)

const (
	ConditionReady        = "Ready"
	ConditionReconciling  = "Reconciling"
	ConditionInstalled    = "Installed"
	ConditionError        = "Error"
	ConditionStorageReady = "StorageReady"
)

// Writer updates Neo4j status from observed cluster state (ADR-004).
type Writer struct {
	Client client.Client
}

func NewWriter(c client.Client) *Writer {
	return &Writer{Client: c}
}

// MarkReconciling sets the Reconciling condition at pipeline start.
func (w *Writer) MarkReconciling(neo4j *neo4jv1beta1.Neo4j) {
	setCondition(neo4j, ConditionReconciling, metav1.ConditionTrue, "InProgress", "Reconciliation in progress")
	setCondition(neo4j, ConditionError, metav1.ConditionFalse, "NoError", "")
}

// MarkPipelineError records a reconcile failure.
func (w *Writer) MarkPipelineError(neo4j *neo4jv1beta1.Neo4j, err error) {
	setCondition(neo4j, ConditionError, metav1.ConditionTrue, "ReconcileFailed", err.Error())
	setCondition(neo4j, ConditionReady, metav1.ConditionFalse, "ReconcileError", err.Error())
	setCondition(neo4j, ConditionReconciling, metav1.ConditionFalse, "Failed", err.Error())
	neo4j.Status.Phase = neo4jv1beta1.Neo4jPhaseFailed
}

// ObserveAndWrite refreshes status from the API server and patches status subresource.
func (w *Writer) ObserveAndWrite(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) error {
	var ready, desired int32
	var anySTSFound bool
	storageReady := true

	for _, pool := range render.ActivePools(neo4j) {
		ctxRender := render.ContextForPool(neo4j, pool)
		poolDesired := ctxRender.PoolReplicas()

		var sts appsv1.StatefulSet
		stsKey := types.NamespacedName{Name: ctxRender.STSName(), Namespace: ctxRender.Namespace()}
		if w.Client.Get(ctx, stsKey, &sts) == nil {
			anySTSFound = true
			desired += poolDesired
			ready += sts.Status.ReadyReplicas
		}
		if !w.checkPoolStorageReady(ctx, ctxRender) {
			storageReady = false
		}
	}

	setCondition(neo4j, ConditionInstalled, boolCondition(anySTSFound), installedReason(anySTSFound), "")
	neo4j.Status.ServerSummary = &neo4jv1beta1.ReplicaSummary{Servers: desired, Ready: ready}
	setCondition(neo4j, ConditionStorageReady, boolCondition(storageReady), storageReason(storageReady), "")

	allReady := anySTSFound && ready == desired && desired > 0 && storageReady
	setCondition(neo4j, ConditionReconciling, metav1.ConditionFalse, "Completed", "")
	setCondition(neo4j, ConditionError, metav1.ConditionFalse, "NoError", "")
	setCondition(neo4j, ConditionReady, boolCondition(allReady), readyReason(allReady), readyMessage(ready, desired))

	if allReady {
		neo4j.Status.Phase = neo4jv1beta1.Neo4jPhaseRunning
		neo4j.Status.Version = neo4j.Spec.Version
	} else if anySTSFound {
		neo4j.Status.Phase = neo4jv1beta1.Neo4jPhaseBootstrapping
	} else {
		neo4j.Status.Phase = neo4jv1beta1.Neo4jPhaseProvisioning
	}

	neo4j.Status.Endpoints = buildEndpoints(render.ClientServiceContext(neo4j))
	neo4j.Status.ObservedGeneration = neo4j.Generation

	return w.Client.Status().Update(ctx, neo4j)
}

func (w *Writer) checkPoolStorageReady(ctx context.Context, ctxRender render.Context) bool {
	pvcName := fmt.Sprintf("data-%s-0", ctxRender.STSName())
	var pvc corev1.PersistentVolumeClaim
	if err := w.Client.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: ctxRender.Namespace()}, &pvc); err != nil {
		return false
	}
	return pvc.Status.Phase == corev1.ClaimBound
}

func buildEndpoints(ctx render.Context) *neo4jv1beta1.EndpointsStatus {
	ns := ctx.Namespace()
	name := ctx.ClientServiceName()
	boltPort := ctx.ServiceFacadePort(render.ConnectorBolt)
	if boltPort == 0 {
		boltPort = ctx.BoltPort()
	}
	boltURI := fmt.Sprintf("neo4j://%s.%s.svc:%d", name, ns, boltPort)
	ep := &neo4jv1beta1.EndpointsStatus{
		Bolt:     boltURI,
		Internal: fmt.Sprintf("%s.%s.svc:%d", ctx.HeadlessServiceName(), ns, ctx.BoltPort()),
		ConnectionExamples: &neo4jv1beta1.ConnectionExamples{
			BoltURI:     boltURI,
			PortForward: fmt.Sprintf("kubectl port-forward -n %s svc/%s %d:%d", ns, name, boltPort, boltPort),
		},
	}
	if ctx.HTTPEnabled() {
		exposed := false
		for _, name := range ctx.ClientExpose() {
			if name == render.ConnectorHTTP {
				exposed = true
				break
			}
		}
		if exposed {
			httpPort := ctx.ServiceFacadePort(render.ConnectorHTTP)
			if httpPort == 0 {
				httpPort = ctx.HTTPPort()
			}
			ep.HTTP = fmt.Sprintf("http://%s.%s.svc:%d", name, ns, httpPort)
		}
	}
	return ep
}

func setCondition(neo4j *neo4jv1beta1.Neo4j, typ string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&neo4j.Status.Conditions, metav1.Condition{
		Type:               typ,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: neo4j.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func boolCondition(ok bool) metav1.ConditionStatus {
	if ok {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

func installedReason(ok bool) string {
	if ok {
		return "ObjectsCreated"
	}
	return "Pending"
}

func storageReason(ok bool) string {
	if ok {
		return "PVCBound"
	}
	return "PVCPending"
}

func readyReason(ok bool) string {
	if ok {
		return "AllMembersReady"
	}
	return "MembersNotReady"
}

func readyMessage(ready, desired int32) string {
	return fmt.Sprintf("%d/%d servers ready", ready, desired)
}

// IsReady reports whether the Ready condition is True.
func IsReady(neo4j *neo4jv1beta1.Neo4j) bool {
	for _, c := range neo4j.Status.Conditions {
		if c.Type == ConditionReady {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}
