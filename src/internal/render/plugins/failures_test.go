package plugins

import (
	"strings"
	"testing"
)

// The strings below are the Neo4j image's own, copied from load_plugin_from_url in
// /startup/docker-entrypoint.sh (2026.07.1-enterprise). They are the contract this file matches
// against, so they are reproduced verbatim rather than paraphrased — a paraphrase that drifts
// would still pass a test written against the paraphrase.
const (
	downloadFailureLog = `Fetching versions.json for Plugin 'apoc-extended' from https://neo4j-contrib.github.io/neo4j-apoc-procedures/versions.json
ERROR: could not query https://neo4j-contrib.github.io/neo4j-apoc-procedures/versions.json for plugin compatibility information.
    This could indicate a problem with your network or this container's network settings.
    Neo4j will continue to start, but "apoc-extended" will not be loaded.`

	incompatibleLog = `ERROR: No compatible "apoc-extended" plugin found for Neo4j 2026.07.1 enterprise.
    This can happen with the newest Neo4j versions when a compatible plugin has not yet been released.
    Neo4j will continue to start, but "apoc-extended" will not be loaded.`

	unreadableLog = `Installing Plugin 'graph-data-science' from /var/lib/neo4j/products/neo4j-graph-data-science-*.jar to /plugins/graph-data-science.jar
Plugin at '/plugins/graph-data-science.jar' is not readable`

	healthyLog = `Installing Plugin 'apoc' from /var/lib/neo4j/labs/apoc-*-core.jar to /plugins/apoc.jar
Applying default values for plugin apoc to neo4j.conf
2026-09-22 08:02:54.787+0000 INFO  Starting...`
)

func TestDiagnoseLogDownloadFailure(t *testing.T) {
	got := DiagnoseLog(downloadFailureLog)
	if len(got) != 1 {
		t.Fatalf("expected one failure, got %#v", got)
	}
	if got[0].Kind != FailureDownload {
		t.Errorf("kind = %v, want FailureDownload", got[0].Kind)
	}
	if got[0].Plugin != "apoc-extended" {
		t.Errorf("plugin = %q, want apoc-extended (recovered from the versions URL)", got[0].Plugin)
	}
}

func TestDiagnoseLogVersionIncompatible(t *testing.T) {
	got := DiagnoseLog(incompatibleLog)
	if len(got) != 1 || got[0].Kind != FailureVersionIncompatible {
		t.Fatalf("expected one FailureVersionIncompatible, got %#v", got)
	}
	if got[0].Plugin != "apoc-extended" {
		t.Errorf("plugin = %q", got[0].Plugin)
	}
}

func TestDiagnoseLogJarUnreadable(t *testing.T) {
	got := DiagnoseLog(unreadableLog)
	if len(got) != 1 || got[0].Kind != FailureJarUnreadable {
		t.Fatalf("expected one FailureJarUnreadable, got %#v", got)
	}
	if got[0].Plugin != "graph-data-science" {
		t.Errorf("plugin = %q, want graph-data-science (recovered from the jar path)", got[0].Plugin)
	}
}

// A successful install must read as silence. The install lines themselves mention plugins and a
// pattern anchored too loosely would call a healthy boot a failure, holding Ready back forever.
func TestDiagnoseLogIgnoresHealthyInstall(t *testing.T) {
	if got := DiagnoseLog(healthyLog); len(got) != 0 {
		t.Fatalf("healthy install must report nothing, got %#v", got)
	}
	if got := DiagnoseLog(""); len(got) != 0 {
		t.Fatalf("empty log must report nothing, got %#v", got)
	}
}

// "Neo4j will continue to start, but X will not be loaded" appears under both non-fatal failures,
// so matching on it would double-count. Each message must yield exactly one failure.
func TestDiagnoseLogDoesNotDoubleCount(t *testing.T) {
	got := DiagnoseLog(downloadFailureLog + "\n" + incompatibleLog)
	if len(got) != 2 {
		t.Fatalf("expected exactly two failures, got %d: %#v", len(got), got)
	}
	if got[0].Kind != FailureDownload || got[1].Kind != FailureVersionIncompatible {
		t.Errorf("order or kinds wrong: %#v", got)
	}
}

// The detail is what reaches the user in the condition message, so it has to carry the image's own
// words — the URL it tried, not a rephrasing of the problem.
func TestFailureDetailKeepsTheImageLine(t *testing.T) {
	got := DiagnoseLog(downloadFailureLog)
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	const want = "https://neo4j-contrib.github.io/neo4j-apoc-procedures/versions.json"
	if !strings.Contains(got[0].Detail, want) {
		t.Errorf("detail %q does not name the URL the image tried", got[0].Detail)
	}
}
