package formation

import (
	"fmt"
	"strconv"
	"strings"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const (
	// DrainOKAnnotation maps pool → replica floor that is safe to shrink to after drain/drop.
	// Format: "primary=3,read=1"
	DrainOKAnnotation = "neo4j.com/drain-ok"

	// PrimaryCapAnnotation holds primary STS growth when system still has a single primary.
	// Growing past one system primary (single→cluster) is not automated — keep STS at 1.
	PrimaryCapAnnotation = "neo4j.com/primary-replicas-cap"

	ConditionServersPendingDrain = "ServersPendingDrain"
	ConditionClusterFormed       = "ClusterFormed"
)

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

// ParseDrainOK reads the drain-ok annotation into pool→replicas.
func ParseDrainOK(neo4j *neo4jv1beta1.Neo4j) map[string]int32 {
	out := map[string]int32{}
	if neo4j.Annotations == nil {
		return out
	}
	raw := strings.TrimSpace(neo4j.Annotations[DrainOKAnnotation])
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 32)
		if err != nil {
			continue
		}
		out[strings.TrimSpace(k)] = int32(n)
	}
	return out
}

// SetDrainOK writes or clears one pool entry in the annotation (mutates neo4j.Annotations).
func SetDrainOK(neo4j *neo4jv1beta1.Neo4j, pool render.PoolID, replicas int32, clear bool) {
	m := ParseDrainOK(neo4j)
	key := string(pool)
	if clear {
		delete(m, key)
	} else {
		m[key] = replicas
	}
	if neo4j.Annotations == nil {
		neo4j.Annotations = map[string]string{}
	}
	if len(m) == 0 {
		delete(neo4j.Annotations, DrainOKAnnotation)
		return
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s=%d", k, v))
	}
	// stable-ish order not required for correctness
	neo4j.Annotations[DrainOKAnnotation] = strings.Join(parts, ",")
}

// PrimaryReplicasCap is the primary-pool ceiling while system has a single primary.
func PrimaryReplicasCap(neo4j *neo4jv1beta1.Neo4j) (int32, bool) {
	if neo4j.Annotations == nil {
		return 0, false
	}
	raw := strings.TrimSpace(neo4j.Annotations[PrimaryCapAnnotation])
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || n < 1 {
		return 0, false
	}
	return int32(n), true
}

// SetPrimaryReplicasCap writes or clears neo4j.com/primary-replicas-cap.
func SetPrimaryReplicasCap(neo4j *neo4jv1beta1.Neo4j, replicas int32, clear bool) {
	if neo4j.Annotations == nil {
		neo4j.Annotations = map[string]string{}
	}
	if clear {
		delete(neo4j.Annotations, PrimaryCapAnnotation)
		return
	}
	neo4j.Annotations[PrimaryCapAnnotation] = strconv.FormatInt(int64(replicas), 10)
}

// EffectiveReplicas returns STS replicas to apply: scale-up immediately (unless primary
// cap — single system primary cannot grow via ENABLE alone); scale-down only after drain-ok.
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

// ClientBoltURI is the aggregate client Service bolt URI (north-south clients).
func ClientBoltURI(neo4j *neo4jv1beta1.Neo4j) string {
	ctx := render.ClientServiceContext(neo4j)
	host := fmt.Sprintf("%s.%s.svc.%s", ctx.ClientServiceName(), ctx.Namespace(), ctx.ClusterDomain())
	return "bolt://" + host + ":7687"
}

// BoltTLSEnabled is true when bolt certificates are configured.
func BoltTLSEnabled(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j.Spec.Trust != nil && neo4j.Spec.Trust.Enabled &&
		neo4j.Spec.Trust.Certificates != nil && neo4j.Spec.Trust.Certificates.Bolt != nil
}
