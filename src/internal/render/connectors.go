package render

import (
	"strings"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
)

// Connector names used in connectivity.service.expose and Service port names (BDR-007).
const (
	ConnectorBolt    = "bolt"
	ConnectorHTTP    = "http"
	ConnectorHTTPS   = "https"
	ConnectorBackup  = "backup"
	ConnectorMetrics = "metrics"

	DefaultHTTPSPort   int32 = 7473
	DefaultBackupPort  int32 = 6362
	DefaultMetricsPort int32 = 2004
)

// AdminServiceName is the derived admin Service (BDR-007).
func (c Context) AdminServiceName() string { return c.Neo4j.Name + "-admin" }

// BackupFeatureEnabled reports features.backup.enabled.
func (c Context) BackupFeatureEnabled() bool {
	return c.Neo4j.Spec.Features != nil &&
		c.Neo4j.Spec.Features.Backup != nil &&
		c.Neo4j.Spec.Features.Backup.Enabled
}

// PrometheusFeatureEnabled reports features.monitoring.prometheus.enabled.
func (c Context) PrometheusFeatureEnabled() bool {
	return c.Neo4j.Spec.Features != nil &&
		c.Neo4j.Spec.Features.Monitoring != nil &&
		c.Neo4j.Spec.Features.Monitoring.Prometheus != nil &&
		c.Neo4j.Spec.Features.Monitoring.Prometheus.Enabled
}

// ShouldCreateAdminService is true when backup, prometheus, or Cluster mode needs ops access (BDR-007).
func (c Context) ShouldCreateAdminService() bool {
	return IsClusterMode(c.Neo4j) || c.BackupFeatureEnabled() || c.PrometheusFeatureEnabled()
}

func (c Context) listeners() *neo4jv1beta1.ConnectivityListenersSpec {
	if c.Neo4j.Spec.Connectivity == nil {
		return nil
	}
	return c.Neo4j.Spec.Connectivity.Listeners
}

// BoltEnabled is true unless bolt was intentionally unset as a non-default-off connector.
// V1: bolt defaults on (nil listeners.bolt ⇒ enabled at 7687). JSON null and omit are both nil in Go.
func (c Context) BoltEnabled() bool { return true }

// HTTPEnabled defaults on (same nil semantics as bolt).
func (c Context) HTTPEnabled() bool { return true }

// HTTPSEnabled is true when connectivity.listeners.https is set.
func (c Context) HTTPSEnabled() bool {
	l := c.listeners()
	return l != nil && l.HTTPS != nil
}

// BackupListenerEnabled is true when connectivity.listeners.backup is set.
func (c Context) BackupListenerEnabled() bool {
	l := c.listeners()
	return l != nil && l.Backup != nil
}

// MetricsListenerEnabled is true when connectivity.listeners.metrics is set.
func (c Context) MetricsListenerEnabled() bool {
	l := c.listeners()
	return l != nil && l.Metrics != nil
}

// HTTPSPort returns the HTTPS listen port (default 7473 when enabled).
func (c Context) HTTPSPort() int32 {
	if c.HTTPSEnabled() {
		return *c.listeners().HTTPS
	}
	return DefaultHTTPSPort
}

// BackupPort returns the backup listen port (default 6362 when enabled).
func (c Context) BackupPort() int32 {
	if c.BackupListenerEnabled() {
		return *c.listeners().Backup
	}
	return DefaultBackupPort
}

// MetricsPort returns the metrics listen port (default 2004 when enabled).
func (c Context) MetricsPort() int32 {
	if c.MetricsListenerEnabled() {
		return *c.listeners().Metrics
	}
	return DefaultMetricsPort
}

// ListenerPort returns the Neo4j listen/target port for a connector, or 0 if unknown/disabled.
func (c Context) ListenerPort(connector string) int32 {
	switch strings.ToLower(connector) {
	case ConnectorBolt:
		if c.BoltEnabled() {
			return c.BoltPort()
		}
	case ConnectorHTTP:
		if c.HTTPEnabled() {
			return c.HTTPPort()
		}
	case ConnectorHTTPS:
		if c.HTTPSEnabled() {
			return c.HTTPSPort()
		}
	case ConnectorBackup:
		if c.BackupListenerEnabled() {
			return c.BackupPort()
		}
	case ConnectorMetrics:
		if c.MetricsListenerEnabled() {
			return c.MetricsPort()
		}
	}
	return 0
}

// ConnectorEnabled reports whether the named connector should listen in the pod.
func (c Context) ConnectorEnabled(connector string) bool {
	return c.ListenerPort(connector) != 0
}

// ClientExpose returns connectors published on the client Service (BDR-007 Amendment E).
// Empty expose ⇒ [bolt, http] for enabled listeners.
func (c Context) ClientExpose() []string {
	if c.Neo4j.Spec.Connectivity != nil && c.Neo4j.Spec.Connectivity.Service != nil &&
		len(c.Neo4j.Spec.Connectivity.Service.Expose) > 0 {
		out := make([]string, 0, len(c.Neo4j.Spec.Connectivity.Service.Expose))
		for _, name := range c.Neo4j.Spec.Connectivity.Service.Expose {
			name = strings.ToLower(strings.TrimSpace(name))
			if c.ConnectorEnabled(name) {
				out = append(out, name)
			}
		}
		return out
	}
	defaults := make([]string, 0, 2)
	if c.BoltEnabled() {
		defaults = append(defaults, ConnectorBolt)
	}
	if c.HTTPEnabled() {
		defaults = append(defaults, ConnectorHTTP)
	}
	return defaults
}

// AdminExpose returns connectors on the derived admin Service (ops-relevant enabled connectors).
func (c Context) AdminExpose() []string {
	var out []string
	for _, name := range []string{ConnectorBolt, ConnectorHTTP, ConnectorHTTPS, ConnectorBackup, ConnectorMetrics} {
		if c.ConnectorEnabled(name) {
			out = append(out, name)
		}
	}
	return out
}

// ServiceFacadePort returns the Service port for a connector (optional remap via service.ports).
func (c Context) ServiceFacadePort(connector string) int32 {
	listen := c.ListenerPort(connector)
	if listen == 0 {
		return 0
	}
	if c.Neo4j.Spec.Connectivity == nil || c.Neo4j.Spec.Connectivity.Service == nil ||
		c.Neo4j.Spec.Connectivity.Service.Ports == nil {
		return listen
	}
	p := c.Neo4j.Spec.Connectivity.Service.Ports
	switch strings.ToLower(connector) {
	case ConnectorBolt:
		if p.Bolt != nil {
			return *p.Bolt
		}
	case ConnectorHTTP:
		if p.HTTP != nil {
			return *p.HTTP
		}
	case ConnectorHTTPS:
		if p.HTTPS != nil {
			return *p.HTTPS
		}
	case ConnectorBackup:
		if p.Backup != nil {
			return *p.Backup
		}
	case ConnectorMetrics:
		if p.Metrics != nil {
			return *p.Metrics
		}
	}
	return listen
}

// ClientServiceAnnotations returns optional annotations for the client Service.
func (c Context) ClientServiceAnnotations() map[string]string {
	if c.Neo4j.Spec.Connectivity != nil && c.Neo4j.Spec.Connectivity.Service != nil {
		return c.Neo4j.Spec.Connectivity.Service.Annotations
	}
	return nil
}

// ClientLoadBalancerSourceRanges returns LoadBalancer source ranges when set.
func (c Context) ClientLoadBalancerSourceRanges() []string {
	if c.Neo4j.Spec.Connectivity != nil && c.Neo4j.Spec.Connectivity.Service != nil {
		return c.Neo4j.Spec.Connectivity.Service.LoadBalancerSourceRanges
	}
	return nil
}

// ServicePortName returns the Kubernetes Service port name for a connector (Helm tcp-* for cluster internals).
func ServicePortName(connector string) string {
	switch strings.ToLower(connector) {
	case ConnectorBolt:
		return "tcp-bolt"
	case ConnectorHTTP:
		return "tcp-http"
	case ConnectorHTTPS:
		return "tcp-https"
	case ConnectorBackup:
		return "tcp-backup"
	case ConnectorMetrics:
		return "tcp-prometheus"
	default:
		return "tcp-" + strings.ToLower(connector)
	}
}
