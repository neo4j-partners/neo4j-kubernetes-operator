package formation

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/persistence"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const requeueAfter = 15 * time.Second

// Reconciler runs ENABLE SERVER / drain+DROP (NEO-3-011-SRV-01, BDR-009, ADR-007).
type Reconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
	// Connect builds an Admin; nil → real Bolt driver.
	Connect func(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) (intneo4j.Admin, error)
}

func New(c client.Client, scheme *runtime.Scheme) *Reconciler {
	return &Reconciler{Client: c, Scheme: scheme}
}

func (r *Reconciler) Reconcile(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	log := ctrllog.FromContext(ctx)
	if !render.IsClusterMode(neo4j) || offlineMode(neo4j) {
		log.V(1).Info("formation skip", "cluster", render.IsClusterMode(neo4j), "offline", offlineMode(neo4j))
		clearFormationConditions(neo4j)
		return shared.Done()
	}

	connect := r.Connect
	if connect == nil {
		connect = r.defaultConnect
	}
	admin, err := connect(ctx, neo4j)
	if err != nil {
		// Cluster not reachable yet (formation / auth lag).
		log.Info("bolt unavailable, requeue", "err", err.Error())
		setCondition(neo4j, ConditionClusterFormed, metav1.ConditionFalse, "BoltUnavailable", err.Error())
		return shared.Requeue(requeueAfter)
	}
	defer func() { _ = admin.Close(ctx) }()

	servers, err := admin.ShowServers(ctx)
	if err != nil {
		log.Info("show servers failed, requeue", "err", err.Error())
		setCondition(neo4j, ConditionClusterFormed, metav1.ConditionFalse, "ShowServersFailed", err.Error())
		return shared.Requeue(requeueAfter)
	}
	log.V(1).Info("show servers", "count", len(servers))

	pendingDrain := false
	annotDirty := false

	// Hold primary STS / ENABLE when system still has a single primary but CR asks for more.
	// Deploying at 1 (+ analytics/read) is fine; growing 1→N is not automated.
	systemScaleBlocked, capDirty, err := r.syncSystemPrimaryCap(ctx, admin, neo4j)
	if err != nil {
		return adminErrResult(neo4j, err)
	}
	if capDirty {
		annotDirty = true
	}
	if systemScaleBlocked {
		log.Info("system primary scale-out blocked", "reason", "UnsupportedSystemScaleUp")
	}

	scalingIn := false
	for _, pool := range render.ActivePools(neo4j) {
		ctxPool := render.ContextForPool(neo4j, pool)
		var sts appsv1.StatefulSet
		if err := r.Client.Get(ctx, types.NamespacedName{Name: ctxPool.STSName(), Namespace: ctxPool.Namespace()}, &sts); err != nil {
			return shared.Requeue(requeueAfter)
		}
		current := int32(0)
		if sts.Spec.Replicas != nil {
			current = *sts.Spec.Replicas
		}
		if current > ctxPool.PoolReplicas() {
			log.Info("scale-in detected", "pool", string(pool), "stsReplicas", current, "desired", ctxPool.PoolReplicas())
			scalingIn = true
			break
		}
	}

	// Shrink DB topologies to fit remaining servers before DEALLOCATE (else Neo4j ArgumentError).
	if scalingIn {
		ok, err := r.ensureDatabaseTopologies(ctx, admin, neo4j, true)
		if err != nil {
			if isUnsupportedSinglePrimary(err) {
				log.Error(err, "scale-in blocked", "reason", "UnsupportedSinglePrimary")
				setCondition(neo4j, ConditionServersPendingDrain, metav1.ConditionTrue, "UnsupportedSinglePrimary",
					err.Error())
				setCondition(neo4j, ConditionClusterFormed, metav1.ConditionFalse, "UnsupportedSinglePrimary",
					err.Error())
				// Do not Failed — Neo4j cannot ALTER multi→1; leave STS held and wait for spec fix.
				return shared.Requeue(5 * time.Minute)
			}
			return adminErrResult(neo4j, err)
		}
		if !ok {
			log.Info("shrinking database topologies for scale-in")
			setCondition(neo4j, ConditionServersPendingDrain, metav1.ConditionTrue, "ShrinkingTopology",
				"reducing database topologies to fit scale-in")
			return shared.Requeue(requeueAfter)
		}
	}

	for _, pool := range render.ActivePools(neo4j) {
		ctxPool := render.ContextForPool(neo4j, pool)
		desired := ctxPool.PoolReplicas()

		var sts appsv1.StatefulSet
		stsKey := types.NamespacedName{Name: ctxPool.STSName(), Namespace: ctxPool.Namespace()}
		if err := r.Client.Get(ctx, stsKey, &sts); err != nil {
			return shared.Requeue(requeueAfter)
		}
		current := int32(0)
		if sts.Spec.Replicas != nil {
			current = *sts.Spec.Replicas
		}

		// Scale-in: drain/drop tail before allowing STS shrink.
		if current > desired {
			pendingDrain = true
			for _, m := range TailMembers(neo4j, pool, current) {
				log.Info("draining member", "pool", string(pool), "pod", m.PodName, "ordinal", m.Ordinal)
				done, err := r.ensureDropped(ctx, admin, servers, m)
				if err != nil {
					return adminErrResult(neo4j, err)
				}
				if !done {
					setCondition(neo4j, ConditionServersPendingDrain, metav1.ConditionTrue, "Draining",
						fmt.Sprintf("draining %s", m.PodName))
					return shared.Requeue(requeueAfter)
				}
				log.Info("member drained/dropped", "pool", string(pool), "pod", m.PodName)
				// Refresh view after drop.
				servers, err = admin.ShowServers(ctx)
				if err != nil {
					return shared.Requeue(requeueAfter)
				}
			}
			before := neo4j.Annotations[DrainOKAnnotation]
			SetDrainOK(neo4j, pool, desired, false)
			if neo4j.Annotations[DrainOKAnnotation] != before {
				annotDirty = true
			}
		} else {
			// Not scaling down this pool — clear stale drain-ok if present for other targets.
			ok := ParseDrainOK(neo4j)[string(pool)]
			if ok != 0 && ok != desired {
				SetDrainOK(neo4j, pool, 0, true)
				annotDirty = true
			}
			// Dropped server UUIDs cannot rejoin — wipe retained Dynamic PVCs for ordinals >= desired.
			if err := persistence.WipeStaleMemberPVCs(ctx, r.Client, neo4j, pool, desired); err != nil {
				return shared.Failed(err)
			}
		}
	}

	if annotDirty {
		if err := r.Client.Update(ctx, neo4j); err != nil {
			return shared.Failed(err)
		}
		return shared.Requeue(time.Second) // let workload apply shrink
	}

	if pendingDrain {
		// All tails dropped and annotation written on prior pass; wait for STS shrink.
		log.Info("drain complete, awaiting statefulset shrink")
		setCondition(neo4j, ConditionServersPendingDrain, metav1.ConditionTrue, "AwaitingSTSShrink",
			"Neo4j drain complete; waiting for StatefulSet scale-down")
	} else {
		setCondition(neo4j, ConditionServersPendingDrain, metav1.ConditionFalse, "NoDrain", "")
	}

	// Scale-out / heal: enable every desired member (all Free in this pass).
	// Skip extra PRIMARY ordinals while system is still single-primary (scale-out unsupported).
	pendingEnable := false
	for _, m := range DesiredMembers(neo4j) {
		if systemScaleBlocked && m.Pool == render.PoolPrimary && m.Ordinal >= 1 {
			continue
		}
		ok, err := r.ensureEnabled(ctx, admin, neo4j, servers, m)
		if err != nil {
			return adminErrResult(neo4j, err)
		}
		if !ok {
			pendingEnable = true
			log.Info("enabling server", "pool", string(m.Pool), "pod", m.PodName, "ordinal", m.Ordinal)
			setCondition(neo4j, ConditionClusterFormed, metav1.ConditionFalse, "EnablingServer",
				fmt.Sprintf("enabling %s", m.PodName))
		}
		servers, err = admin.ShowServers(ctx)
		if err != nil {
			return shared.Requeue(requeueAfter)
		}
	}
	if pendingEnable {
		return shared.Requeue(requeueAfter)
	}

	if systemScaleBlocked {
		setCondition(neo4j, ConditionClusterFormed, metav1.ConditionFalse, "UnsupportedSystemScaleUp",
			"system has 1 primary; growing to multiple system primaries is not supported (bootstrap with primaries.members at the final size, typically 3). Scaling secondaries only is fine.")
		return shared.Requeue(5 * time.Minute)
	}

	// Align standard DB topologies to defaultPrimariesCount + secondary pools.
	if !scalingIn {
		ok, err := r.ensureDatabaseTopologies(ctx, admin, neo4j, false)
		if err != nil {
			if isUnsupportedSinglePrimary(err) {
				setCondition(neo4j, ConditionClusterFormed, metav1.ConditionFalse, "UnsupportedSinglePrimary", err.Error())
				return shared.Requeue(5 * time.Minute)
			}
			return adminErrResult(neo4j, err)
		}
		if !ok {
			log.Info("aligning database topologies")
			setCondition(neo4j, ConditionClusterFormed, metav1.ConditionFalse, "AligningTopology",
				"altering database topologies to match primary/secondary pool sizes")
			return shared.Requeue(requeueAfter)
		}
	}

	min := render.ClientServiceContext(neo4j).MinimumMembers()
	enabledPrimaries := countEnabledPrimaries(neo4j, servers)
	if enabledPrimaries < min {
		log.Info("waiting for primary quorum", "enabledPrimaries", enabledPrimaries, "minimumMembers", min)
		setCondition(neo4j, ConditionClusterFormed, metav1.ConditionFalse, "WaitingQuorum",
			fmt.Sprintf("enabled primaries %d < minimumMembers %d", enabledPrimaries, min))
		return shared.Requeue(requeueAfter)
	}

	log.Info("cluster formed", "enabledPrimaries", enabledPrimaries, "minimumMembers", min)
	setCondition(neo4j, ConditionClusterFormed, metav1.ConditionTrue, "Formed", "All desired servers enabled")
	return shared.Done()
}

// syncSystemPrimaryCap holds primary STS at 1 when system still has a single primary
// but the CR asks for more. Deploying at 1 is supported; scale-out 1→N is not.
func (r *Reconciler) syncSystemPrimaryCap(ctx context.Context, admin intneo4j.Admin, neo4j *neo4jv1beta1.Neo4j) (blocked, annotDirty bool, err error) {
	desired := render.ContextForPool(neo4j, render.PoolPrimary).PoolReplicas()
	if desired <= 1 {
		if _, ok := PrimaryReplicasCap(neo4j); ok {
			SetPrimaryReplicasCap(neo4j, 0, true)
			return false, true, nil
		}
		return false, false, nil
	}
	dbs, err := admin.ShowDatabaseTopologies(ctx)
	if err != nil {
		return false, false, err
	}
	sys, ok := findSystemTopology(dbs)
	if !ok || sys.CurrentPrimaries != 1 {
		if _, had := PrimaryReplicasCap(neo4j); had {
			SetPrimaryReplicasCap(neo4j, 0, true)
			return false, true, nil
		}
		return false, false, nil
	}
	before, had := PrimaryReplicasCap(neo4j)
	SetPrimaryReplicasCap(neo4j, 1, false)
	return true, !had || before != 1, nil
}

func findSystemTopology(dbs []intneo4j.DatabaseTopology) (intneo4j.DatabaseTopology, bool) {
	for _, db := range dbs {
		if strings.EqualFold(db.Name, "system") {
			return db, true
		}
	}
	return intneo4j.DatabaseTopology{}, false
}

func (r *Reconciler) ensureEnabled(ctx context.Context, admin intneo4j.Admin, neo4j *neo4jv1beta1.Neo4j, servers []intneo4j.Server, m Member) (bool, error) {
	s, found := intneo4j.FindActiveByAddress(servers, m.BoltAddress)
	if !found {
		// Dropped/Deallocated identity remounted from an old PVC — recycle for a new UUID.
		if old, ok := intneo4j.FindByAddress(servers, m.BoltAddress); ok && intneo4j.IsTerminalRemoval(old.State) {
			if err := persistence.RecycleMemberStore(ctx, r.Client, neo4j, m.Pool, m.Ordinal, m.PodName); err != nil {
				return false, fmt.Errorf("recycle dropped member %s: %w", m.PodName, err)
			}
		}
		return false, nil
	}
	if intneo4j.IsEnabled(s.State) {
		return true, nil
	}
	if !intneo4j.IsFree(s.State) {
		if intneo4j.IsDeallocating(s.State) || intneo4j.IsTerminalRemoval(s.State) {
			return false, nil
		}
	}
	if err := admin.EnableServer(ctx, s.Name, m.ModeConstraint); err != nil {
		if strings.Contains(err.Error(), "deallocated or dropped") {
			if err2 := persistence.RecycleMemberStore(ctx, r.Client, neo4j, m.Pool, m.Ordinal, m.PodName); err2 != nil {
				return false, fmt.Errorf("recycle after ENABLE reject %s: %w", m.PodName, err2)
			}
			return false, nil
		}
		// OPTIONS may be rejected depending on Neo4j version — fall back to plain ENABLE.
		if m.ModeConstraint == "" {
			return false, fmt.Errorf("ENABLE SERVER %s: %w", s.Name, err)
		}
		if err2 := admin.EnableServer(ctx, s.Name, ""); err2 != nil {
			if strings.Contains(err2.Error(), "deallocated or dropped") {
				if err3 := persistence.RecycleMemberStore(ctx, r.Client, neo4j, m.Pool, m.Ordinal, m.PodName); err3 != nil {
					return false, fmt.Errorf("recycle after ENABLE reject %s: %w", m.PodName, err3)
				}
				return false, nil
			}
			return false, fmt.Errorf("ENABLE SERVER %s: %w (without options: %v)", s.Name, err, err2)
		}
	}
	return false, nil // requeue to confirm Enabled
}

func (r *Reconciler) ensureDropped(ctx context.Context, admin intneo4j.Admin, servers []intneo4j.Server, m Member) (bool, error) {
	s, found := intneo4j.FindByAddress(servers, m.BoltAddress)
	if !found {
		return true, nil // already gone from Neo4j
	}
	if intneo4j.IsDropped(s.State) {
		return true, nil // DROP already done; entry may linger in SHOW SERVERS
	}
	if intneo4j.IsDeallocated(s.State) {
		if err := admin.DropServer(ctx, s.Name); err != nil {
			if strings.Contains(err.Error(), "already dropped") {
				return true, nil
			}
			return false, fmt.Errorf("DROP SERVER %s: %w", s.Name, err)
		}
		return false, nil
	}
	if intneo4j.IsDeallocating(s.State) {
		return false, nil
	}
	// Enabled / Free still hosting — deallocate.
	if err := admin.DeallocateDatabases(ctx, s.Name); err != nil {
		return false, fmt.Errorf("DEALLOCATE DATABASES FROM SERVER %s: %w", s.Name, err)
	}
	return false, nil
}

// ensureDatabaseTopologies aligns standard databases to topology.defaultPrimariesCount
// (not primaries.members) and analytics+read pool sizes for secondaries.
// On scale-in, only shrinks primaries when they exceed the remaining primary pool
// (so defaultPrimariesCount=1 does not block draining servers after a manual ALTER to 3).
// Skips system/composite.
func (r *Reconciler) ensureDatabaseTopologies(ctx context.Context, admin intneo4j.Admin, neo4j *neo4jv1beta1.Neo4j, scalingIn bool) (bool, error) {
	targetP, targetS, poolP := desiredHostingCapacity(neo4j)
	dbs, err := admin.ShowDatabaseTopologies(ctx)
	if err != nil {
		return false, err
	}
	pending := false
	for _, db := range dbs {
		if !db.HasTopology {
			continue
		}
		typ := strings.ToLower(db.Type)
		if typ == "system" || typ == "composite" {
			continue
		}
		wantP, wantS := targetP, targetS
		if scalingIn {
			// Fit remaining servers only — do not force toward defaultPrimariesCount.
			if db.RequestedPrimaries > poolP {
				wantP = poolP
			} else {
				wantP = db.RequestedPrimaries
			}
			if db.RequestedSecondaries > targetS {
				wantS = targetS
			} else {
				wantS = db.RequestedSecondaries
			}
		}
		if wantP < 1 {
			wantP = 1
		}
		// Neo4j forbids ALTER DATABASE from multiple primaries → 1 primary (Raft quorum).
		if wantP < 2 && db.RequestedPrimaries > 1 {
			return false, fmt.Errorf("%w: database %q has %d primaries; set topology.defaultPrimariesCount >= 3 before shrinking hosting, or recreate the database manually (dbms.cluster.recreateDatabase)",
				errUnsupportedSinglePrimary, db.Name, db.RequestedPrimaries)
		}
		if wantP != db.RequestedPrimaries || wantS != db.RequestedSecondaries {
			if err := admin.SetDatabaseTopology(ctx, db.Name, wantP, wantS); err != nil {
				if isUnsupportedSinglePrimary(err) || strings.Contains(err.Error(), "multiple primaries to one primary") {
					return false, fmt.Errorf("%w: %v", errUnsupportedSinglePrimary, err)
				}
				return false, fmt.Errorf("ALTER DATABASE %s SET TOPOLOGY: %w", db.Name, err)
			}
			pending = true
			continue
		}
		if db.CurrentPrimaries != wantP || db.CurrentSecondaries != wantS {
			pending = true
		}
	}
	return !pending, nil
}

var errUnsupportedSinglePrimary = fmt.Errorf("Neo4j cannot automatically shrink a multi-primary database to one primary")

func isUnsupportedSinglePrimary(err error) bool {
	return err != nil && (strings.Contains(err.Error(), errUnsupportedSinglePrimary.Error()) ||
		strings.Contains(err.Error(), "multiple primaries to one primary"))
}

// desiredHostingCapacity returns steady-state ALTER DATABASE targets for standard DBs.
// Primaries follow topology.defaultPrimariesCount (default 1), clamped to the primary
// pool size. Secondaries follow analytics+read pool members. poolP is the primary
// pool ceiling used on scale-in to fit remaining servers.
func desiredHostingCapacity(neo4j *neo4jv1beta1.Neo4j) (primaries, secondaries, poolP int64) {
	poolP = int64(render.ContextForPool(neo4j, render.PoolPrimary).PoolReplicas())
	if cap, ok := PrimaryReplicasCap(neo4j); ok && int64(cap) < poolP {
		poolP = int64(cap)
	}
	primaries = int64(render.ClientServiceContext(neo4j).DefaultPrimariesCount())
	if primaries > poolP {
		primaries = poolP
	}
	if primaries < 1 {
		primaries = 1
	}
	for _, pool := range []render.PoolID{render.PoolAnalytics, render.PoolRead} {
		secondaries += int64(render.ContextForPool(neo4j, pool).PoolReplicas())
	}
	return
}

func (r *Reconciler) defaultConnect(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
	ctxRender := render.ClientServiceContext(neo4j)
	var secret corev1.Secret
	key := types.NamespacedName{Name: ctxRender.AuthSecretName(), Namespace: ctxRender.Namespace()}
	if err := r.Client.Get(ctx, key, &secret); err != nil {
		return nil, fmt.Errorf("auth secret: %w", err)
	}
	raw := string(secret.Data["NEO4J_AUTH"])
	user, pass, err := intneo4j.ParseAuthSecret(raw)
	if err != nil {
		return nil, err
	}
	return intneo4j.Connect(ctx, AdminBoltURI(neo4j), user, pass, BoltTLSEnabled(neo4j))
}

func countEnabledPrimaries(neo4j *neo4jv1beta1.Neo4j, servers []intneo4j.Server) int32 {
	var n int32
	ctx := render.ContextForPool(neo4j, render.PoolPrimary)
	for o := int32(0); o < ctx.PoolReplicas(); o++ {
		m := memberAt(ctx, render.PoolPrimary, o, "PRIMARY")
		if s, ok := intneo4j.FindByAddress(servers, m.BoltAddress); ok && intneo4j.IsEnabled(s.State) {
			n++
		}
	}
	return n
}

func setCondition(neo4j *neo4jv1beta1.Neo4j, ctype string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&neo4j.Status.Conditions, metav1.Condition{
		Type:               ctype,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: neo4j.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func clearFormationConditions(neo4j *neo4jv1beta1.Neo4j) {
	meta.RemoveStatusCondition(&neo4j.Status.Conditions, ConditionServersPendingDrain)
	meta.RemoveStatusCondition(&neo4j.Status.Conditions, ConditionClusterFormed)
}

func offlineMode(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j.Spec.Maintenance != nil && neo4j.Spec.Maintenance.OfflineMode
}

func adminErrResult(neo4j *neo4jv1beta1.Neo4j, err error) shared.StepResult {
	if isRetryableAdmin(err) {
		setCondition(neo4j, ConditionClusterFormed, metav1.ConditionFalse, "WaitingSystemLeader", err.Error())
		return shared.Requeue(requeueAfter)
	}
	return shared.Failed(err)
}

func isRetryableAdmin(err error) bool {
	s := err.Error()
	return strings.Contains(s, "NotALeader") ||
		strings.Contains(s, "TransactionExecutionLimit") ||
		strings.Contains(s, "ForbiddenOnReadOnlyDatabase") ||
		strings.Contains(s, "Unable to reallocate") ||
		strings.Contains(s, "Required topology")
}
