package plugins

import (
	"fmt"
	"sort"
	"strings"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
)

// Source says where a plugin's JAR comes from, which decides whether installing it needs egress.
// The image entrypoint uses its bundled copy only when the glob in /startup/neo4j-plugins.json
// matches a file, and downloads otherwise — so the same catalog id is a local copy on one edition
// and a network fetch on another.
type Source int

const (
	// BundledAnyEdition ships in both the community and enterprise images: never downloaded.
	BundledAnyEdition Source = iota
	// BundledEnterprise ships under /var/lib/neo4j/products, which the community image does not
	// have. Requesting it on community makes the entrypoint fall through to a download that
	// cannot succeed — refused at admission instead (BDR-004).
	BundledEnterprise
	// AlwaysDownloaded has no bundled copy in any image, only a versions URL, so installing it
	// always needs egress from the node.
	AlwaysDownloaded
)

// Plugin is one V1 catalog entry (BDR-004).
type Plugin struct {
	// ImageName is the identifier NEO4J_PLUGINS accepts, which is not always the catalog id.
	ImageName string
	// Source decides whether the install reads a local file or reaches the network.
	Source Source
}

// catalog maps CRD catalog ids (BDR-004) to the image's own plugin registry. Kept in step with
// /startup/neo4j-plugins.json: an id the image does not know makes the entrypoint exit 1 before
// Neo4j starts, so Validate refuses it first (NEO-013).
// See https://neo4j.com/docs/operations-manual/current/docker/plugins/
var catalog = map[string]Plugin{
	"apoc":             {ImageName: "apoc", Source: BundledAnyEdition},
	"apoc-extended":    {ImageName: "apoc-extended", Source: AlwaysDownloaded},
	"gds":              {ImageName: "graph-data-science", Source: BundledEnterprise},
	"bloom":            {ImageName: "bloom", Source: BundledEnterprise},
	"genai":            {ImageName: "genai", Source: BundledEnterprise},
	"fleet-management": {ImageName: "fleet-management", Source: BundledEnterprise},
}

// Known reports whether catalogID is a V1 catalog plugin.
func Known(catalogID string) bool {
	_, ok := catalog[catalogID]
	return ok
}

// ImageName returns the NEO4J_PLUGINS identifier for a catalog id.
// Unknown ids return empty — they must not be passed through to the image (NEO-013).
func ImageName(catalogID string) string {
	return catalog[catalogID].ImageName
}

// SourceOf returns where the JAR for a catalog id comes from.
func SourceOf(catalogID string) Source {
	return catalog[catalogID].Source
}

// CatalogIDs returns every catalog id, sorted, for messages that have to list the options.
func CatalogIDs() []string {
	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// EnterpriseOnlyIDs returns the catalog ids only the enterprise image bundles, sorted. The CEL
// rule on Neo4jSpec has to spell the same list literally, and a test holds the two together.
func EnterpriseOnlyIDs() []string {
	var ids []string
	for id, p := range catalog {
		if p.Source == BundledEnterprise {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// SkipNetworkFetch is true when plugins are supplied on an Existing volume.
// The image entrypoint must not download JARs in that case (NEO-013).
func SkipNetworkFetch(neo4j *neo4jv1.Neo4j) bool {
	if neo4j.Spec.Storage == nil || neo4j.Spec.Storage.Volumes == nil || neo4j.Spec.Storage.Volumes.Plugins == nil {
		return false
	}
	return neo4j.Spec.Storage.Volumes.Plugins.Mode == neo4jv1.VolumeModeExisting
}

// Validate rejects unknown catalog ids, unused version pins, and unknown
// pluginDefinitions keys (NEO-013).
func Validate(neo4j *neo4jv1.Neo4j) error {
	options := strings.Join(CatalogIDs(), ", ")
	for _, id := range assignedIDs(neo4j) {
		if !Known(id) {
			return fmt.Errorf("plugin %q is not in the V1 catalog (%s)", id, options)
		}
		// The community image has no /var/lib/neo4j/products, so the entrypoint falls through to a
		// download that cannot succeed — and it does so without failing, leaving a server that
		// reports healthy with the plugin missing. Refused here instead (BDR-004).
		//
		// Unless the JARs come from the user's own volume: SkipNetworkFetch leaves NEO4J_PLUGINS
		// unset, so the image installs nothing and the edition decides nothing. That channel is
		// how a free GDS build legitimately runs on community.
		if SourceOf(id) == BundledEnterprise && neo4j.Spec.Edition == neo4jv1.EditionCommunity &&
			!SkipNetworkFetch(neo4j) {
			return fmt.Errorf("plugin %q ships only in the enterprise image; set edition to enterprise, "+
				"supply the JAR through storage.volumes.plugins mode Existing, or drop the plugin", id)
		}
	}
	for id, def := range neo4j.Spec.PluginDefinitions {
		if !Known(id) {
			return fmt.Errorf("pluginDefinitions[%q] is not a V1 catalog id (%s)", id, options)
		}
		if def.Version != "" {
			return fmt.Errorf("pluginDefinitions[%q].version is not supported; pin JARs with storage.volumes.plugins mode Existing or a custom image (NEO-013)", id)
		}
	}
	return nil
}

func assignedIDs(neo4j *neo4jv1.Neo4j) []string {
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
