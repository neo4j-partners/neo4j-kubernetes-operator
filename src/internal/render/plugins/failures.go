package plugins

import (
	"regexp"
	"strings"
)

// FailureKind is one way a plugin install goes wrong, as the Neo4j image reports it.
//
// The operator cannot install plugins itself — NEO4J_PLUGINS makes the image entrypoint do it,
// before the server starts — so these are read back out of the container's output. The strings
// matched below are the entrypoint's own, from install_neo4j_plugins and load_plugin_from_url in
// /startup/docker-entrypoint.sh.
type FailureKind int

const (
	// FailureDownload is a version index the entrypoint could not reach: no egress, or a blocked
	// host. Not fatal to the container — the entrypoint returns and Neo4j starts without the
	// plugin, which is why the operator has to look for it.
	FailureDownload FailureKind = iota
	// FailureVersionIncompatible is a plugin that publishes no build for this Neo4j version.
	// Also not fatal.
	FailureVersionIncompatible
	// FailureJarUnreadable is the one fatal case: the entrypoint exits before starting Neo4j, so
	// the container never comes up at all.
	FailureJarUnreadable
)

// Failure is one diagnosed plugin problem, with the line it was read from.
type Failure struct {
	Kind FailureKind
	// Plugin is the image's plugin name, which is not always the catalog id — the entrypoint
	// prints its own vocabulary.
	Plugin string
	// Detail is the entrypoint's line, kept verbatim so the condition message carries the URL or
	// path the image named rather than a paraphrase.
	Detail string
}

// Patterns are anchored on the parts of each message that carry no interpolated value, so a
// changed URL or version does not stop them matching. Quoting differs per message in the
// entrypoint — "%s" in one, \"%s\" in another — hence a pattern per message rather than one
// generic plugin-name capture.
var (
	// ERROR: could not query <url> for plugin compatibility information.
	// Names the URL, not the plugin — see reAttribution.
	reDownload = regexp.MustCompile(`could not query \S+ for plugin compatibility information`)
	// ERROR: No compatible "<name>" plugin found for Neo4j <version> <edition>.
	reIncompatible = regexp.MustCompile(`No compatible "([^"]+)" plugin found for Neo4j`)
	// Plugin at '<destination>' is not readable
	reUnreadable = regexp.MustCompile(`Plugin at '([^']+)' is not readable`)
	// Fetching versions.json for Plugin '<name>' from <url>
	// Installing Plugin '<name>' from <source> to <destination>
	//
	// The entrypoint announces which plugin it is working on before it can fail on it, which is
	// the only reliable attribution available: the download failure names only the URL, and one
	// URL serves two plugins — the apoc-extended version index lives in the apoc repository, so
	// matching catalog names against the URL blames apoc for apoc-extended's failure.
	reAttribution = regexp.MustCompile(`(?:Fetching versions\.json for|Installing) Plugin '([^']+)'`)
)

// DiagnoseLog reads a container's output and reports the plugin failures it names, in the order
// they appear. Returns nothing when the log shows no plugin problem, which is the common case —
// callers must not treat an empty result as "no plugins installed".
//
// Matching text rather than exit codes is forced by the image: three of the four failures let the
// container start normally, so there is no status field that carries them.
func DiagnoseLog(log string) []Failure {
	var out []Failure
	// The plugin the entrypoint last announced. Carried forward because the download failure
	// message does not name one.
	working := ""
	for _, line := range strings.Split(log, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := reAttribution.FindStringSubmatch(line); m != nil {
			working = m[1]
			continue
		}
		switch {
		case reDownload.MatchString(line):
			out = append(out, Failure{Kind: FailureDownload, Plugin: working, Detail: line})
		case reIncompatible.MatchString(line):
			m := reIncompatible.FindStringSubmatch(line)
			out = append(out, Failure{Kind: FailureVersionIncompatible, Plugin: m[1], Detail: line})
		case reUnreadable.MatchString(line):
			m := reUnreadable.FindStringSubmatch(line)
			out = append(out, Failure{Kind: FailureJarUnreadable, Plugin: pluginFromJarPath(m[1]), Detail: line})
		}
	}
	return out
}

// pluginFromJarPath recovers the plugin name from <plugins-dir>/<name>.jar, the destination the
// entrypoint builds.
func pluginFromJarPath(path string) string {
	base := path[strings.LastIndexByte(path, '/')+1:]
	return strings.TrimSuffix(base, ".jar")
}
