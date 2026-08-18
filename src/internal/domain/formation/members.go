package formation

import (
	"fmt"
	"strings"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const (
	ConditionServersPendingDrain = "ServersPendingDrain"
	ConditionClusterFormed       = "ClusterFormed"
)

// ReasonDatabaseTopologyResized is Event-only: a scale-in left fewer servers than a database
// claimed, so the operator ran ALTER DATABASE SET TOPOLOGY on a topology it does not own. This is
// the only case where it rewrites one. Declared here rather than in status because status imports
// this package, and it is catalogued in status.ErrorOracle so tests and docs share one identifier.
const ReasonDatabaseTopologyResized = "DatabaseTopologyResized"

// Member is one desired (or draining) cluster server.
type Member struct {
	Pool           render.PoolID
	Ordinal        int32
	PodName        string
	BoltAddress    string // host:port as advertised / matched in SHOW SERVERS
	ModeConstraint string // PRIMARY | SECONDARY | ""
}

// DesiredMembers returns every pool ordinal 0..members-1 from the CR.
func DesiredMembers(neo4j *neo4jv1beta1.Neo4j) []Member {
	var out []Member
	for _, pool := range render.ActivePools(neo4j) {
		ctx := render.ContextForPool(neo4j, pool)
		n := ctx.PoolReplicas()
		mode := modeConstraint(pool)
		for o := int32(0); o < n; o++ {
			out = append(out, memberAt(ctx, pool, o, mode))
		}
	}
	return out
}

// TailMembers returns ordinals [desired .. currentReplicas-1] for a pool (scale-in candidates).
func TailMembers(neo4j *neo4jv1beta1.Neo4j, pool render.PoolID, currentReplicas int32) []Member {
	ctx := render.ContextForPool(neo4j, pool)
	desired := ctx.PoolReplicas()
	if currentReplicas <= desired {
		return nil
	}
	mode := modeConstraint(pool)
	out := make([]Member, 0, currentReplicas-desired)
	for o := currentReplicas - 1; o >= desired; o-- {
		out = append(out, memberAt(ctx, pool, o, mode))
	}
	return out
}

func memberAt(ctx render.Context, pool render.PoolID, ordinal int32, mode string) Member {
	pod := ctx.PodName(ordinal)
	host := fmt.Sprintf("%s.%s.svc.%s", pod, ctx.Namespace(), ctx.ClusterDomain())
	return Member{
		Pool:           pool,
		Ordinal:        ordinal,
		PodName:        pod,
		BoltAddress:    host + ":7687",
		ModeConstraint: mode,
	}
}

func modeConstraint(pool render.PoolID) string {
	switch pool {
	case render.PoolPrimary:
		return "PRIMARY"
	case render.PoolAnalytics, render.PoolRead:
		return "SECONDARY"
	default:
		return ""
	}
}

// ParseDrainOK reads operator-owned status.drainOK (ADD-02).
// Entries are ignored unless DrainOKGeneration matches metadata.generation, so a
// stale confirmation cannot authorize a newer scale-in intent. CR annotations are never trusted.
func ParseDrainOK(neo4j *neo4jv1beta1.Neo4j) map[string]int32 {
	out := map[string]int32{}
	if neo4j.Status.DrainOKGeneration != neo4j.Generation {
		return out
	}
	for k, v := range neo4j.Status.DrainOK {
		out[k] = v
	}
	return out
}

// SetDrainOK writes or clears one pool entry in status.drainOK (mutates neo4j.Status).
func SetDrainOK(neo4j *neo4jv1beta1.Neo4j, pool render.PoolID, replicas int32, clear bool) {
	m := map[string]int32{}
	for k, v := range neo4j.Status.DrainOK {
		m[k] = v
	}
	key := string(pool)
	if clear {
		delete(m, key)
	} else {
		m[key] = replicas
	}
	if len(m) == 0 {
		neo4j.Status.DrainOK = nil
		neo4j.Status.DrainOKGeneration = 0
		return
	}
	neo4j.Status.DrainOK = m
	neo4j.Status.DrainOKGeneration = neo4j.Generation
}

// PrimaryReplicasCap is the primary-pool ceiling while system has a single primary.
func PrimaryReplicasCap(neo4j *neo4jv1beta1.Neo4j) (int32, bool) {
	if neo4j.Status.PrimaryReplicasCap == nil {
		return 0, false
	}
	n := *neo4j.Status.PrimaryReplicasCap
	if n < 1 {
		return 0, false
	}
	return n, true
}

// SetPrimaryReplicasCap writes or clears status.primaryReplicasCap.
func SetPrimaryReplicasCap(neo4j *neo4jv1beta1.Neo4j, replicas int32, clear bool) {
	if clear {
		neo4j.Status.PrimaryReplicasCap = nil
		return
	}
	n := replicas
	neo4j.Status.PrimaryReplicasCap = &n
}

// EffectiveReplicas returns STS replicas to apply: scale-up immediately (unless primary
// cap — single system primary cannot grow via ENABLE alone); scale-down only after
// operator-owned status.drainOK (ADD-02).
func EffectiveReplicas(neo4j *neo4jv1beta1.Neo4j, pool render.PoolID, desired, currentSpec int32) int32 {
	if desired >= currentSpec {
		if pool == render.PoolPrimary {
			if cap, ok := PrimaryReplicasCap(neo4j); ok && desired > cap {
				if currentSpec > cap {
					return currentSpec
				}
				return cap
			}
		}
		return desired
	}
	if ParseDrainOK(neo4j)[string(pool)] == desired {
		return desired
	}
	return currentSpec
}

// AdminBoltURI is the operator's system-DB admin entrypoint.
// Must use neo4j:// so the driver routes writes to the system LEADER — bolt:// to any
// member (including primary-0) fails with Neo.ClientError.Cluster.NotALeader.
func AdminBoltURI(neo4j *neo4jv1beta1.Neo4j) string {
	return strings.Replace(ClientBoltURI(neo4j), "bolt://", "neo4j://", 1)
}

// ClientBoltURI is the in-cluster Bolt URI the operator dials (and clients may use).
// Host is the short Service DNS name only — never CR connectivity.clusterDomain.
// That field is for Neo4j-advertised FQDNs / CLUSTER_DOMAIN; feeding it into the
// operator dial would let a CR author redirect admin credentials (ADD-01).
func ClientBoltURI(neo4j *neo4jv1beta1.Neo4j) string {
	ctx := render.ClientServiceContext(neo4j)
	host := fmt.Sprintf("%s.%s.svc", ctx.ClientServiceName(), ctx.Namespace())
	return "bolt://" + host + ":7687"
}

// BoltTLSEnabled is true when bolt certificates are configured.
func BoltTLSEnabled(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j.Spec.Trust != nil && neo4j.Spec.Trust.Enabled &&
		neo4j.Spec.Trust.Certificates != nil && neo4j.Spec.Trust.Certificates.Bolt != nil
}
