package serverconfig

import (
	"strings"

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

// seedProviderConfKeys enables the seed providers a restore can use, matched to what the instance
// can actually reach. Since Neo4j 2025.01 neither file: nor the cloud schemes have a provider by
// default, so restore-by-backupRef / seed-from-URI is dead without this. It lives in the defaults
// layer so a user can override the list via spec.config.neo4j.
//
//   - FileSeedProvider  — when a backups volume is mounted, so a PVC round-trip can seed
//     file:/backups/<artifact> from that claim (ADR-015).
//   - CloudSeedProvider — when the instance has a cloud identity (spec.security.cloudIdentity), the
//     ADR-016 signal that it reads/writes object storage; the server pods then seed azb:/s3:/gs:
//     backups. Without it the seed fails with "provided uri does not point to a valid location".
//
// Nothing to reach → nil (leave Neo4j's default untouched).
func seedProviderConfKeys(ctx render.Context) map[string]string {
	var providers []string
	if s := ctx.Neo4j.Spec.Storage; s != nil && s.Volumes != nil && s.Volumes.Backups != nil {
		providers = append(providers, "FileSeedProvider", "CloudSeedProvider")
	} else if sec := ctx.Neo4j.Spec.Security; sec != nil && sec.CloudIdentity != nil {
		providers = append(providers, "CloudSeedProvider")
	}
	if len(providers) == 0 {
		return nil
	}
	return map[string]string{
		"dbms.databases.seed_from_uri_providers": strings.Join(providers, ","),
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
