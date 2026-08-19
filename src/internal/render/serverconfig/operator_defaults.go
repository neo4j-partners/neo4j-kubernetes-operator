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

	// System Raft gate vs default DB topology are independent: the gate comes from
	// topology.minimumMembers or its 1/3 derivation (see render.Context.MinimumMembers) and stays
	// put across scaling, while defaultPrimariesCount is the new-database default (1 when unset).
	// Both initial.* keys only seed the DBMS at initialisation; formation re-applies them on a
	// running cluster through dbms.setDefaultAllocationNumbers.
	keys["initial.dbms.default_primaries_count"] = strconv.FormatInt(int64(ctx.DefaultPrimariesCount()), 10)
	keys["initial.dbms.default_secondaries_count"] = strconv.FormatInt(int64(ctx.DefaultSecondariesCount()), 10)
	keys["dbms.cluster.minimum_initial_system_primaries_count"] = strconv.FormatInt(int64(ctx.MinimumMembers()), 10)
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

	// Read/analytics pools must not bootstrap system/neo4j as primaries.
	if ctx.Pool == render.PoolAnalytics || ctx.Pool == render.PoolRead {
		keys["server.cluster.system_database_mode"] = "SECONDARY"
		keys["initial.server.mode_constraint"] = "SECONDARY"
	}

	return keys
}

// pluginConfKeys points Neo4j at /plugins and allowlists procedures for assigned
// catalog plugins. Does not set procedures.unrestricted (sandbox bypass) — that
// must be explicit via spec.config.neo4j (NEO-024).
//
// /plugins is always the load path when plugins are in play: either
// storage.volumes.plugins or an operator-managed emptyDir (ensurePluginsMount).
func pluginConfKeys(ctx render.Context) map[string]string {
	ids := ctx.PoolPluginIDs()
	pluginsVolume := ctx.Neo4j.Spec.Storage != nil &&
		ctx.Neo4j.Spec.Storage.Volumes != nil &&
		ctx.Neo4j.Spec.Storage.Volumes.Plugins != nil

	if len(ids) == 0 && !pluginsVolume && ctx.Pool != render.PoolAnalytics {
		return nil
	}

	keys := map[string]string{}
	// Catalog plugins → STS mounts /plugins (volume or emptyDir). Explicit plugins
	// volume alone also needs the override so Neo4j reads the mount.
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
	// Analytics pool defaults: GDS procedures allowed (still sandboxed).
	if ctx.Pool == render.PoolAnalytics {
		add("gds.*")
	}
	if len(patterns) == 0 {
		return keys
	}
	keys["dbms.security.procedures.allowlist"] = strings.Join(patterns, ",")
	return keys
}
