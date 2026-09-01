package status

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
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
	setCondition(neo4j, oracle.ConditionReconciling, metav1.ConditionTrue, oracle.ReasonInProgress, "Reconciliation in progress")
	setCondition(neo4j, oracle.ConditionError, metav1.ConditionFalse, oracle.ReasonNoError, "")
}

// PipelineErrorReason maps a pipeline error to a catalogued reason (internal/oracle).
// Callers use it for both the Error condition and the matching Warning Event.
func PipelineErrorReason(err error) oracle.Reason {
	switch {
	case errors.Is(err, rendersecrets.ErrNotMountable):
		return oracle.ReasonSecretNotMountable
	case errors.Is(err, rendersecrets.ErrAuthNotDelegated):
		return oracle.ReasonSecretNotDelegated
	case errors.Is(err, rendersecrets.ErrAuthValueRejected):
		return oracle.ReasonAuthSecretInvalid
	case errors.Is(err, renderstorage.ErrTemplateDrift):
		return oracle.ReasonStorageTemplateDrift
	default:
		return oracle.ReasonReconcileFailed
	}
}

// MarkPipelineError records a reconcile failure.
func (w *Writer) MarkPipelineError(neo4j *neo4jv1beta1.Neo4j, err error) {
	setCondition(neo4j, oracle.ConditionError, metav1.ConditionTrue, PipelineErrorReason(err), err.Error())
	setCondition(neo4j, oracle.ConditionReady, metav1.ConditionFalse, oracle.ReasonReconcileError, err.Error())
	setCondition(neo4j, oracle.ConditionReconciling, metav1.ConditionFalse, oracle.ReasonFailed, err.Error())
	if rendertrust.TrustEnabled(neo4j) && isTLSSecretError(err) {
		setCondition(neo4j, oracle.ConditionTLSReady, metav1.ConditionFalse, oracle.ReasonSecretMissing, err.Error())
	}
	neo4j.Status.Phase = neo4jv1beta1.Neo4jPhaseFailed
}

// ObserveAndWrite refreshes status from the API server and patches status subresource.
func (w *Writer) ObserveAndWrite(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) error {
	log := ctrllog.FromContext(ctx).WithName("status").WithValues("domain", "status", "reconciler", "status")
	var ready, desired int32
	var anySTSFound, rolling bool
	storageReady := true
	storageReason, storageMsg := oracle.ReasonPVCBound, ""

	for _, pool := range render.ActivePools(neo4j) {
		ctxRender := render.ContextForPool(neo4j, pool)
		poolDesired := ctxRender.PoolReplicas()

		var sts appsv1.StatefulSet
		var liveReplicas int32
		stsKey := types.NamespacedName{Name: ctxRender.STSName(), Namespace: ctxRender.Namespace()}
		if w.Client.Get(ctx, stsKey, &sts) == nil {
			anySTSFound = true
			desired += poolDesired
			ready += sts.Status.ReadyReplicas
			if sts.Spec.Replicas != nil {
				liveReplicas = *sts.Spec.Replicas
			}
			if stsRolling(sts) {
				rolling = true
			}
			log.V(1).Info("observed statefulset",
				"pool", string(pool),
				"name", sts.Name,
				"readyReplicas", sts.Status.ReadyReplicas,
				"desiredReplicas", poolDesired,
				"rolling", stsRolling(sts),
			)
		}
		if ok, reason, msg := w.observePoolStorageReady(ctx, ctxRender, liveReplicas); !ok {
			storageReady = false
			// First failing pool wins — same pattern as TLS observation.
			if storageMsg == "" {
				storageReason, storageMsg = reason, msg
				log.Info("storage not ready", "pool", string(pool), "reason", reason.String(), "message", msg)
			}
		}
	}

	tlsReady, tlsReason, tlsMsg := w.observeTLSReady(ctx, neo4j)
	setCondition(neo4j, oracle.ConditionTLSReady, boolCondition(tlsReady), tlsReason, tlsMsg)

	setCondition(neo4j, oracle.ConditionInstalled, boolCondition(anySTSFound), installedReason(anySTSFound), "")
	neo4j.Status.ServerSummary = &neo4jv1beta1.ReplicaSummary{Servers: desired, Ready: ready}
	setCondition(neo4j, oracle.ConditionStorageReady, boolCondition(storageReady), storageReason, storageMsg)

	// Read once: it gates allReady just below, and it is also the signal that a scale-in the user
	// asked for is in flight, which decides the phase (ADR-004).
	drainPending := false
	if c := meta.FindStatusCondition(neo4j.Status.Conditions, oracle.ConditionServersPendingDrain.String()); c != nil {
		drainPending = c.Status == metav1.ConditionTrue
	}

	allReady := anySTSFound && ready == desired && desired > 0 && storageReady && tlsReady
	if render.IsClusterMode(neo4j) && allReady {
		if c := meta.FindStatusCondition(neo4j.Status.Conditions, oracle.ConditionClusterFormed.String()); c == nil || c.Status != metav1.ConditionTrue {
			allReady = false
		}
		if drainPending {
			allReady = false
		}
	}
	setCondition(neo4j, oracle.ConditionReconciling, metav1.ConditionFalse, oracle.ReasonCompleted, "")
	setCondition(neo4j, oracle.ConditionError, metav1.ConditionFalse, oracle.ReasonNoError, "")

	offline := offlineMode(neo4j)
	if offline {
		// Pods run a sleep loop (NotReady via Bolt readiness) — do not report Running/Ready.
		setCondition(neo4j, oracle.ConditionReady, metav1.ConditionFalse, oracle.ReasonOfflineMaintenance,
			"spec.maintenance.offlineMode is true; Neo4j process is not running")
	} else {
		setCondition(neo4j, oracle.ConditionReady, boolCondition(allReady), readyReason(allReady, tlsReady, storageReady), readyMessage(ready, desired))
		if allReady {
			neo4j.Status.Version = neo4j.Spec.Version
		}
	}
	changing := changeInFlight(neo4j, rolling, drainPending)
	// Status is passed whole and read before the assignment: the previously published phase and
	// version are what let the decision refuse to regress (ADR-004).
	neo4j.Status.Phase = nextPhase(neo4j.Status, offline, allReady, anySTSFound, changing)

	neo4j.Status.Endpoints = buildEndpoints(render.ClientServiceContext(neo4j))
	neo4j.Status.ObservedGeneration = neo4j.Generation

	log.Info("status update",
		"phase", neo4j.Status.Phase,
		"readyReplicas", ready,
		"desiredReplicas", desired,
		"storageReady", storageReady,
		"storageReason", storageReason.String(),
		"tlsReady", tlsReady,
		"allReady", allReady,
		"rolling", rolling,
		"drainPending", drainPending,
		"changeInFlight", changing,
	)
	return w.Client.Status().Update(ctx, neo4j)
}

// observePoolStorageReady reports claim readiness for one pool: every claim bound, one still behind
// the size the spec asks for, or one whose capacity has not caught up with its own request.
//
// It judges every claim the pool's StatefulSet owns, not just ordinal 0, and it compares sizes
// rather than stopping at Bound — a claim can be Bound and still be serving the old size, which is
// exactly the state a grow passes through and the state a StorageClass that forbids expansion
// leaves behind for good. replicas is the live StatefulSet's size so that a claim a scale-out has
// not created yet is not held against the CR; 0 means no StatefulSet, where ordinal 0 is what is
// expected next.
// ponytail: no StorageClass Get — V1 RBAC is namespace Role only (cluster-scoped SC needs ClusterRole).
// Surface storageClassName from the PVC/spec so describe shows why Pending.
func (w *Writer) observePoolStorageReady(ctx context.Context, ctxRender render.Context,
	replicas int32) (ok bool, reason oracle.Reason, message string) {
	claims := poolClaims(ctxRender, replicas)
	if len(claims) == 0 {
		// Existing.volume (raw VolumeSource) — no PVC to observe.
		return true, oracle.ReasonPVCBound, ""
	}
	var behind, resizing []string
	for _, claim := range claims {
		var pvc corev1.PersistentVolumeClaim
		key := types.NamespacedName{Name: claim.name, Namespace: ctxRender.Namespace()}
		if err := w.Client.Get(ctx, key, &pvc); err != nil {
			if apierrors.IsNotFound(err) {
				return false, oracle.ReasonPVCPending, fmt.Sprintf("waiting for PVC %q", claim.name)
			}
			return false, oracle.ReasonPVCPending, err.Error()
		}
		if pvc.Status.Phase != corev1.ClaimBound {
			return false, oracle.ReasonPVCPending, pendingMessage(claim.name, &pvc, ctxRender)
		}
		requested := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		if claim.want == nil || requested.IsZero() {
			// Nothing to hold the claim to, or a claim that records no request at all — there is no
			// size question to answer, only whether it is bound, which it is.
			continue
		}
		if claim.want.Cmp(requested) > 0 {
			behind = append(behind, fmt.Sprintf("%s requests %s", claim.name, requested.String()))
			continue
		}
		if actual := pvc.Status.Capacity[corev1.ResourceStorage]; actual.Cmp(requested) < 0 {
			resizing = append(resizing, fmt.Sprintf("%s at %s of %s", claim.name, actual.String(), requested.String()))
		}
	}
	switch {
	case len(behind) > 0:
		return false, oracle.ReasonStorageResizeFailed, fmt.Sprintf(
			"the spec asks for %s but %s; if this persists the StorageClass likely has allowVolumeExpansion: false — see the StorageResizeFailed Event",
			desiredDataSize(ctxRender), strings.Join(behind, ", "))
	case len(resizing) > 0:
		return false, oracle.ReasonStorageResizing, fmt.Sprintf(
			"volume expansion in flight: %s", strings.Join(resizing, ", "))
	}
	return true, oracle.ReasonPVCBound, ""
}

// claimWant is one PVC the pool should have, and the size the spec holds it to. want is nil for a
// claim the operator does not template — an Existing claimName, where binding is all there is to
// check.
type claimWant struct {
	name string
	want *resource.Quantity
}

// poolClaims names every claim a pool's StatefulSet owns right now. The rendered templates give the
// volumes and the replica count gives the ordinals, because a StatefulSet publishes no list of the
// claims it created.
func poolClaims(ctxRender render.Context, replicas int32) []claimWant {
	if replicas < 1 {
		replicas = 1
	}
	var out []claimWant
	for _, vct := range renderstorage.VolumeClaimTemplates(ctxRender) {
		var want *resource.Quantity
		if q, has := vct.Spec.Resources.Requests[corev1.ResourceStorage]; has {
			size := q
			want = &size
		}
		for ordinal := int32(0); ordinal < replicas; ordinal++ {
			out = append(out, claimWant{
				name: renderstorage.ClaimName(vct.Name, ctxRender.STSName(), ordinal),
				want: want,
			})
		}
	}
	// A data volume bound by Existing.claimName renders no template, so it would otherwise go
	// unobserved even though it is the volume Neo4j actually runs on.
	if name, ok := renderstorage.DataPVCLookup(ctxRender); ok && !named(out, name) {
		out = append(out, claimWant{name: name})
	}
	return out
}

func named(claims []claimWant, name string) bool {
	for _, c := range claims {
		if c.name == name {
			return true
		}
	}
	return false
}

func pendingMessage(pvcName string, pvc *corev1.PersistentVolumeClaim, ctxRender render.Context) string {
	if sc := storageClassNameOf(pvc, ctxRender); sc != "" {
		return fmt.Sprintf(
			"PVC %q is Pending (storageClassName=%q); ensure the StorageClass exists and the provisioner is healthy",
			pvcName, sc)
	}
	return fmt.Sprintf("PVC %q is Pending (no storageClassName set; waiting for a default StorageClass)", pvcName)
}

func desiredDataSize(ctxRender render.Context) string {
	if ctxRender.Neo4j.Spec.Storage == nil || ctxRender.Neo4j.Spec.Storage.Volumes == nil {
		return "the rendered size"
	}
	data := ctxRender.Neo4j.Spec.Storage.Volumes.Data
	if data.Dynamic != nil && data.Dynamic.Size != "" {
		return data.Dynamic.Size
	}
	return "the rendered size"
}

func storageClassNameOf(pvc *corev1.PersistentVolumeClaim, ctxRender render.Context) string {
	if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
		return *pvc.Spec.StorageClassName
	}
	return ctxRender.DataStorageClassName()
}

func (w *Writer) observeTLSReady(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) (ok bool, reason oracle.Reason, message string) {
	if !rendertrust.TrustEnabled(neo4j) {
		return true, oracle.ReasonTrustDisabled, "trust.enabled is false"
	}
	for _, name := range rendertrust.BYOSecretNames(neo4j) {
		var secret corev1.Secret
		if err := w.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: neo4j.Namespace}, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				return false, oracle.ReasonSecretMissing, fmt.Sprintf("trust secret %q not found", name)
			}
			return false, oracle.ReasonSecretMissing, err.Error()
		}
	}
	for _, need := range rendertrust.RequiredSecretKeys(neo4j) {
		var secret corev1.Secret
		if err := w.Client.Get(ctx, types.NamespacedName{Name: need.SecretName, Namespace: neo4j.Namespace}, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				return false, oracle.ReasonSecretMissing, fmt.Sprintf("trust secret %q not found", need.SecretName)
			}
			return false, oracle.ReasonSecretMissing, err.Error()
		}
		if secret.Data == nil || len(secret.Data[need.Key]) == 0 {
			return false, oracle.ReasonSecretMissing, fmt.Sprintf("trust secret %q missing data key %q", need.SecretName, need.Key)
		}
	}
	// cert-manager material gets its own reason: the Secret is operator-provisioned, so a
	// gap here means issuance is in flight rather than something the user must fix.
	for _, need := range rendertrust.ProvisionedSecretKeys(neo4j) {
		var secret corev1.Secret
		if err := w.Client.Get(ctx, types.NamespacedName{Name: need.SecretName, Namespace: neo4j.Namespace}, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				return false, oracle.ReasonCertificatePending, fmt.Sprintf(
					"waiting for cert-manager to issue certificate into secret %q", need.SecretName)
			}
			return false, oracle.ReasonCertificatePending, err.Error()
		}
		if secret.Data == nil || len(secret.Data[need.Key]) == 0 {
			return false, oracle.ReasonCertificatePending, fmt.Sprintf(
				"cert-manager secret %q does not carry %q yet", need.SecretName, need.Key)
		}
	}
	return true, oracle.ReasonSecretsPresent, "required TLS secrets and keys present"
}

func buildEndpoints(ctx render.Context) *neo4jv1beta1.EndpointsStatus {
	ns := ctx.Namespace()
	name := ctx.ClientServiceName()
	boltPort := ctx.ServiceFacadePort(render.ConnectorBolt)
	if boltPort == 0 {
		boltPort = ctx.BoltPort()
	}
	scheme := "neo4j"
	directScheme := "bolt"
	if rendertrust.BoltTLSEnabled(ctx.Neo4j) {
		scheme = "neo4j+s"
		directScheme = "bolt+s"
	}
	host := fmt.Sprintf("%s.%s.svc:%d", name, ns, boltPort)
	boltURI := fmt.Sprintf("%s://%s", scheme, host)
	ep := &neo4jv1beta1.EndpointsStatus{
		Bolt:     boltURI,
		Neo4j:    boltURI,
		Internal: fmt.Sprintf("%s.%s.svc:%d", ctx.HeadlessServiceName(), ns, ctx.BoltPort()),
		ConnectionExamples: &neo4jv1beta1.ConnectionExamples{
			BoltURI:     boltURI,
			Neo4jURI:    boltURI,
			PortForward: portForwardHint(ns, name, boltPort, directScheme),
		},
	}
	if ctx.HTTPEnabled() && clientExposes(ctx, render.ConnectorHTTP) {
		httpPort := ctx.ServiceFacadePort(render.ConnectorHTTP)
		if httpPort == 0 {
			httpPort = ctx.HTTPPort()
		}
		ep.HTTP = fmt.Sprintf("http://%s.%s.svc:%d", name, ns, httpPort)
	}
	if ctx.HTTPSEnabled() && clientExposes(ctx, render.ConnectorHTTPS) {
		httpsPort := ctx.ServiceFacadePort(render.ConnectorHTTPS)
		if httpsPort == 0 {
			httpsPort = ctx.HTTPSPort()
		}
		ep.HTTPS = fmt.Sprintf("https://%s.%s.svc:%d", name, ns, httpsPort)
	}
	return ep
}

func clientExposes(ctx render.Context, connector string) bool {
	for _, name := range ctx.ClientExpose() {
		if name == connector {
			return true
		}
	}
	return false
}

func portForwardHint(ns, svc string, boltPort int32, directScheme string) string {
	cmd := fmt.Sprintf("kubectl port-forward -n %s svc/%s %d:%d", ns, svc, boltPort, boltPort)
	if directScheme == "bolt+s" {
		return fmt.Sprintf("%s # then %s://127.0.0.1:%d (use bolt+s, not neo4j+s, over port-forward)", cmd, directScheme, boltPort)
	}
	return cmd
}

func isTLSSecretError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "trust secret") || strings.Contains(msg, "trust.certificates")
}

// setCondition takes catalogued values only: an undeclared reason cannot be built outside
// internal/oracle, so it cannot reach the status subresource (ADR-014).
func setCondition(neo4j *neo4jv1beta1.Neo4j, typ oracle.Condition, status metav1.ConditionStatus, reason oracle.Reason, message string) {
	meta.SetStatusCondition(&neo4j.Status.Conditions, metav1.Condition{
		Type:               typ.String(),
		Status:             status,
		Reason:             reason.String(),
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

func installedReason(ok bool) oracle.Reason {
	if ok {
		return oracle.ReasonObjectsCreated
	}
	return oracle.ReasonPending
}

// readyReason names what is holding Ready back, so the reason never contradicts the message: a
// storage problem on a pool whose members are all up would otherwise read MembersNotReady next to
// "1/1 servers ready".
func readyReason(ok, tlsReady, storageReady bool) oracle.Reason {
	switch {
	case ok:
		return oracle.ReasonAllMembersReady
	case !tlsReady:
		return oracle.ReasonTLSNotReady
	case !storageReady:
		return oracle.ReasonStorageNotReady
	default:
		return oracle.ReasonMembersNotReady
	}
}

func readyMessage(ready, desired int32) string {
	return fmt.Sprintf("%d/%d servers ready", ready, desired)
}

// nextPhase decides status.phase (ADR-004). Phase answers where the object is in its life and what
// the operator is doing; health is carried by Ready and the domain conditions, never by a phase
// downgrade. Hence the two ordering rules below: a workload that has already been ready never falls
// back to Provisioning or Bootstrapping, and a not-ready state the user asked for keeps Running.
func nextPhase(prior neo4jv1beta1.Neo4jStatus, offline, allReady, anySTSFound,
	changing bool) neo4jv1beta1.Neo4jPhase {
	switch {
	case offline:
		return neo4jv1beta1.Neo4jPhaseMaintenance
	case allReady:
		return neo4jv1beta1.Neo4jPhaseRunning
	case !anySTSFound:
		return neo4jv1beta1.Neo4jPhaseProvisioning
	case !established(prior):
		return neo4jv1beta1.Neo4jPhaseBootstrapping // never been ready — a genuine first install
	case changing:
		return neo4jv1beta1.Neo4jPhaseRunning // a roll, a scale or an upgrade we asked for
	default:
		return neo4jv1beta1.Neo4jPhaseDegraded // unplanned loss after the object was established
	}
}

// changeInFlight reports whether the workload is unsettled by something the user asked for rather
// than by something that went wrong: a spec change not yet absorbed, a StatefulSet still moving pods
// onto a new revision, or a scale-in waiting on Neo4j to release a member (ADR-007). This is the
// predicate that keeps a routine roll from being reported as a degradation.
func changeInFlight(neo4j *neo4jv1beta1.Neo4j, rolling, drainPending bool) bool {
	return neo4j.Generation != neo4j.Status.ObservedGeneration || rolling || drainPending
}

// established reports whether the workload has ever been fully ready, read from status.version:
// the writer sets it only under allReady, and unlike phase it survives the Failed value a transient
// pipeline error leaves behind — so a CR that has served is never called Bootstrapping again.
func established(prior neo4jv1beta1.Neo4jStatus) bool {
	return prior.Version != ""
}

// stsRolling reports whether a StatefulSet is still moving pods onto a new revision. Both revisions
// must be set: on a StatefulSet being created for the first time only the update revision exists,
// and that is an install, not a roll.
func stsRolling(sts appsv1.StatefulSet) bool {
	return sts.Status.CurrentRevision != "" && sts.Status.UpdateRevision != "" &&
		sts.Status.CurrentRevision != sts.Status.UpdateRevision
}

// IsReady reports whether the Ready condition is True.
func IsReady(neo4j *neo4jv1beta1.Neo4j) bool {
	for _, c := range neo4j.Status.Conditions {
		if c.Type == oracle.ConditionReady.String() {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}

// OfflineMode reports whether the CR requests offline maintenance (NEO-3-017-MNT-01).
func OfflineMode(neo4j *neo4jv1beta1.Neo4j) bool {
	return offlineMode(neo4j)
}

func offlineMode(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j.Spec.Maintenance != nil && neo4j.Spec.Maintenance.OfflineMode
}
