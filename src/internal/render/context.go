package render

import (
	"strings"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
)

const (
	LabelName      = "app.kubernetes.io/name"
	LabelInstance  = "app.kubernetes.io/instance"
	LabelManagedBy = "app.kubernetes.io/managed-by"
	LabelPool      = "neo4j.com/pool"
	LabelComponent = "neo4j.com/component"

	ManagedByValue = "neo4j-operator"
	AppNameValue   = "neo4j"
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

// ConfigMapName is the neo4j.conf ConfigMap.
func (c Context) ConfigMapName() string { return c.Neo4j.Name + "-config" }

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

// ImageRef returns the effective container image reference (repository:tag).
// spec.version is the Neo4j calver without edition suffix; Enterprise images use a -enterprise tag (Helm parity).
func (c Context) ImageRef() string {
	repo := "neo4j"
	if c.Neo4j.Spec.Image != nil && c.Neo4j.Spec.Image.Repository != "" {
		repo = c.Neo4j.Spec.Image.Repository
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
