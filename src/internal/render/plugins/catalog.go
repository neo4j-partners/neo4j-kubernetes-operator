package plugins

// catalogToImage maps CRD catalog ids (BDR-004) to NEO4J_PLUGINS names accepted by the
// official Neo4j Docker image startup script.
// See https://neo4j.com/docs/operations-manual/current/docker/plugins/
var catalogToImage = map[string]string{
	"apoc":  "apoc",
	"gds":   "graph-data-science",
	"bloom": "bloom",
}

// ImageName returns the NEO4J_PLUGINS identifier for a catalog id.
func ImageName(catalogID string) string {
	if name, ok := catalogToImage[catalogID]; ok {
		return name
	}
	return catalogID
}
