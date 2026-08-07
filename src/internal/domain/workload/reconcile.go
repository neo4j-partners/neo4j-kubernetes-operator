package workload

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/formation"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderwl "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
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
	log := ctrllog.FromContext(ctx)
	if err := renderwl.ValidateSecurity(neo4j); err != nil {
		log.Error(err, "workload security validation failed")
		return shared.Failed(err)
	}

	baseCtx := render.ContextForPool(neo4j, render.ActivePools(neo4j)[0])

	saDesired := renderwl.OperandServiceAccount(baseCtx)
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saDesired.Name, Namespace: saDesired.Namespace}}
	if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, sa, func() error {
		sa.Labels = saDesired.Labels
		sa.Annotations = saDesired.Annotations
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
		log.Error(err, "referenced auth secret missing", "secret", baseCtx.AuthSecretName())
		return shared.Failed(err)
	} else {
		log.Info("auth secret referenced", "secret", baseCtx.AuthSecretName())
	}

	if err := r.ensurePluginLicenseSecrets(ctx, neo4j); err != nil {
		return shared.Failed(err)
	}

	for _, pool := range render.ActivePools(neo4j) {
		ctxRender := render.ContextForPool(neo4j, pool)
		stsDesired := renderwl.PoolStatefulSet(ctxRender)
		sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: stsDesired.Name, Namespace: stsDesired.Namespace}}
		var existing appsv1.StatefulSet
		exists := r.Client.Get(ctx, types.NamespacedName{Name: stsDesired.Name, Namespace: stsDesired.Namespace}, &existing) == nil
		vcts := len(stsDesired.Spec.VolumeClaimTemplates)
		log.Info("reconciling statefulset",
			"pool", string(pool),
			"name", stsDesired.Name,
			"exists", exists,
			"desiredReplicas", ctxRender.PoolReplicas(),
			"volumeClaimTemplates", vcts,
			"image", ctxRender.ImageRef(),
		)
		if exists {
			log.V(1).Info("statefulset volumeClaimTemplates are immutable after create; PVC size/class changes require recreate",
				"pool", string(pool), "name", stsDesired.Name)
		}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, sts, func() error {
			sts.Labels = stsDesired.Labels
			// StatefulSet forbids changing serviceName, selector, volumeClaimTemplates,
			// podManagementPolicy after create — only patch mutable fields on update.
			if sts.CreationTimestamp.IsZero() {
				sts.Spec = stsDesired.Spec
				return nil
			}
			desired := int32(1)
			if stsDesired.Spec.Replicas != nil {
				desired = *stsDesired.Spec.Replicas
			}
			current := desired
			if sts.Spec.Replicas != nil {
				current = *sts.Spec.Replicas
			}
			effective := formation.EffectiveReplicas(neo4j, pool, desired, current)
			if effective != current {
				log.Info("statefulset replica change",
					"pool", string(pool),
					"name", stsDesired.Name,
					"from", current,
					"to", effective,
					"specDesired", desired,
				)
			}
			sts.Spec.Replicas = &effective
			sts.Spec.Template = stsDesired.Spec.Template
			sts.Spec.UpdateStrategy = stsDesired.Spec.UpdateStrategy
			sts.Spec.PersistentVolumeClaimRetentionPolicy = stsDesired.Spec.PersistentVolumeClaimRetentionPolicy
			return nil
		}); err != nil {
			return shared.Failed(err)
		}
	}

	if out := r.reconcilePDB(ctx, neo4j, baseCtx); out.Err != nil {
		return out
	}
	if out := r.reconcileNetworkPolicy(ctx, neo4j, baseCtx); out.Err != nil {
		return out
	}

	r.recordCredentials(neo4j, baseCtx.AuthSecretName(), generated)
	return shared.Done()
}

func (r *Reconciler) reconcilePDB(ctx context.Context, neo4j *neo4jv1beta1.Neo4j, baseCtx render.Context) shared.StepResult {
	log := ctrllog.FromContext(ctx)
	if !renderwl.PDBEnabled(neo4j) {
		log.V(1).Info("poddisruptionbudget disabled, ensure absent")
		return r.deletePDBIfPresent(ctx, neo4j, baseCtx)
	}
	desired := renderwl.PodDisruptionBudget(baseCtx)
	log.Info("reconciling poddisruptionbudget", "name", desired.Name)
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace},
	}
	if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, pdb, func() error {
		pdb.Labels = desired.Labels
		pdb.Spec = desired.Spec
		return nil
	}); err != nil {
		return shared.Failed(fmt.Errorf("apply PodDisruptionBudget: %w", err))
	}
	return shared.Done()
}

func (r *Reconciler) deletePDBIfPresent(ctx context.Context, neo4j *neo4jv1beta1.Neo4j, baseCtx render.Context) shared.StepResult {
	pdb := &policyv1.PodDisruptionBudget{}
	key := types.NamespacedName{Name: renderwl.PDBName(baseCtx), Namespace: neo4j.Namespace}
	if err := r.Client.Get(ctx, key, pdb); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return shared.Done()
		}
		return shared.Failed(fmt.Errorf("get PodDisruptionBudget for delete: %w", err))
	}
	if err := r.Client.Delete(ctx, pdb); err != nil && client.IgnoreNotFound(err) != nil {
		return shared.Failed(fmt.Errorf("delete PodDisruptionBudget: %w", err))
	}
	ctrllog.FromContext(ctx).Info("deleted poddisruptionbudget", "name", key.Name)
	return shared.Done()
}

func (r *Reconciler) reconcileNetworkPolicy(ctx context.Context, neo4j *neo4jv1beta1.Neo4j, baseCtx render.Context) shared.StepResult {
	log := ctrllog.FromContext(ctx)
	if !renderwl.NetworkPolicyEnabled(neo4j) {
		log.V(1).Info("networkpolicy disabled, ensure absent")
		return r.deleteNetworkPolicyIfPresent(ctx, neo4j, baseCtx)
	}
	desired := renderwl.NetworkPolicy(baseCtx)
	log.Info("reconciling networkpolicy", "name", desired.Name)
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace},
	}
	if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, np, func() error {
		np.Labels = desired.Labels
		np.Spec = desired.Spec
		return nil
	}); err != nil {
		return shared.Failed(fmt.Errorf("apply NetworkPolicy: %w", err))
	}
	return shared.Done()
}

func (r *Reconciler) deleteNetworkPolicyIfPresent(ctx context.Context, neo4j *neo4jv1beta1.Neo4j, baseCtx render.Context) shared.StepResult {
	np := &networkingv1.NetworkPolicy{}
	key := types.NamespacedName{Name: renderwl.NetworkPolicyName(baseCtx), Namespace: neo4j.Namespace}
	if err := r.Client.Get(ctx, key, np); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return shared.Done()
		}
		return shared.Failed(fmt.Errorf("get NetworkPolicy for delete: %w", err))
	}
	if err := r.Client.Delete(ctx, np); err != nil && client.IgnoreNotFound(err) != nil {
		return shared.Failed(fmt.Errorf("delete NetworkPolicy: %w", err))
	}
	ctrllog.FromContext(ctx).Info("deleted networkpolicy", "name", key.Name)
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
	log := ctrllog.FromContext(ctx)
	secretName := ctxRender.AuthSecretName()
	var existing corev1.Secret
	err := r.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: ctxRender.Namespace()}, &existing)
	if err == nil {
		log.Info("auth secret already exists", "secret", secretName, "action", "reuse")
		return "", nil
	}
	if !apierrors.IsNotFound(err) {
		return "", err
	}

	password, err := randomPassword(16)
	if err != nil {
		return "", fmt.Errorf("generate auth password: %w", err)
	}
	log.Info("generating auth secret", "secret", secretName, "action", "create")
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
		&policyv1.PodDisruptionBudget{},
		&networkingv1.NetworkPolicy{},
	}
}
