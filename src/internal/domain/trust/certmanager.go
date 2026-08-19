package trust

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

// issuanceRequeue is how long to wait before re-checking a Secret cert-manager has not
// published yet. Issuance is usually sub-second for a local Issuer but can involve an
// ACME order, so this polls rather than blocks the pipeline.
const issuanceRequeue = 10 * time.Second

// reconcileCertificates applies one cert-manager Certificate per active TLS policy and
// prunes Certificates for policies that are no longer configured (BDR-006).
func (r *Reconciler) reconcileCertificates(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	log := ctrllog.FromContext(ctx)

	desired := rendertrust.Certificates(neo4j, rendersecrets.WithMountableLabel(nil))
	keep := make(map[string]struct{}, len(desired))
	for _, pc := range desired {
		keep[pc.Object.GetName()] = struct{}{}
		log.Info("reconciling Certificate", "policy", pc.Policy, "name", pc.Object.GetName())
		if err := r.applyCertificate(ctx, neo4j, pc); err != nil {
			return shared.Failed(err)
		}
	}
	return r.pruneCertificates(ctx, neo4j, keep)
}

func (r *Reconciler) applyCertificate(ctx context.Context, neo4j *neo4jv1beta1.Neo4j, pc rendertrust.PolicyCertificate) error {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(rendertrust.CertificateGVK)
	cert.SetName(pc.Object.GetName())
	cert.SetNamespace(pc.Object.GetNamespace())

	err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, cert, func() error {
		cert.SetLabels(pc.Object.GetLabels())
		spec, found, err := unstructured.NestedMap(pc.Object.Object, "spec")
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("rendered Certificate for policy %q has no spec", pc.Policy)
		}
		return unstructured.SetNestedMap(cert.Object, spec, "spec")
	})
	if err == nil {
		return nil
	}
	// Unlike the optional ServiceMonitor, a missing CRD here cannot be skipped: the user
	// asked for cert-manager, and carrying on would leave pods waiting forever on a Secret
	// nothing is going to write.
	if meta.IsNoMatchError(err) {
		return fmt.Errorf("trust.certManager.enabled requires cert-manager to be installed (no %s type in the cluster): %w",
			rendertrust.CertificateGVK.GroupKind(), err)
	}
	return fmt.Errorf("apply Certificate for policy %q: %w", pc.Policy, err)
}

// pruneCertificates deletes operator-owned Certificates that are no longer desired, so
// disabling a policy stops its renewals instead of leaving a live certificate behind.
func (r *Reconciler) pruneCertificates(ctx context.Context, neo4j *neo4jv1beta1.Neo4j, keep map[string]struct{}) shared.StepResult {
	log := ctrllog.FromContext(ctx)

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(rendertrust.CertificateGVK)
	err := r.Client.List(ctx, list,
		client.InNamespace(neo4j.Namespace),
		client.MatchingLabels{
			render.LabelInstance:  neo4j.Name,
			render.LabelComponent: "trust",
		})
	if err != nil {
		if meta.IsNoMatchError(err) {
			return shared.Done()
		}
		return shared.Failed(fmt.Errorf("list Certificates: %w", err))
	}

	for i := range list.Items {
		cert := &list.Items[i]
		if _, wanted := keep[cert.GetName()]; wanted {
			continue
		}
		// Name collision must not delete a foreign Certificate (ADD-05).
		if !metav1.IsControlledBy(cert, neo4j) {
			continue
		}
		log.Info("deleting stale Certificate", "name", cert.GetName())
		if err := r.Client.Delete(ctx, cert); err != nil && client.IgnoreNotFound(err) != nil {
			return shared.Failed(fmt.Errorf("delete Certificate %q: %w", cert.GetName(), err))
		}
	}
	return shared.Done()
}

// awaitIssuedSecrets requeues until cert-manager has published usable key material.
// A missing Secret is expected on first reconcile, so it requeues rather than failing.
func (r *Reconciler) awaitIssuedSecrets(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	log := ctrllog.FromContext(ctx)
	for _, need := range rendertrust.ProvisionedSecretKeys(neo4j) {
		var secret corev1.Secret
		key := types.NamespacedName{Name: need.SecretName, Namespace: neo4j.Namespace}
		if err := r.Client.Get(ctx, key, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("waiting for cert-manager to issue certificate",
					"secret", need.SecretName, "requeueAfter", issuanceRequeue)
				return shared.Requeue(issuanceRequeue)
			}
			return shared.Failed(fmt.Errorf("get issued trust secret %q: %w", need.SecretName, err))
		}
		// The target Secret name is CR-supplied, so it could point at an unrelated Secret
		// that already exists. Requiring the same opt-in label as any other operator mount
		// keeps NEO-005 intact; Certificates we create carry it via secretTemplate.
		if err := rendersecrets.RequireMountable(&secret); err != nil {
			return shared.Failed(err)
		}
		if len(secret.Data[need.Key]) == 0 {
			log.Info("waiting for cert-manager to populate certificate",
				"secret", need.SecretName, "key", need.Key, "requeueAfter", issuanceRequeue)
			return shared.Requeue(issuanceRequeue)
		}
	}
	return shared.Done()
}
