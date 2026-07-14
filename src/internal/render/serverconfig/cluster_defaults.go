package serverconfig

import (
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
