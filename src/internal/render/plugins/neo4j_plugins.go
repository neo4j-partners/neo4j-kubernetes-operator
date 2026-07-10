package plugins

import (
	"encoding/json"
	"sort"
)

// NEO4JPluginsEnv returns the JSON value for NEO4J_PLUGINS, or empty when no plugins are assigned.
func NEO4JPluginsEnv(catalogIDs []string) string {
	if len(catalogIDs) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(catalogIDs))
	names := make([]string, 0, len(catalogIDs))
	for _, id := range catalogIDs {
		name := ImageName(id)
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	b, err := json.Marshal(names)
	if err != nil {
		return ""
	}
	return string(b)
}

// Assigned reports whether catalogID is listed for the current pool.
func Assigned(catalogIDs []string, catalogID string) bool {
	for _, id := range catalogIDs {
		if id == catalogID {
			return true
		}
	}
	return false
}
