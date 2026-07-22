package serverconfig

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func k8sNeo4jConfKeys() map[string]string {
	return map[string]string{
		"server.default_listen_address": "0.0.0.0",
	}
}

func listenerConfKeys(ctx render.Context) map[string]string {
	keys := map[string]string{}

	if ctx.BoltEnabled() {
		keys["server.bolt.listen_address"] = fmt.Sprintf(":%d", ctx.BoltPort())
		keys["server.bolt.enabled"] = "true"
	}
	if ctx.HTTPEnabled() {
		keys["server.http.listen_address"] = fmt.Sprintf(":%d", ctx.HTTPPort())
		keys["server.http.enabled"] = "true"
	}
	if ctx.HTTPSEnabled() {
		keys["server.https.listen_address"] = fmt.Sprintf(":%d", ctx.HTTPSPort())
		keys["server.https.enabled"] = "true"
	}
	if ctx.BackupListenerEnabled() {
		keys["server.backup.listen_address"] = fmt.Sprintf("0.0.0.0:%d", ctx.BackupPort())
		keys["server.backup.enabled"] = "true"
	}
	if ctx.MetricsListenerEnabled() {
		keys["server.metrics.prometheus.enabled"] = "true"
		keys["server.metrics.prometheus.endpoint"] = fmt.Sprintf("localhost:%d", ctx.MetricsPort())
	}
	return keys
}

func clusterNeo4jConfKeys(ctx render.Context) map[string]string {
	keys := map[string]string{}

	// Formation gate: topology.minimumMembers (Helm minimumClusterSize), not STS replica count.
	mcount := strconv.FormatInt(int64(ctx.MinimumMembers()), 10)
	keys["initial.dbms.default_primaries_count"] = mcount
	keys["dbms.cluster.minimum_initial_system_primaries_count"] = mcount
	keys["dbms.cluster.raft.binding_timeout"] = "1d"

	keys["dbms.cluster.discovery.resolver_type"] = "K8S"
	keys["dbms.routing.default_router"] = "SERVER"
	keys["dbms.routing.client_side.enforce_for_domains"] = fmt.Sprintf("*.%s", ctx.ClusterDomain())
	keys["dbms.routing.enabled"] = "true"
	keys["dbms.kubernetes.discovery.service_port_name"] = "tcp-tx"
	keys["dbms.kubernetes.label_selector"] = ctx.ClusterDiscoveryLabelSelector()

	// Helm: advertised addresses expand SERVICE_NEO4J / SERVICE_NEO4J_INTERNALS (per-member Service FQDNs).
	keys["server.bolt.advertised_address"] = "$(bash -c 'echo ${SERVICE_NEO4J}')"
	keys["server.cluster.raft.advertised_address"] = "$(bash -c 'echo ${SERVICE_NEO4J_INTERNALS}')"
	keys["server.cluster.advertised_address"] = "$(bash -c 'echo ${SERVICE_NEO4J_INTERNALS}')"
	keys["server.routing.advertised_address"] = "$(bash -c 'echo ${SERVICE_NEO4J_INTERNALS}')"

	// Read/analytics pools must not bootstrap system/neo4j as primaries when
	// minimum_initial_system_primaries_count is 1 (race with the primary pool).
	if ctx.Pool == render.PoolAnalytics || ctx.Pool == render.PoolRead {
		keys["server.cluster.system_database_mode"] = "SECONDARY"
		keys["initial.server.mode_constraint"] = "SECONDARY"
	}

	return keys
}

// pluginConfKeys points Neo4j at /plugins and opens procedures for assigned catalog plugins.
// Overridable via spec.config.neo4j (BDR-008 defaults layer).
func pluginConfKeys(ctx render.Context) map[string]string {
	ids := ctx.PoolPluginIDs()
	pluginsVolume := ctx.Neo4j.Spec.Storage != nil &&
		ctx.Neo4j.Spec.Storage.Volumes != nil &&
		ctx.Neo4j.Spec.Storage.Volumes.Plugins != nil

	if len(ids) == 0 && !pluginsVolume && ctx.Pool != render.PoolAnalytics {
		return nil
	}

	keys := map[string]string{}
	if len(ids) > 0 || pluginsVolume {
		keys["server.directories.plugins"] = "/plugins"
	}

	var patterns []string
	seen := map[string]struct{}{}
	add := func(p string) {
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		patterns = append(patterns, p)
	}
	for _, id := range ids {
		switch id {
		case "apoc":
			add("apoc.*")
		case "gds":
			add("gds.*")
		case "bloom":
			add("bloom.*")
		}
	}
	// Helm analytics secondary: open GDS procedures on the analytics member.
	if ctx.Pool == render.PoolAnalytics {
		add("gds.*")
	}
	if len(patterns) == 0 {
		return keys
	}
	joined := strings.Join(patterns, ",")
	keys["dbms.security.procedures.unrestricted"] = joined
	keys["dbms.security.procedures.allowlist"] = joined
	if _, ok := seen["gds.*"]; ok {
		keys["dbms.security.http_auth_allowlist"] = "gds.*"
	}
	return keys
}
