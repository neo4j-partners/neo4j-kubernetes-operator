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
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/persistence"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/events"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
)

const requeueAfter = 15 * time.Second

// Reconciler runs ENABLE SERVER / drain+DROP (NEO-3-011-SRV-01, BDR-009, ADR-007).
type Reconciler struct {
	Client   client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	// Connect builds an Admin; nil → real Bolt driver.
	Connect func(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) (intneo4j.Admin, error)
	// advisories keeps the NEO-004 dial warnings to one Event per generation, so they do not spend
	// the object's Event budget that ReasonDatabaseTopologyResized needs. Zero value is usable, so
	// every construction path gets it, tests included.
	advisories events.Advisory
}

func New(c client.Client, scheme *runtime.Scheme, recorder record.EventRecorder) *Reconciler {
	return &Reconciler{Client: c, Scheme: scheme, Recorder: recorder}
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
		// A declared gate above the declared pool is fatal at first bootstrap: Neo4j waits for
		// primaries that will never appear, so nothing answers on Bolt. Say so instead of the generic
		// lag message — admission only catches it when webhooks are enabled. Only the explicit field
		// is tested, since the derived gate cannot outgrow a healthy pool. Harmless once formed: a
		// running cluster answers and never reaches this branch.
		gate, pool := neo4j.Spec.Topology.MinimumMembers, neo4j.Spec.Topology.Primaries
		if gate != nil && pool != nil && *gate > pool.Members {
			msg := fmt.Sprintf("topology.minimumMembers %d exceeds the %d primaries in the pool: the system database cannot bootstrap, so Bolt never answers (%v)", *gate, pool.Members, err)
			log.Info("bootstrap gate above pool, requeue", "bootstrapGate", *gate, "primaryPool", pool.Members)
			setCondition(neo4j, oracle.ConditionClusterFormed, metav1.ConditionFalse, oracle.ReasonBootstrapGateTooHigh, msg)
			return shared.Requeue(requeueAfter)
		}
		// Cluster not reachable yet (formation / auth lag).
		log.Info("bolt unavailable, requeue", "err", err.Error())
		setCondition(neo4j, oracle.ConditionClusterFormed, metav1.ConditionFalse, oracle.ReasonBoltUnavailable, err.Error())
		return shared.Requeue(requeueAfter)
	}
	defer func() { _ = admin.Close(ctx) }()

	servers, err := admin.ShowServers(ctx)
	if err != nil {
		log.Info("show servers failed, requeue", "err", err.Error())
		setCondition(neo4j, oracle.ConditionClusterFormed, metav1.ConditionFalse, oracle.ReasonShowServersFailed, err.Error())
		return shared.Requeue(requeueAfter)
	}
	log.V(1).Info("show servers", "count", len(servers))

	pendingDrain := false
	statusDirty := false

	// Hold primary STS / ENABLE when system still has a single primary but CR asks for more.
	// Deploying at 1 (+ analytics/read) is fine; growing 1→N is not automated.
	systemScaleBlocked, capDirty, err := r.syncSystemPrimaryCap(ctx, admin, neo4j)
	if err != nil {
		return adminErrResult(neo4j, err)
	}
	if capDirty {
		statusDirty = true
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
	// The same read serves ensureDropped: it names the databases whose presence in a member's
	// hosting list does not mean the member still owes anyone data.
	var drainExempt map[string]bool
	if scalingIn {
		dbs, err := admin.ShowDatabaseTopologies(ctx)
		if err != nil {
			return adminErrResult(neo4j, err)
		}
		drainExempt = drainExemptDatabases(dbs)
		ok, err := r.ensureDatabaseTopologies(ctx, admin, neo4j, dbs)
		if err != nil {
			if isUnsupportedSinglePrimary(err) {
				log.Error(err, "scale-in blocked", "reason", oracle.ReasonUnsupportedSinglePrimary.String())
				setCondition(neo4j, oracle.ConditionServersPendingDrain, metav1.ConditionTrue, oracle.ReasonUnsupportedSinglePrimary,
					err.Error())
				setCondition(neo4j, oracle.ConditionClusterFormed, metav1.ConditionFalse, oracle.ReasonUnsupportedSinglePrimary,
					err.Error())
				// Do not Failed — Neo4j cannot ALTER multi→1; leave STS held and wait for spec fix.
				return shared.Requeue(5 * time.Minute)
			}
			return adminErrResult(neo4j, err)
		}
		if !ok {
			log.Info("shrinking database topologies for scale-in")
			setCondition(neo4j, oracle.ConditionServersPendingDrain, metav1.ConditionTrue, oracle.ReasonShrinkingTopology,
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
				done, err := r.ensureDropped(ctx, admin, servers, m, drainExempt)
				if err != nil {
					return adminErrResult(neo4j, err)
				}
				if !done {
					if waited := drainWaited(neo4j); waited > drainBudget {
						return r.reportDrainTimeout(ctx, neo4j, servers, m, waited)
					}
					setCondition(neo4j, oracle.ConditionServersPendingDrain, metav1.ConditionTrue, oracle.ReasonDraining,
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
			before := cloneDrainOK(neo4j)
			beforeGen := neo4j.Status.DrainOKGeneration
			SetDrainOK(neo4j, pool, desired, false)
			if !drainOKEqual(before, neo4j.Status.DrainOK) || beforeGen != neo4j.Status.DrainOKGeneration {
				statusDirty = true
			}
		} else {
			// Not scaling down this pool — clear stale drain-ok (wrong target or stale generation).
			raw := int32(0)
			if neo4j.Status.DrainOK != nil {
				raw = neo4j.Status.DrainOK[string(pool)]
			}
			if raw != 0 && (neo4j.Status.DrainOKGeneration != neo4j.Generation || raw != desired) {
				SetDrainOK(neo4j, pool, 0, true)
				statusDirty = true
			}
			// Dropped server UUIDs cannot rejoin — wipe Dynamic PVCs for ordinals >= desired
			// only when whenScaled=Delete (NEO-007).
			if err := persistence.WipeStaleMemberPVCs(ctx, r.Client, neo4j, pool, desired); err != nil {
				return shared.Failed(err)
			}
		}
	}

	if statusDirty {
		if err := r.Client.Status().Update(ctx, neo4j); err != nil {
			return shared.Failed(err)
		}
		return shared.Requeue(time.Second) // let workload apply shrink
	}

	if pendingDrain {
		// All tails dropped and status.drainOK written on prior pass; wait for STS shrink.
		log.Info("drain complete, awaiting statefulset shrink")
		setCondition(neo4j, oracle.ConditionServersPendingDrain, metav1.ConditionTrue, oracle.ReasonAwaitingSTSShrink,
			"Neo4j drain complete; waiting for StatefulSet scale-down")
	} else {
		setCondition(neo4j, oracle.ConditionServersPendingDrain, metav1.ConditionFalse, oracle.ReasonNoDrain, "")
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
			setCondition(neo4j, oracle.ConditionClusterFormed, metav1.ConditionFalse, oracle.ReasonEnablingServer,
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
		setCondition(neo4j, oracle.ConditionClusterFormed, metav1.ConditionFalse, oracle.ReasonUnsupportedSystemScaleUp,
			"system has 1 primary; growing to multiple system primaries is not supported (bootstrap with primaries.members at the final size, typically 3). Scaling secondaries only is fine.")
		return shared.Requeue(5 * time.Minute)
	}

	// No steady-state topology pass here on purpose: existing databases are left exactly as their
	// owner declared them, and the scale-in path above is the only place the operator rewrites one.
	// topology.defaultPrimariesCount only decides what a database gets when it is created, which is
	// Neo4j's own default to hold — see applyDefaultAllocation below.
	// Secondary check only: every desired member is already enabled by the time we get here, so this
	// guards against a pool that reports fewer primaries than the system database needs to exist.
	// The gate is capped by the pool because topology.minimumMembers is immutable and a later
	// scale-in may leave it above the pool — it was a bootstrap bar, not a permanent floor.
	min := systemQuorumFloor(neo4j)
	enabledPrimaries := countEnabledPrimaries(neo4j, servers)
	if enabledPrimaries < min {
		log.Info("waiting for primary quorum", "enabledPrimaries", enabledPrimaries, "bootstrapGate", min)
		setCondition(neo4j, oracle.ConditionClusterFormed, metav1.ConditionFalse, oracle.ReasonWaitingQuorum,
			fmt.Sprintf("enabled primaries %d, the system database needs %d", enabledPrimaries, min))
		return shared.Requeue(requeueAfter)
	}

	log.Info("cluster formed", "enabledPrimaries", enabledPrimaries, "bootstrapGate", min)
	setCondition(neo4j, oracle.ConditionClusterFormed, metav1.ConditionTrue, oracle.ReasonFormed, "All desired servers enabled")

	// Only past quorum: the creation defaults live in the system database, which needs a leader.
	// A failure is retried rather than surfaced — it leaves the DBMS defaults on their previous
	// value and unforms nothing.
	if err := r.applyDefaultAllocation(ctx, admin, neo4j); err != nil {
		log.Info("creation defaults not applied, will retry", "err", err.Error())
		return shared.Requeue(requeueAfter)
	}
	return shared.Done()
}

// syncSystemPrimaryCap holds primary STS at 1 when system still has a single primary
// but the CR asks for more. Deploying at 1 is supported; scale-out 1→N is not.
func (r *Reconciler) syncSystemPrimaryCap(ctx context.Context, admin intneo4j.Admin, neo4j *neo4jv1beta1.Neo4j) (blocked, statusDirty bool, err error) {
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

func cloneDrainOK(neo4j *neo4jv1beta1.Neo4j) map[string]int32 {
	if neo4j.Status.DrainOK == nil {
		return nil
	}
	out := make(map[string]int32, len(neo4j.Status.DrainOK))
	for k, v := range neo4j.Status.DrainOK {
		out[k] = v
	}
	return out
}

func drainOKEqual(a, b map[string]int32) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
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

// ensureDropped drives one departing member out of the Neo4j cluster view: DEALLOCATE, then DROP
// once the member has handed its databases over. exempt names the databases whose presence in a
// hosting list does not mean the member still owes anyone data.
func (r *Reconciler) ensureDropped(ctx context.Context, admin intneo4j.Admin, servers []intneo4j.Server,
	m Member, exempt map[string]bool) (bool, error) {
	s, found := intneo4j.FindByAddress(servers, m.BoltAddress)
	if !found {
		return true, nil // already gone from Neo4j
	}
	if intneo4j.IsDropped(s.State) {
		return true, nil // DROP already done; entry may linger in SHOW SERVERS
	}
	if intneo4j.IsDeallocated(s.State) {
		return dropServer(ctx, admin, s)
	}
	if intneo4j.IsDeallocating(s.State) {
		// Neo4j defines Deallocated by what a member still hosts, so read that rather than wait for
		// a label a drained secondary can keep for good: hosting down to system (and composites,
		// virtual and listed on every server) means there is nothing left to hand over, and Neo4j's
		// own recovery procedure drops servers in this very state. Secondaries only — their copies
		// are read replicas and their system copy does not vote, so dropping one costs neither data
		// nor quorum, where a primary still in the raft group could cost both. And the caller only
		// reaches this line once every user database reports current == requested, so the copies
		// that matter are already elsewhere (ensureDatabaseTopologies).
		if m.ModeConstraint != "SECONDARY" || !intneo4j.HostsOnly(s, exempt) {
			return false, nil
		}
		ctrllog.FromContext(ctx).Info("dropping a drained secondary Neo4j still labels Deallocating",
			"pod", m.PodName, "hosting", s.Hosting)
		return dropServer(ctx, admin, s)
	}
	// Enabled / Free still hosting — deallocate.
	if err := admin.DeallocateDatabases(ctx, s.Name); err != nil {
		return false, fmt.Errorf("DEALLOCATE DATABASES FROM SERVER %s: %w", s.Name, err)
	}
	return false, nil
}

// dropServer removes one server from the cluster view. Never done on success: the next pass reads
// the state back and confirms Dropped instead of assuming it.
func dropServer(ctx context.Context, admin intneo4j.Admin, s intneo4j.Server) (bool, error) {
	if err := admin.DropServer(ctx, s.Name); err != nil {
		if strings.Contains(err.Error(), "already dropped") {
			return true, nil
		}
		return false, fmt.Errorf("DROP SERVER %s: %w", s.Name, err)
	}
	return false, nil
}

// drainExemptDatabases lists the databases that may stay in a departing member's hosting without
// holding the drain back: system, which Neo4j's own Deallocated definition excludes, and composite
// databases, which have no store and are reported on every server.
func drainExemptDatabases(dbs []intneo4j.DatabaseTopology) map[string]bool {
	exempt := map[string]bool{"system": true}
	for _, db := range dbs {
		if strings.EqualFold(db.Type, "composite") {
			exempt[strings.ToLower(db.Name)] = true
		}
	}
	return exempt
}

// drainBudget is how long a whole scale-in may stay pending before the operator says so — the
// reallocation of the database topologies included, since that is the same wait seen from the
// user's side. A small cluster releases a member in about a minute, so reaching this means
// something is worth a look: usually a topology that cannot fit the remaining servers, sometimes
// just a large store still moving. Hence a report and a slower requeue rather than any action.
const drainBudget = 10 * time.Minute

// drainWaited is how long ServersPendingDrain has been True. meta.SetStatusCondition only moves
// LastTransitionTime when the status itself changes, and a scale-in holds the condition True
// throughout while only the reason moves (ShrinkingTopology → Draining), so the condition already
// dates the episode and no extra status field has to.
func drainWaited(neo4j *neo4jv1beta1.Neo4j) time.Duration {
	c := meta.FindStatusCondition(neo4j.Status.Conditions, oracle.ConditionServersPendingDrain.String())
	if c == nil || c.Status != metav1.ConditionTrue || c.LastTransitionTime.IsZero() {
		return 0
	}
	return time.Since(c.LastTransitionTime.Time)
}

// reportDrainTimeout surfaces a drain that outlived its budget: the catalogued reason on
// ServersPendingDrain with what Neo4j still reports, one Warning Event per generation, and a
// slower requeue. The operator forces nothing — the StatefulSet keeps its current size, so the
// cluster is intact and a human decides what to do.
func (r *Reconciler) reportDrainTimeout(ctx context.Context, neo4j *neo4jv1beta1.Neo4j,
	servers []intneo4j.Server, m Member, waited time.Duration) shared.StepResult {
	detail := "Neo4j no longer reports it in SHOW SERVERS"
	if s, ok := intneo4j.FindByAddress(servers, m.BoltAddress); ok {
		detail = fmt.Sprintf("Neo4j reports it %s, hosting %v", s.State, s.Hosting)
	}
	ctrllog.FromContext(ctx).Info("drain budget exceeded",
		"pod", m.PodName, "pending", waited.String(), "budget", drainBudget.String(), "detail", detail)
	setCondition(neo4j, oracle.ConditionServersPendingDrain, metav1.ConditionTrue, oracle.ReasonDrainTimeout,
		fmt.Sprintf("scale-in pending for %s: %s has not been released yet — %s. The StatefulSet keeps its current size, so no data is at risk, but the scale-in cannot finish until Neo4j releases the member",
			waited.Round(time.Second), m.PodName, detail))
	// The budget rather than the live duration, so the text is stable and the Advisory keeps this
	// to one Event per generation instead of one per requeue.
	r.advisories.Emitf(r.Recorder, neo4j, corev1.EventTypeWarning, oracle.ReasonDrainTimeout,
		"scale-in still pending after %s: %s has not been released yet — %s. The StatefulSet keeps its current size; nothing is at risk",
		drainBudget, m.PodName, detail)
	return shared.Requeue(5 * time.Minute)
}

// ensureDatabaseTopologies caps standard database topologies to what the pools still hold.
// Scale-in only: Neo4j refuses to DEALLOCATE a server while a database claims more hosts than
// remain, so a topology wider than the target pool has to come down first. It never widens a
// topology and never pushes one toward topology.defaultPrimariesCount — a topology chosen by
// its owner is theirs (TOPO-006). Skips system/composite.
func (r *Reconciler) ensureDatabaseTopologies(ctx context.Context, admin intneo4j.Admin,
	neo4j *neo4jv1beta1.Neo4j, dbs []intneo4j.DatabaseTopology) (bool, error) {
	poolP, poolS := hostingCapacity(neo4j)
	pending := false
	for _, db := range dbs {
		if !db.HasTopology {
			continue
		}
		typ := strings.ToLower(db.Type)
		if typ == "system" || typ == "composite" {
			continue
		}
		wantP, wantS := db.RequestedPrimaries, db.RequestedSecondaries
		if wantP > poolP {
			wantP = poolP
		}
		if wantS > poolS {
			wantS = poolS
		}
		if wantP < 1 {
			wantP = 1
		}
		// Neo4j forbids ALTER DATABASE from multiple primaries → 1 primary (Raft quorum).
		if wantP < 2 && db.RequestedPrimaries > 1 {
			return false, fmt.Errorf("%w: database %q has %d primaries and the scale-in would leave %d primary server(s); keep topology.primaries.members >= 3, or drop and recreate the database yourself (dbms.cluster.recreateDatabase)",
				errUnsupportedSinglePrimary, db.Name, db.RequestedPrimaries, poolP)
		}
		if wantP != db.RequestedPrimaries || wantS != db.RequestedSecondaries {
			if err := admin.SetDatabaseTopology(ctx, db.Name, wantP, wantS); err != nil {
				if isUnsupportedSinglePrimary(err) || strings.Contains(err.Error(), "multiple primaries to one primary") {
					return false, fmt.Errorf("%w: %v", errUnsupportedSinglePrimary, err)
				}
				return false, fmt.Errorf("ALTER DATABASE %s SET TOPOLOGY: %w", db.Name, err)
			}
			r.reportTopologyResized(ctx, neo4j, db, wantP, wantS)
			pending = true
			continue
		}
		if db.CurrentPrimaries != wantP || db.CurrentSecondaries != wantS {
			pending = true
		}
	}
	return !pending, nil
}

// applyDefaultAllocation puts topology.defaultPrimariesCount (and the secondary pool total) in
// charge of databases created from now on, by writing the DBMS-wide defaults Neo4j applies to a
// CREATE DATABASE with no TOPOLOGY clause. The matching initial.dbms.default_*_count keys only
// seed those defaults at DBMS initialisation, so without this call editing the field would be a
// silent no-op on a running cluster. Existing databases are untouched.
func (r *Reconciler) applyDefaultAllocation(ctx context.Context, admin intneo4j.Admin, neo4j *neo4jv1beta1.Neo4j) error {
	poolP, poolS := hostingCapacity(neo4j)
	primaries := int64(render.ClientServiceContext(neo4j).DefaultPrimariesCount())
	if primaries > poolP {
		primaries = poolP
	}
	if primaries < 1 {
		primaries = 1
	}
	if err := admin.SetDefaultAllocationNumbers(ctx, primaries, poolS); err != nil {
		return fmt.Errorf("dbms.setDefaultAllocationNumbers(%d, %d): %w", primaries, poolS, err)
	}
	ctrllog.FromContext(ctx).V(1).Info("default allocation numbers applied",
		"primaries", primaries, "secondaries", poolS)
	return nil
}

// reportTopologyResized makes an operator-decided ALTER DATABASE visible. A scale-in caps the
// databases hosted on the pool, which rewrites a topology its owner set, so it is never silent:
// one operator log entry and one Warning Event naming the database, both counts before and after,
// and why. Reason comes from the oracle (ReasonDatabaseTopologyResized).
func (r *Reconciler) reportTopologyResized(ctx context.Context, neo4j *neo4jv1beta1.Neo4j,
	db intneo4j.DatabaseTopology, toPrimaries, toSecondaries int64) {
	const cause = "the scale-in leaves fewer servers than the topology claimed"
	ctrllog.FromContext(ctx).Info("database topology resized",
		"database", db.Name,
		"fromPrimaries", db.RequestedPrimaries, "fromSecondaries", db.RequestedSecondaries,
		"toPrimaries", toPrimaries, "toSecondaries", toSecondaries,
		"cause", cause)
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(neo4j, corev1.EventTypeWarning, oracle.ReasonDatabaseTopologyResized.String(),
		"database %q: requested topology rewritten from %d primaries / %d secondaries to %d / %d — %s",
		db.Name, db.RequestedPrimaries, db.RequestedSecondaries, toPrimaries, toSecondaries, cause)
}

var errUnsupportedSinglePrimary = fmt.Errorf("Neo4j cannot automatically shrink a multi-primary database to one primary")

func isUnsupportedSinglePrimary(err error) bool {
	return err != nil && (strings.Contains(err.Error(), errUnsupportedSinglePrimary.Error()) ||
		strings.Contains(err.Error(), "multiple primaries to one primary"))
}

// systemQuorumFloor is how many enabled primaries formation waits for: the bootstrap gate
// (topology.minimumMembers or its derived value), capped by what the primary pool can still offer.
func systemQuorumFloor(neo4j *neo4jv1beta1.Neo4j) int32 {
	gate := render.ClientServiceContext(neo4j).MinimumMembers()
	poolP, _ := hostingCapacity(neo4j)
	if int64(gate) > poolP {
		return int32(poolP)
	}
	return gate
}

// hostingCapacity returns how many hosts of each kind the pools offer: the primary pool size
// (capped while system is still single-primary) and the analytics+read total. It is a ceiling for
// database topologies, never a target.
func hostingCapacity(neo4j *neo4jv1beta1.Neo4j) (poolP, poolS int64) {
	poolP = int64(render.ContextForPool(neo4j, render.PoolPrimary).PoolReplicas())
	if cap, ok := PrimaryReplicasCap(neo4j); ok && int64(cap) < poolP {
		poolP = int64(cap)
	}
	for _, pool := range []render.PoolID{render.PoolAnalytics, render.PoolRead} {
		poolS += int64(render.ContextForPool(neo4j, pool).PoolReplicas())
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
	if err := rendersecrets.RequireAuthSecretDelegated(&secret, neo4j); err != nil {
		return nil, err
	}
	raw := string(secret.Data["NEO4J_AUTH"])
	user, pass, err := intneo4j.ParseAuthSecret(raw)
	if err != nil {
		return nil, err
	}
	opts, err := r.adminConnectOpts(ctx, neo4j)
	if err != nil {
		return nil, err
	}
	return intneo4j.Connect(ctx, AdminBoltURI(neo4j), user, pass, opts)
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

// setCondition takes catalogued values only — see internal/oracle and status.setCondition.
func setCondition(neo4j *neo4jv1beta1.Neo4j, ctype oracle.Condition, status metav1.ConditionStatus, reason oracle.Reason, message string) {
	meta.SetStatusCondition(&neo4j.Status.Conditions, metav1.Condition{
		Type:               ctype.String(),
		Status:             status,
		Reason:             reason.String(),
		Message:            message,
		ObservedGeneration: neo4j.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func clearFormationConditions(neo4j *neo4jv1beta1.Neo4j) {
	meta.RemoveStatusCondition(&neo4j.Status.Conditions, oracle.ConditionServersPendingDrain.String())
	meta.RemoveStatusCondition(&neo4j.Status.Conditions, oracle.ConditionClusterFormed.String())
}

func offlineMode(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j.Spec.Maintenance != nil && neo4j.Spec.Maintenance.OfflineMode
}

func adminErrResult(neo4j *neo4jv1beta1.Neo4j, err error) shared.StepResult {
	if isRetryableAdmin(err) {
		setCondition(neo4j, oracle.ConditionClusterFormed, metav1.ConditionFalse, oracle.ReasonWaitingSystemLeader, err.Error())
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
