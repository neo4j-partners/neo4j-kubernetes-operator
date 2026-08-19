package render

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

const (
	LabelName        = "app.kubernetes.io/name"
	LabelInstance    = "app.kubernetes.io/instance"
	LabelManagedBy   = "app.kubernetes.io/managed-by"
	LabelPool        = "neo4j.com/pool"
	LabelComponent   = "neo4j.com/component"
	LabelServiceRole = "neo4j.com/service"
	LabelClustering  = "neo4j.com/clustering"

	ManagedByValue       = "neo4j-operator"
	ServiceRoleInternals = "internals"
	ServiceRoleAdmin     = "admin"
	AppNameValue         = "neo4j"
)

// PoolID identifies a workload pool for naming and labels (ADR-005, BDR-009).
type PoolID string

const (
	PoolServer    PoolID = "server"
	PoolPrimary   PoolID = "primary"
	PoolAnalytics PoolID = "analytics"
	PoolRead      PoolID = "read"
)

// Context carries Neo4j CR metadata for deterministic child object names (ADR-005).
type Context struct {
	Neo4j *neo4jv1beta1.Neo4j
	Pool  PoolID
}

// NewContext builds a render context for the given pool.
func NewContext(neo4j *neo4jv1beta1.Neo4j, pool PoolID) Context {
	return Context{Neo4j: neo4j, Pool: pool}
}

// StandaloneContext returns the render context for a Standalone deployment.
func StandaloneContext(neo4j *neo4jv1beta1.Neo4j) Context {
	return NewContext(neo4j, PoolServer)
}

func (c Context) Name() string      { return c.Neo4j.Name }
func (c Context) Namespace() string { return c.Neo4j.Namespace }

// STSName returns the StatefulSet name for this pool.
func (c Context) STSName() string {
	switch c.Pool {
	case PoolServer:
		return c.Neo4j.Name + "-server"
	case PoolPrimary:
		return c.Neo4j.Name + "-primary"
	case PoolAnalytics:
		return c.Neo4j.Name + "-analytics"
	case PoolRead:
		return c.Neo4j.Name + "-read"
	default:
		return c.Neo4j.Name + "-server"
	}
}

// ClientServiceName is the north-south client Service (BDR-007).
func (c Context) ClientServiceName() string { return c.Neo4j.Name }

// HeadlessServiceName is the StatefulSet headless Service.
func (c Context) HeadlessServiceName() string { return c.STSName() }

// ConfigMapName is the neo4j.conf ConfigMap for this pool.
func (c Context) ConfigMapName() string {
	if c.Pool == PoolServer {
		return c.Neo4j.Name + "-config"
	}
	return c.Neo4j.Name + "-" + string(c.Pool) + "-config"
}

// ApocConfigMapName is the apoc.conf ConfigMap for this pool (Helm: {release}-apoc-config).
func (c Context) ApocConfigMapName() string {
	if c.Pool == PoolServer {
		return c.Neo4j.Name + "-apoc-config"
	}
	return c.Neo4j.Name + "-" + string(c.Pool) + "-apoc-config"
}

// ServerLogsConfigMapName is the instance-wide server-logs.xml ConfigMap (Helm parity).
func (c Context) ServerLogsConfigMapName() string { return c.Neo4j.Name + "-server-logs-config" }

// UserLogsConfigMapName is the instance-wide user-logs.xml ConfigMap (Helm parity).
func (c Context) UserLogsConfigMapName() string { return c.Neo4j.Name + "-user-logs-config" }

// InternalsServiceName is the legacy aggregate internals Service name (unused in cluster mode).
func (c Context) InternalsServiceName() string { return c.Neo4j.Name + "-internals" }

// PodName returns the StatefulSet pod name for an ordinal.
func (c Context) PodName(ordinal int32) string {
	return fmt.Sprintf("%s-%d", c.STSName(), ordinal)
}

// MemberInternalsServiceName returns the per-pod internals Service name (Helm: {release}-internals).
func (c Context) MemberInternalsServiceName(podName string) string {
	return podName + "-internals"
}

// HeadlessServiceDomain is the cluster DNS suffix for this pool's headless Service.
func (c Context) HeadlessServiceDomain() string {
	return fmt.Sprintf("%s.%s.svc.%s", c.HeadlessServiceName(), c.Namespace(), c.ClusterDomain())
}

// ClusterDiscoveryLabelSelector matches internals Services for K8S discovery (Helm parity).
// Primaries restrict to neo4j.com/clustering=true so system Raft bootstrap does not see
// analytics/read secondaries (those have lower ServerIds and steal SELECTED_BOOTSTRAPPER).
// Secondaries omit the filter so they still discover primaries.
func (c Context) ClusterDiscoveryLabelSelector() string {
	base := fmt.Sprintf("%s=%s,%s=%s,%s=%s",
		LabelName, AppNameValue,
		LabelInstance, c.Name(),
		LabelServiceRole, ServiceRoleInternals)
	if c.Pool == PoolPrimary || c.Pool == PoolServer {
		return base + "," + LabelClustering + "=true"
	}
	return base
}

// ClusterMemberSelectorLabels selects every Neo4j pod in the deployment (all pools).
func (c Context) ClusterMemberSelectorLabels() map[string]string {
	return map[string]string{
		LabelInstance: c.Neo4j.Name,
		LabelName:     AppNameValue,
	}
}

// PoolReplicas returns desired StatefulSet replicas for this pool.
func (c Context) PoolReplicas() int32 {
	if !IsClusterMode(c.Neo4j) {
		return 1
	}
	switch c.Pool {
	case PoolPrimary:
		if c.Neo4j.Spec.Topology.Primaries != nil {
			return c.Neo4j.Spec.Topology.Primaries.Members
		}
		return 1
	case PoolAnalytics:
		if c.Neo4j.Spec.Topology.Secondaries != nil && c.Neo4j.Spec.Topology.Secondaries.Analytics != nil {
			return c.Neo4j.Spec.Topology.Secondaries.Analytics.Members
		}
	case PoolRead:
		if c.Neo4j.Spec.Topology.Secondaries != nil && c.Neo4j.Spec.Topology.Secondaries.Read != nil {
			return c.Neo4j.Spec.Topology.Secondaries.Read.Members
		}
	}
	return 0
}

// ClusterDomain returns the Kubernetes cluster DNS suffix for Neo4j-advertised
// FQDNs (CLUSTER_DOMAIN, discovery, routing enforce_for_domains). It must not be
// used when the operator dials Bolt with admin credentials (ADD-01) — see
// formation.ClientBoltURI.
func (c Context) ClusterDomain() string {
	if c.Neo4j.Spec.Connectivity != nil && c.Neo4j.Spec.Connectivity.ClusterDomain != "" {
		return c.Neo4j.Spec.Connectivity.ClusterDomain
	}
	return "cluster.local"
}

// AuthSecretName resolves the auth Secret name from spec or operator default.
func (c Context) AuthSecretName() string {
	if c.Neo4j.Spec.Auth != nil && c.Neo4j.Spec.Auth.PasswordSecretRef != nil &&
		c.Neo4j.Spec.Auth.PasswordSecretRef.Name != "" {
		return c.Neo4j.Spec.Auth.PasswordSecretRef.Name
	}
	return c.Neo4j.Name + "-auth"
}

// OperandServiceAccountName is the Neo4j workload ServiceAccount.
func (c Context) OperandServiceAccountName() string { return c.Neo4j.Name }

// CommonLabels returns labels applied to every rendered object.
func (c Context) CommonLabels(component string) map[string]string {
	return map[string]string{
		LabelName:      AppNameValue,
		LabelInstance:  c.Neo4j.Name,
		LabelManagedBy: ManagedByValue,
		LabelComponent: component,
	}
}

// OperandInstanceLabels are provenance labels that must match before the operator
// deletes instance-scoped objects (ADD-04). app.kubernetes.io/instance alone is
// a shared Helm convention and is not sufficient.
func OperandInstanceLabels(neo4jName string) map[string]string {
	return map[string]string{
		LabelName:      AppNameValue,
		LabelInstance:  neo4jName,
		LabelManagedBy: ManagedByValue,
	}
}

// StoragePVCSelector lists Dynamic PVCs created for this Neo4j instance.
func StoragePVCSelector(neo4jName string) labels.Selector {
	m := OperandInstanceLabels(neo4jName)
	m[LabelComponent] = "storage"
	return labels.SelectorFromSet(m)
}

// HasOperandLabels reports whether obj carries operator provenance for neo4jName.
func HasOperandLabels(obj metav1.Object, neo4jName string) bool {
	l := obj.GetLabels()
	if l == nil {
		return false
	}
	return l[LabelName] == AppNameValue &&
		l[LabelInstance] == neo4jName &&
		l[LabelManagedBy] == ManagedByValue
}

// WorkloadLabels returns selector labels for StatefulSet pods and Services.
func (c Context) WorkloadLabels() map[string]string {
	labels := c.CommonLabels("workload")
	labels[LabelPool] = string(c.Pool)
	return labels
}

// SelectorLabels returns the minimal label set for Service selectors.
func (c Context) SelectorLabels() map[string]string {
	return map[string]string{
		LabelInstance: c.Neo4j.Name,
		LabelPool:     string(c.Pool),
	}
}

// ImageRef returns the effective container image reference.
// Prefer digest pin (repo@sha256:...) when set; otherwise repository:tag (NEO-012).
// spec.version is the Neo4j calver without edition suffix; Enterprise images use a -enterprise tag (Helm parity).
func (c Context) ImageRef() string {
	repo := "neo4j"
	if c.Neo4j.Spec.Image != nil && c.Neo4j.Spec.Image.Repository != "" {
		repo = c.Neo4j.Spec.Image.Repository
	}
	if c.Neo4j.Spec.Image != nil && c.Neo4j.Spec.Image.Digest != "" {
		return repo + "@" + c.Neo4j.Spec.Image.Digest
	}
	return repo + ":" + imageTag(c.Neo4j.Spec.Version, c.Neo4j.Spec.Edition)
}

func imageTag(version string, edition neo4jv1beta1.Edition) string {
	if version == "" {
		return version
	}
	if edition == neo4jv1beta1.EditionEnterprise && !strings.HasSuffix(version, "-enterprise") {
		return version + "-enterprise"
	}
	return version
}

// LicenseAcceptEnv returns NEO4J_ACCEPT_LICENSE_AGREEMENT (yes | eval) from spec.license.accept.
func (c Context) LicenseAcceptEnv() string {
	return string(c.Neo4j.Spec.License.Accept)
}

// Neo4jEditionK8SEnv returns NEO4J_EDITION for the official Neo4j K8s image (Helm: ENTERPRISE_K8S).
func (c Context) Neo4jEditionK8SEnv() string {
	return strings.ToUpper(string(c.Neo4j.Spec.Edition)) + "_K8S"
}

// MinimumMembers returns the system bootstrap gate
// (dbms.cluster.minimum_initial_system_primaries_count, Helm minimumClusterSize):
// spec.topology.minimumMembers when the user set it, otherwise 1 for a single-primary cluster and
// 3 for any multi-primary one. The derived value deliberately ignores primaries.members — tracking
// the pool would move the gate on every scale, rewriting neo4j.conf and rolling the pool during a
// resize. Staying at 3 costs no redundancy: the system database has no explicit topology and spans
// every member, so a 5-primary cluster still ends up with system on all 5. A user value only
// raises the bootstrap bar, since the DBMS then waits for that many primaries to meet.
func (c Context) MinimumMembers() int32 {
	if n := c.Neo4j.Spec.Topology.MinimumMembers; n != nil && *n > 0 {
		return *n
	}
	primaries := int32(1)
	if c.Neo4j.Spec.Topology.Primaries != nil && c.Neo4j.Spec.Topology.Primaries.Members > 0 {
		primaries = c.Neo4j.Spec.Topology.Primaries.Members
	}
	if primaries < 3 {
		return 1
	}
	return 3
}

// DefaultSecondariesCount returns initial.dbms.default_secondaries_count: the analytics+read
// members, so a database created without a TOPOLOGY clause is readable from the secondary pools
// the CR asked for. Existing databases are never rewritten to follow it.
func (c Context) DefaultSecondariesCount() int32 {
	var n int32
	sec := c.Neo4j.Spec.Topology.Secondaries
	if sec == nil {
		return 0
	}
	if sec.Analytics != nil && sec.Analytics.Members > 0 {
		n += sec.Analytics.Members
	}
	if sec.Read != nil && sec.Read.Members > 0 {
		n += sec.Read.Members
	}
	return n
}

// DefaultPrimariesCount returns initial.dbms.default_primaries_count.
// Defaults to 1 when unset, independently of the system bootstrap gate: how many primaries must meet
// to create the system database says nothing about how wide a user database should be.
// Clamped to [1, primaries.members].
func (c Context) DefaultPrimariesCount() int32 {
	primaries := int32(1)
	if c.Neo4j.Spec.Topology.Primaries != nil && c.Neo4j.Spec.Topology.Primaries.Members > 0 {
		primaries = c.Neo4j.Spec.Topology.Primaries.Members
	}
	n := int32(1)
	if c.Neo4j.Spec.Topology.DefaultPrimariesCount != nil {
		n = *c.Neo4j.Spec.Topology.DefaultPrimariesCount
	}
	if n > primaries {
		return primaries
	}
	if n < 1 {
		return 1
	}
	return n
}

// BoltPort returns the Bolt listen port (default 7687).
func (c Context) BoltPort() int32 {
	if c.Neo4j.Spec.Connectivity != nil && c.Neo4j.Spec.Connectivity.Listeners != nil &&
		c.Neo4j.Spec.Connectivity.Listeners.Bolt != nil {
		return *c.Neo4j.Spec.Connectivity.Listeners.Bolt
	}
	return 7687
}

// HTTPPort returns the HTTP listen port (default 7474).
func (c Context) HTTPPort() int32 {
	if c.Neo4j.Spec.Connectivity != nil && c.Neo4j.Spec.Connectivity.Listeners != nil &&
		c.Neo4j.Spec.Connectivity.Listeners.HTTP != nil {
		return *c.Neo4j.Spec.Connectivity.Listeners.HTTP
	}
	return 7474
}

// DataVolumeSize returns the requested data PVC size for Dynamic storage.
func (c Context) DataVolumeSize() string {
	if c.Neo4j.Spec.Storage != nil && c.Neo4j.Spec.Storage.Volumes != nil &&
		c.Neo4j.Spec.Storage.Volumes.Data.Dynamic != nil {
		return c.Neo4j.Spec.Storage.Volumes.Data.Dynamic.Size
	}
	return "10Gi"
}

// DataStorageClassName returns optional StorageClass for data volume.
func (c Context) DataStorageClassName() string {
	if c.Neo4j.Spec.Storage != nil && c.Neo4j.Spec.Storage.Volumes != nil &&
		c.Neo4j.Spec.Storage.Volumes.Data.Dynamic != nil {
		return c.Neo4j.Spec.Storage.Volumes.Data.Dynamic.StorageClassName
	}
	return ""
}

// PoolPluginIDs returns catalog plugin ids assigned to the current pool (BDR-004).
func (c Context) PoolPluginIDs() []string {
	if c.Neo4j.Spec.Topology.Mode == neo4jv1beta1.TopologyModeStandalone {
		return c.Neo4j.Spec.Plugins
	}
	switch c.Pool {
	case PoolPrimary:
		if c.Neo4j.Spec.Topology.Primaries != nil {
			return c.Neo4j.Spec.Topology.Primaries.Plugins
		}
	case PoolAnalytics:
		if c.Neo4j.Spec.Topology.Secondaries != nil && c.Neo4j.Spec.Topology.Secondaries.Analytics != nil {
			return c.Neo4j.Spec.Topology.Secondaries.Analytics.Plugins
		}
	case PoolRead:
		if c.Neo4j.Spec.Topology.Secondaries != nil && c.Neo4j.Spec.Topology.Secondaries.Read != nil {
			return c.Neo4j.Spec.Topology.Secondaries.Read.Plugins
		}
	}
	return nil
}

// OfflineModeEnabled is true when spec.maintenance.offlineMode is set (NEO-3-017-MNT-01).
func (c Context) OfflineModeEnabled() bool {
	return c.Neo4j.Spec.Maintenance != nil && c.Neo4j.Spec.Maintenance.OfflineMode
}

// ShouldGenerateAuthSecret is true when the operator must create the auth Secret.
func (c Context) ShouldGenerateAuthSecret() bool {
	if c.Neo4j.Spec.Auth == nil {
		return true
	}
	if c.Neo4j.Spec.Auth.PasswordSecretRef != nil && c.Neo4j.Spec.Auth.PasswordSecretRef.Name != "" {
		return false
	}
	if c.Neo4j.Spec.Auth.GeneratePassword != nil {
		return *c.Neo4j.Spec.Auth.GeneratePassword
	}
	return true
}
