package workload

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
	renderwl "github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render/workload"
)

// Reconciler applies workload objects for each active pool.
type Reconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

func New(c client.Client, scheme *runtime.Scheme) *Reconciler {
	return &Reconciler{Client: c, Scheme: scheme}
}

func (r *Reconciler) Reconcile(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	baseCtx := render.ContextForPool(neo4j, render.ActivePools(neo4j)[0])

	saDesired := renderwl.OperandServiceAccount(baseCtx)
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saDesired.Name, Namespace: saDesired.Namespace}}
	if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, sa, func() error {
		sa.Labels = saDesired.Labels
		return nil
	}); err != nil {
		return shared.Failed(err)
	}

	if render.IsClusterMode(neo4j) {
		roleDesired := renderwl.ServiceReaderRole(baseCtx)
		role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: roleDesired.Name, Namespace: roleDesired.Namespace}}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, role, func() error {
			role.Labels = roleDesired.Labels
			role.Rules = roleDesired.Rules
			return nil
		}); err != nil {
			return shared.Failed(err)
		}

		bindingDesired := renderwl.ServiceReaderRoleBinding(baseCtx)
		binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: bindingDesired.Name, Namespace: bindingDesired.Namespace}}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, binding, func() error {
			binding.Labels = bindingDesired.Labels
			binding.Subjects = bindingDesired.Subjects
			binding.RoleRef = bindingDesired.RoleRef
			return nil
		}); err != nil {
			return shared.Failed(err)
		}
	}

	generated := false
	if baseCtx.ShouldGenerateAuthSecret() {
		password, err := r.ensureAuthSecret(ctx, neo4j, baseCtx)
		if err != nil {
			return shared.Failed(err)
		}
		_ = password
		generated = true
	} else if err := r.ensureReferencedAuthSecret(ctx, baseCtx); err != nil {
		return shared.Failed(err)
	}

	if err := r.ensurePluginLicenseSecrets(ctx, neo4j); err != nil {
		return shared.Failed(err)
	}

	for _, pool := range render.ActivePools(neo4j) {
		ctxRender := render.ContextForPool(neo4j, pool)
		stsDesired := renderwl.PoolStatefulSet(ctxRender)
		sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: stsDesired.Name, Namespace: stsDesired.Namespace}}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, sts, func() error {
			sts.Labels = stsDesired.Labels
			// StatefulSet forbids changing serviceName, selector, volumeClaimTemplates,
			// podManagementPolicy after create — only patch mutable fields on update.
			if sts.CreationTimestamp.IsZero() {
				sts.Spec = stsDesired.Spec
				return nil
			}
			sts.Spec.Replicas = stsDesired.Spec.Replicas
			sts.Spec.Template = stsDesired.Spec.Template
			sts.Spec.UpdateStrategy = stsDesired.Spec.UpdateStrategy
			return nil
		}); err != nil {
			return shared.Failed(err)
		}
	}

	r.recordCredentials(neo4j, baseCtx.AuthSecretName(), generated)
	return shared.Done()
}

func (r *Reconciler) ensurePluginLicenseSecrets(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) error {
	if neo4j.Spec.PluginDefinitions == nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, pool := range render.ActivePools(neo4j) {
		poolCtx := render.ContextForPool(neo4j, pool)
		for _, pluginID := range poolCtx.PoolPluginIDs() {
			def, ok := neo4j.Spec.PluginDefinitions[pluginID]
			if !ok || def.LicenseSecretRef == "" {
				continue
			}
			if _, dup := seen[def.LicenseSecretRef]; dup {
				continue
			}
			seen[def.LicenseSecretRef] = struct{}{}
			var secret corev1.Secret
			if err := r.Client.Get(ctx, types.NamespacedName{Name: def.LicenseSecretRef, Namespace: poolCtx.Namespace()}, &secret); err != nil {
				return fmt.Errorf("plugin license secret %q for %q: %w", def.LicenseSecretRef, pluginID, err)
			}
		}
	}
	return nil
}

func (r *Reconciler) ensureAuthSecret(ctx context.Context, neo4j *neo4jv1beta1.Neo4j, ctxRender render.Context) (string, error) {
	secretName := ctxRender.AuthSecretName()
	var existing corev1.Secret
	err := r.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: ctxRender.Namespace()}, &existing)
	if err == nil {
		return "", nil
	}
	if !apierrors.IsNotFound(err) {
		return "", err
	}

	password, err := randomPassword(16)
	if err != nil {
		return "", fmt.Errorf("generate auth password: %w", err)
	}
	secretDesired := renderwl.AuthSecret(ctxRender, password)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretDesired.Name, Namespace: secretDesired.Namespace}}
	if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, secret, func() error {
		secret.Labels = secretDesired.Labels
		secret.Type = secretDesired.Type
		secret.StringData = secretDesired.StringData
		return nil
	}); err != nil {
		return "", err
	}
	return password, nil
}

func (r *Reconciler) ensureReferencedAuthSecret(ctx context.Context, ctxRender render.Context) error {
	var secret corev1.Secret
	if err := r.Client.Get(ctx, types.NamespacedName{Name: ctxRender.AuthSecretName(), Namespace: ctxRender.Namespace()}, &secret); err != nil {
		return fmt.Errorf("auth secret %q: %w", ctxRender.AuthSecretName(), err)
	}
	return nil
}

func (r *Reconciler) recordCredentials(neo4j *neo4jv1beta1.Neo4j, secretName string, generated bool) {
	if neo4j.Status.Credentials == nil {
		neo4j.Status.Credentials = &neo4jv1beta1.CredentialsStatus{}
	}
	neo4j.Status.Credentials.SecretName = secretName
	neo4j.Status.Credentials.Generated = generated
}

func randomPassword(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// OwnedTypes returns types watched via Owns().
func OwnedTypes() []client.Object {
	return []client.Object{
		&appsv1.StatefulSet{},
		&corev1.Secret{},
		&corev1.ServiceAccount{},
		&rbacv1.Role{},
		&rbacv1.RoleBinding{},
	}
}
