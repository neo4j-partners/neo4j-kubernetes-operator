package serverconfig

import (
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

// mergedNeo4jConf follows BDR-008 + APOC file split:
//
//	defaults (incl. generated dbms.security.procedures.* for assigned plugins)
//	→ pluginDefinitions.*.config
//	→ spec.config.neo4j (user wins for the same keys)
//	→ topology/connectivity/trust injections
//
// dbms.security.procedures.* always land in neo4j.conf — never apoc.conf
// (https://neo4j.com/docs/apoc/current/config/).
func mergedNeo4jConf(ctx render.Context) map[string]string {
	merged := map[string]string{}

	for k, v := range operatorDefaultNeo4jConfKeys(ctx) {
		merged[k] = v
	}

	for _, pluginID := range ctx.PoolPluginIDs() {
		if ctx.Neo4j.Spec.PluginDefinitions == nil {
			continue
		}
		def, ok := ctx.Neo4j.Spec.PluginDefinitions[pluginID]
		if !ok || def.Config == nil {
			continue
		}
		for k, v := range def.Config {
			merged[k] = v
		}
	}

	if ctx.Neo4j.Spec.Config != nil && ctx.Neo4j.Spec.Config.Neo4j != nil {
		for k, v := range ctx.Neo4j.Spec.Config.Neo4j {
			merged[k] = v
		}
	}

	for k, v := range operatorInjectedNeo4jConfKeys(ctx) {
		merged[k] = v
	}

	return merged
}

// operatorDefaultNeo4jConfKeys are overridable by spec.config.neo4j (BDR-008 defaults layer).
func operatorDefaultNeo4jConfKeys(ctx render.Context) map[string]string {
	keys := k8sNeo4jConfKeys()
	for k, v := range pluginConfKeys(ctx) {
		keys[k] = v
	}
	return keys
}

// operatorInjectedNeo4jConfKeys win over user config (topology / connectivity / trust).
func operatorInjectedNeo4jConfKeys(ctx render.Context) map[string]string {
	keys := map[string]string{}
	for k, v := range listenerConfKeys(ctx) {
		keys[k] = v
	}
	if render.IsClusterMode(ctx.Neo4j) {
		for k, v := range clusterNeo4jConfKeys(ctx) {
			keys[k] = v
		}
	}
	for k, v := range rendertrust.Neo4jConfKeys(ctx) {
		keys[k] = v
	}
	return keys
}
