package trust

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

// certManagerConfigured is true when the CR has a trust.certManager block at all,
// enabled or not — the signal that Certificates may need applying or pruning.
func certManagerConfigured(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j.Spec.Trust != nil && neo4j.Spec.Trust.CertManager != nil
}

// Reconciler owns cert-manager Certificates and validates TLS Secrets (BDR-006 / BDR-011).
// Volume mounts and conf keys are applied by workload/serverconfig render.
type Reconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

func New(c client.Client, scheme *runtime.Scheme) *Reconciler {
	return &Reconciler{Client: c, Scheme: scheme}
}

// OwnedTypes returns types watched via Owns(). Certificate is registered as unstructured
// so the watch is skipped cleanly when cert-manager is not installed.
func OwnedTypes() []client.Object {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(rendertrust.CertificateGVK)
	return []client.Object{cert}
}

func (r *Reconciler) Reconcile(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	log := ctrllog.FromContext(ctx)
	if err := rendertrust.Validate(neo4j); err != nil {
		log.Error(err, "trust validation failed")
		return shared.Failed(err)
	}
	// Runs even when trust is disabled so turning it off prunes Certificates. Skipped
	// entirely when the CR never mentions cert-manager, to avoid a List on every reconcile.
	//
	// ponytail: dropping the whole trust.certManager block therefore leaves its
	// Certificates until the CR is deleted and owner-reference GC removes them.
	// Upgrade path: gate on a status-recorded "certManager was used" flag instead.
	if certManagerConfigured(neo4j) {
		if out := r.reconcileCertificates(ctx, neo4j); out.Err != nil || out.Result.Requeue || out.Result.RequeueAfter > 0 {
			return out
		}
	}
	if !rendertrust.TrustEnabled(neo4j) {
		log.V(1).Info("trust disabled, skip secret checks")
		return shared.Done()
	}
	if out := r.awaitIssuedSecrets(ctx, neo4j); out.Err != nil || out.Result.RequeueAfter > 0 {
		return out
	}
	for _, name := range rendertrust.BYOSecretNames(neo4j) {
		if _, err := r.getSecret(ctx, neo4j.Namespace, name); err != nil {
			log.Error(err, "trust secret missing", "secret", name)
			return shared.Failed(err)
		}
		log.Info("trust secret present", "secret", name)
	}
	for _, need := range rendertrust.RequiredSecretKeys(neo4j) {
		secret, err := r.getSecret(ctx, neo4j.Namespace, need.SecretName)
		if err != nil {
			log.Error(err, "trust secret key check failed", "secret", need.SecretName, "key", need.Key)
			return shared.Failed(err)
		}
		if err := requireSecretKey(secret, need.Key); err != nil {
			log.Error(err, "trust secret key invalid", "secret", need.SecretName, "key", need.Key)
			return shared.Failed(err)
		}
		log.V(1).Info("trust secret key ok", "secret", need.SecretName, "key", need.Key)
	}
	return shared.Done()
}

func (r *Reconciler) getSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	var secret corev1.Secret
	if err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("trust secret %q not found in namespace %q", name, namespace)
		}
		return nil, fmt.Errorf("get trust secret %q: %w", name, err)
	}
	return &secret, nil
}

func requireSecretKey(secret *corev1.Secret, key string) error {
	if secret.Data == nil {
		return fmt.Errorf("trust secret %q missing data key %q", secret.Name, key)
	}
	v, ok := secret.Data[key]
	if !ok {
		return fmt.Errorf("trust secret %q missing data key %q", secret.Name, key)
	}
	if len(v) == 0 {
		return fmt.Errorf("trust secret %q data key %q is empty", secret.Name, key)
	}
	return nil
}
