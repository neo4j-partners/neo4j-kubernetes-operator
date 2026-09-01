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
	merged, _ := mergeNeo4jConf(ctx)
	return merged
}

// mergeNeo4jConf applies the layers above and reports each key whose value one layer replaced
// with a different one — including the last layer silently discarding a user setting.
func mergeNeo4jConf(ctx render.Context) (map[string]string, []render.Duplicate) {
	merged := map[string]string{}
	originByKey := map[string]string{}
	var dups []render.Duplicate
	put := func(k, v, origin string) {
		if old, ok := merged[k]; ok && old != v {
			dups = append(dups, render.Duplicate{
				Field: FieldConfigNeo4j, Key: k,
				Kept: v, KeptFrom: origin,
				Dropped: old, DroppedFrom: originByKey[k],
			})
		}
		merged[k] = v
		originByKey[k] = origin
	}

	for k, v := range operatorDefaultNeo4jConfKeys(ctx) {
		put(k, v, render.OriginOperatorDefault)
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
			put(k, v, render.OriginPluginDefinition)
		}
	}

	if ctx.Neo4j.Spec.Config != nil && ctx.Neo4j.Spec.Config.Neo4j != nil {
		for k, v := range ctx.Neo4j.Spec.Config.Neo4j {
			put(k, v, render.OriginUser)
		}
	}

	for k, v := range operatorInjectedNeo4jConfKeys(ctx) {
		put(k, v, render.OriginOperatorInjected)
	}

	return merged, render.SortDuplicates(dups)
}

// operatorDefaultNeo4jConfKeys are overridable by spec.config.neo4j (BDR-008 defaults layer).
func operatorDefaultNeo4jConfKeys(ctx render.Context) map[string]string {
	keys := k8sNeo4jConfKeys()
	for k, v := range pluginConfKeys(ctx) {
		keys[k] = v
	}
	for k, v := range seedProviderConfKeys(ctx) {
		keys[k] = v
	}
	return keys
}

// seedProviderConfKeys enables FileSeedProvider (alongside the cloud providers) when a backups
// volume is mounted, so restore-by-backupRef can seed file:/backups/<artifact> from that claim
// (ADR-015 round-trip). Since Neo4j 2025.01 file: has no seed provider by default, so the round
// trip is dead without this. It lives in the defaults layer so a user can override the provider
// list via spec.config.neo4j; no backups volume → no file: seed path → nothing to enable.
func seedProviderConfKeys(ctx render.Context) map[string]string {
	s := ctx.Neo4j.Spec.Storage
	if s == nil || s.Volumes == nil || s.Volumes.Backups == nil {
		return nil
	}
	return map[string]string{
		"dbms.databases.seed_from_uri_providers": "FileSeedProvider,CloudSeedProvider",
	}
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
	for k, v := range loggingNeo4jConfKeys(ctx) {
		keys[k] = v
	}
	return keys
}
