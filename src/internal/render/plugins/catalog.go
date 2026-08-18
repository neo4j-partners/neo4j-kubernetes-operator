package plugins

import (
	"fmt"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// catalogToImage maps CRD catalog ids (BDR-004) to NEO4J_PLUGINS names accepted by the
// official Neo4j Docker image startup script.
// See https://neo4j.com/docs/operations-manual/current/docker/plugins/
var catalogToImage = map[string]string{
	"apoc":  "apoc",
	"gds":   "graph-data-science",
	"bloom": "bloom",
}

// Known reports whether catalogID is a V1 catalog plugin.
func Known(catalogID string) bool {
	_, ok := catalogToImage[catalogID]
	return ok
}

// ImageName returns the NEO4J_PLUGINS identifier for a catalog id.
// Unknown ids return empty — they must not be passed through to the image (NEO-013).
func ImageName(catalogID string) string {
	return catalogToImage[catalogID]
}

// SkipNetworkFetch is true when plugins are supplied on an Existing volume.
// The image entrypoint must not download JARs in that case (NEO-013).
func SkipNetworkFetch(neo4j *neo4jv1beta1.Neo4j) bool {
	if neo4j.Spec.Storage == nil || neo4j.Spec.Storage.Volumes == nil || neo4j.Spec.Storage.Volumes.Plugins == nil {
		return false
	}
	return neo4j.Spec.Storage.Volumes.Plugins.Mode == neo4jv1beta1.VolumeModeExisting
}

// Validate rejects unknown catalog ids, unused version pins, and unknown
// pluginDefinitions keys (NEO-013).
func Validate(neo4j *neo4jv1beta1.Neo4j) error {
	for _, id := range assignedIDs(neo4j) {
		if !Known(id) {
			return fmt.Errorf("plugin %q is not in the V1 catalog (apoc, gds, bloom)", id)
		}
	}
	for id, def := range neo4j.Spec.PluginDefinitions {
		if !Known(id) {
			return fmt.Errorf("pluginDefinitions[%q] is not a V1 catalog id (apoc, gds, bloom)", id)
		}
		if def.Version != "" {
			return fmt.Errorf("pluginDefinitions[%q].version is not supported; pin JARs with storage.volumes.plugins mode Existing or a custom image (NEO-013)", id)
		}
	}
	return nil
}

func assignedIDs(neo4j *neo4jv1beta1.Neo4j) []string {
	var ids []string
	ids = append(ids, neo4j.Spec.Plugins...)
	if neo4j.Spec.Topology.Primaries != nil {
		ids = append(ids, neo4j.Spec.Topology.Primaries.Plugins...)
	}
	if neo4j.Spec.Topology.Secondaries != nil {
		if neo4j.Spec.Topology.Secondaries.Analytics != nil {
			ids = append(ids, neo4j.Spec.Topology.Secondaries.Analytics.Plugins...)
		}
		if neo4j.Spec.Topology.Secondaries.Read != nil {
			ids = append(ids, neo4j.Spec.Topology.Secondaries.Read.Plugins...)
		}
	}
	return ids
}
