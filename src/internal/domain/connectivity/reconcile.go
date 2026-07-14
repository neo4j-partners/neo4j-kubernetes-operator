package connectivity

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
	renderconn "github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render/connectivity"
)

// Reconciler applies headless, client, admin, and cluster-internal Services (BDR-007).
type Reconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

func New(c client.Client, scheme *runtime.Scheme) *Reconciler {
	return &Reconciler{Client: c, Scheme: scheme}
}

func (r *Reconciler) Reconcile(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	for _, pool := range render.ActivePools(neo4j) {
		ctxRender := render.ContextForPool(neo4j, pool)
		headlessDesired := renderconn.HeadlessService(ctxRender)
		headless := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: headlessDesired.Name, Namespace: headlessDesired.Namespace}}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, headless, func() error {
			headless.Labels = headlessDesired.Labels
			headless.Annotations = headlessDesired.Annotations
			headless.Spec = headlessDesired.Spec
			return nil
		}); err != nil {
			return shared.Failed(err)
		}

		if render.IsClusterMode(neo4j) {
			for _, memberSvc := range renderconn.ClusterMemberServices(ctxRender) {
				svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: memberSvc.Name, Namespace: memberSvc.Namespace}}
				if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, svc, func() error {
					svc.Labels = memberSvc.Labels
					svc.Annotations = memberSvc.Annotations
					svc.Spec = memberSvc.Spec
					return nil
				}); err != nil {
					return shared.Failed(err)
				}
			}
		}
	}

	clientCtx := render.ClientServiceContext(neo4j)
	clientDesired := renderconn.ClientService(clientCtx)
	clientSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: clientDesired.Name, Namespace: clientDesired.Namespace}}
	if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, clientSvc, func() error {
		clientSvc.Labels = clientDesired.Labels
		clientSvc.Annotations = clientDesired.Annotations
		clientSvc.Spec = clientDesired.Spec
		return nil
	}); err != nil {
		return shared.Failed(err)
	}

	if clientCtx.ShouldCreateAdminService() {
		adminDesired := renderconn.AdminService(clientCtx)
		adminSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: adminDesired.Name, Namespace: adminDesired.Namespace}}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, adminSvc, func() error {
			adminSvc.Labels = adminDesired.Labels
			adminSvc.Annotations = adminDesired.Annotations
			adminSvc.Spec = adminDesired.Spec
			return nil
		}); err != nil {
			return shared.Failed(err)
		}
	}

	return shared.Done()
}

// OwnedTypes returns types watched via Owns().
func OwnedTypes() []client.Object {
	return []client.Object{&corev1.Service{}}
}
