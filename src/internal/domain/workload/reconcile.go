package workload

import (
	"context"
	"crypto/rand"
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
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
	renderwl "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
)

// kubectlRestartedAt is what `kubectl rollout restart` writes. The operator
// replaces the pod template every reconcile, so it must copy this through or
// a manual restart is undone before any pod is replaced.
const kubectlRestartedAt = "kubectl.kubernetes.io/restartedAt"

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
	if err := renderwl.ValidateNetworkPolicy(neo4j); err != nil {
		log.Error(err, "networkPolicy validation failed")
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

	tlsSum := r.tlsChecksum(ctx, neo4j)
	for _, pool := range render.ActivePools(neo4j) {
		ctxRender := render.ContextForPool(neo4j, pool)
		stsDesired := renderwl.PoolStatefulSet(ctxRender)
		stampTLSChecksum(stsDesired, tlsSum)
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
			// volumeClaimTemplates are immutable after create, so a size change is applied to the
			// claims by the persistence step and the template stays behind — that difference is
			// expected and not drift. Anything else means the spec cannot be realised, and applying
			// the pod template alone is what would leave it mounting a volume no template backs.
			if drift := renderstorage.VolumeClaimDrift(stsDesired.Spec.VolumeClaimTemplates, existing.Spec.VolumeClaimTemplates); drift != "" {
				err := fmt.Errorf("%w: %s", renderstorage.ErrTemplateDrift, drift)
				log.Error(err, "refusing statefulset update", "pool", string(pool), "name", stsDesired.Name)
				return shared.Failed(err)
			}
			log.V(1).Info("statefulset volumeClaimTemplates are immutable after create; a size change is applied to the claims instead",
				"pool", string(pool), "name", stsDesired.Name)
		}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, sts, func() error {
			sts.Labels = stsDesired.Labels
			// StatefulSet forbids changing serviceName, selector, volumeClaimTemplates,
			// podManagementPolicy after create — only patch mutable fields on update.
			preserveRolloutRestart(&sts.Spec.Template, &stsDesired.Spec.Template)
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
	if err := renderwl.ValidatePDB(neo4j); err != nil {
		return shared.Failed(err)
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
	// ADD-05: name collision must not delete a foreign PDB.
	if !metav1.IsControlledBy(pdb, neo4j) {
		ctrllog.FromContext(ctx).V(1).Info("skip delete: PodDisruptionBudget not owned by this Neo4j", "name", key.Name)
		return shared.Done()
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
	// ADD-05: name collision must not delete a foreign NetworkPolicy.
	if !metav1.IsControlledBy(np, neo4j) {
		ctrllog.FromContext(ctx).V(1).Info("skip delete: NetworkPolicy not owned by this Neo4j", "name", key.Name)
		return shared.Done()
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
		// The Secret is reused as-is on every reconcile, so a hand-edited value would
		// crash-loop the pod with the cause buried in the container log.
		if err := rendersecrets.RequireUsableAuthValue(&existing); err != nil {
			return "", err
		}
		log.Info("auth secret already exists", "secret", secretName, "action", "reuse")
		return "", nil
	}
	if !apierrors.IsNotFound(err) {
		return "", err
	}

	password, err := randomPassword(generatedPasswordLength)
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
	return rendersecrets.RequireUsableAuthValue(&secret)
}

func (r *Reconciler) recordCredentials(neo4j *neo4jv1beta1.Neo4j, secretName string, generated bool) {
	if neo4j.Status.Credentials == nil {
		neo4j.Status.Credentials = &neo4jv1beta1.CredentialsStatus{}
	}
	neo4j.Status.Credentials.SecretName = secretName
	neo4j.Status.Credentials.Generated = generated
}

// Generated passwords are alphanumeric on purpose: the Neo4j image entrypoint feeds the
// value to `neo4j-admin dbms set-initial-password` as a positional argument with no "--"
// separator, so a leading "-" is parsed as an option and the container crash-loops, and it
// parses NEO4J_AUTH on "/" (rendersecrets.RequireUsableAuthValue). 24 symbols out of 62
// keep ~143 bits of entropy.
const (
	generatedPasswordLength   = 24
	generatedPasswordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

func randomPassword(n int) (string, error) {
	// Bytes at or above the largest whole multiple of the alphabet are redrawn, otherwise
	// the leading symbols would be slightly more likely than the rest.
	limit := byte(256 - 256%len(generatedPasswordAlphabet))
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b >= limit {
				continue
			}
			out = append(out, generatedPasswordAlphabet[int(b)%len(generatedPasswordAlphabet)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}

func (r *Reconciler) tlsChecksum(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) string {
	keys := rendertrust.MountedSecretKeys(neo4j)
	if len(keys) == 0 {
		return ""
	}
	ns := neo4j.Namespace
	return rendertrust.MaterialChecksum(keys, func(name, key string) []byte {
		var secret corev1.Secret
		if err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &secret); err != nil {
			return nil
		}
		return secret.Data[key]
	})
}

func stampTLSChecksum(sts *appsv1.StatefulSet, sum string) {
	if sum == "" {
		return
	}
	if sts.Spec.Template.Annotations == nil {
		sts.Spec.Template.Annotations = map[string]string{}
	}
	sts.Spec.Template.Annotations[rendertrust.ChecksumAnnotation] = sum
}

func preserveRolloutRestart(live, desired *corev1.PodTemplateSpec) {
	if live.Annotations == nil {
		return
	}
	if v := live.Annotations[kubectlRestartedAt]; v != "" {
		if desired.Annotations == nil {
			desired.Annotations = map[string]string{}
		}
		desired.Annotations[kubectlRestartedAt] = v
	}
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
