package serverconfig

import (
	"strconv"

	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
)

func mergedNeo4jConf(ctx render.Context) map[string]string {
	merged := map[string]string{}

	if ctx.Neo4j.Spec.Config != nil && ctx.Neo4j.Spec.Config.Neo4j != nil {
		for k, v := range ctx.Neo4j.Spec.Config.Neo4j {
			merged[k] = v
		}
	}

	for k, v := range operatorNeo4jConfKeys(ctx) {
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

	return merged
}

func operatorNeo4jConfKeys(ctx render.Context) map[string]string {
	if !render.IsClusterMode(ctx.Neo4j) {
		return nil
	}

	keys := map[string]string{}
	primaries := int32(1)
	if ctx.Neo4j.Spec.Topology.Primaries != nil {
		primaries = ctx.Neo4j.Spec.Topology.Primaries.Members
	}
	pcount := strconv.FormatInt(int64(primaries), 10)
	keys["initial.dbms.default_primaries_count"] = pcount
	keys["dbms.cluster.minimum_initial_system_primaries_count"] = pcount

	keys["dbms.kubernetes.discovery.type"] = "K8S"
	keys["dbms.kubernetes.service_name"] = ctx.InternalsServiceName()
	keys["dbms.kubernetes.namespace"] = ctx.Namespace()
	keys["dbms.kubernetes.label_selector"] = ctx.ClusterDiscoveryLabelSelector()

	if ctx.Pool == render.PoolAnalytics {
		keys["server.cluster.system_database_mode"] = "SECONDARY"
	}

	return keys
}
