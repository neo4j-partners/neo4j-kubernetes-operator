package connectivity

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderconn "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/connectivity"
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
	log := ctrllog.FromContext(ctx)
	for _, pool := range render.ActivePools(neo4j) {
		ctxRender := render.ContextForPool(neo4j, pool)
		headlessDesired := renderconn.HeadlessService(ctxRender)
		log.Info("reconciling service", "pool", string(pool), "role", "headless", "name", headlessDesired.Name)
		headless := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: headlessDesired.Name, Namespace: headlessDesired.Namespace}}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, headless, func() error {
			headless.Labels = headlessDesired.Labels
			headless.Annotations = headlessDesired.Annotations
			preserveServiceServerFields(&headless.Spec, &headlessDesired.Spec)
			headless.Spec = headlessDesired.Spec
			return nil
		}); err != nil {
			return shared.Failed(err)
		}

		if render.IsClusterMode(neo4j) {
			for _, memberSvc := range renderconn.ClusterMemberServices(ctxRender) {
				log.Info("reconciling service", "pool", string(pool), "role", "internals", "name", memberSvc.Name)
				svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: memberSvc.Name, Namespace: memberSvc.Namespace}}
				if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, svc, func() error {
					svc.Labels = memberSvc.Labels
					svc.Annotations = memberSvc.Annotations
					preserveServiceServerFields(&svc.Spec, &memberSvc.Spec)
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
	log.Info("reconciling service", "role", "client", "name", clientDesired.Name, "type", clientDesired.Spec.Type)
	clientSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: clientDesired.Name, Namespace: clientDesired.Namespace}}
	if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, clientSvc, func() error {
		clientSvc.Labels = clientDesired.Labels
		clientSvc.Annotations = clientDesired.Annotations
		preserveServiceServerFields(&clientSvc.Spec, &clientDesired.Spec)
		clientSvc.Spec = clientDesired.Spec
		return nil
	}); err != nil {
		return shared.Failed(err)
	}

	if clientCtx.ShouldCreateAdminService() {
		adminDesired := renderconn.AdminService(clientCtx)
		log.Info("reconciling service", "role", "admin", "name", adminDesired.Name)
		adminSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: adminDesired.Name, Namespace: adminDesired.Namespace}}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, adminSvc, func() error {
			adminSvc.Labels = adminDesired.Labels
			adminSvc.Annotations = adminDesired.Annotations
			preserveServiceServerFields(&adminSvc.Spec, &adminDesired.Spec)
			adminSvc.Spec = adminDesired.Spec
			return nil
		}); err != nil {
			return shared.Failed(err)
		}
	}

	if out := r.reconcileServiceMonitor(ctx, neo4j); out.Err != nil {
		return out
	}

	return shared.Done()
}

// preserveServiceServerFields copies forward Service spec fields the operator
// does not model but that the API server or an external controller populates,
// so the wholesale `svc.Spec = desired.Spec` overwrite below doesn't zero them
// out and trip immutability/validation errors on the next update. The motivating
// case: AWS Load Balancer Controller's webhook sets spec.loadBalancerClass on
// create, which is immutable once non-nil, so clearing it makes the API server
// reject every subsequent update and the controller loops forever (may not change
// once set).
//
// Each field is only copied forward when desired leaves it zero-valued, so the
// operator still wins if it ever starts modelling the field. LoadBalancer- and
// NodePort-scoped fields are gated on desired.Type: carrying them onto a type the
// API server no longer permits (e.g. a LoadBalancer -> ClusterIP downgrade) would
// swap one rejection loop for another.
//
// Intentionally NOT preserved: spec.externalIPs (leaving the overwrite to wipe any
// value the operator didn't set is a guard against the CVE-2020-8554 traffic-hijack
// vector), spec.loadBalancerIP (deprecated), and spec.externalName (only valid for
// the ExternalName type, which the operator never emits).
func preserveServiceServerFields(live, desired *corev1.ServiceSpec) {
	// Valid for every IP-based Service type, so always safe to carry forward.
	if desired.ClusterIP == "" {
		desired.ClusterIP = live.ClusterIP
	}
	if len(desired.ClusterIPs) == 0 {
		desired.ClusterIPs = live.ClusterIPs
	}
	if len(desired.IPFamilies) == 0 {
		desired.IPFamilies = live.IPFamilies
	}
	if desired.IPFamilyPolicy == nil {
		desired.IPFamilyPolicy = live.IPFamilyPolicy
	}
	if desired.SessionAffinity == "" {
		desired.SessionAffinity = live.SessionAffinity
		desired.SessionAffinityConfig = live.SessionAffinityConfig
	}
	if desired.InternalTrafficPolicy == nil {
		desired.InternalTrafficPolicy = live.InternalTrafficPolicy
	}

	lbOrNodePort := desired.Type == corev1.ServiceTypeLoadBalancer || desired.Type == corev1.ServiceTypeNodePort
	if lbOrNodePort {
		if desired.ExternalTrafficPolicy == "" {
			desired.ExternalTrafficPolicy = live.ExternalTrafficPolicy
		}
		// Preserve API-server-assigned nodePorts (matched by port name) to avoid
		// churning the allocation on every reconcile.
		for i := range desired.Ports {
			if desired.Ports[i].NodePort != 0 {
				continue
			}
			for j := range live.Ports {
				if live.Ports[j].Name == desired.Ports[i].Name {
					desired.Ports[i].NodePort = live.Ports[j].NodePort
					break
				}
			}
		}
	}

	if desired.Type == corev1.ServiceTypeLoadBalancer {
		if desired.LoadBalancerClass == nil {
			desired.LoadBalancerClass = live.LoadBalancerClass
		}
		if desired.HealthCheckNodePort == 0 {
			desired.HealthCheckNodePort = live.HealthCheckNodePort
		}
		if desired.AllocateLoadBalancerNodePorts == nil {
			desired.AllocateLoadBalancerNodePorts = live.AllocateLoadBalancerNodePorts
		}
	}
}

// OwnedTypes returns types watched via Owns().
func OwnedTypes() []client.Object {
	return []client.Object{&corev1.Service{}}
}
