/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package resources provides utilities for building Kubernetes resources for Neo4j clusters
package resources

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
)

const (
	// BoltPort is the default port for Neo4j Bolt protocol
	BoltPort = 7687
	// HTTPPort is the default port for Neo4j HTTP API
	HTTPPort = 7474
	// HTTPSPort is the default port for Neo4j HTTPS API
	HTTPSPort = 7473
	// LegacyClusterPort is the Neo4j V1 discovery / cluster port. Deprecated;
	// not used by this operator at runtime — V2 discovery on DiscoveryPort
	// (6000) is the only active path. The constant is kept because the
	// {cluster}-discovery K8s Service still exposes port 5000 (pending a
	// separate removal pass). It is no longer added to the headless service,
	// internals service, or pod containerPorts (issue #117-adjacent cleanup).
	LegacyClusterPort = 5000
	// DiscoveryPort is the default port for Neo4j cluster discovery
	DiscoveryPort = 6000
	// RoutingPort is the default port for Neo4j routing service
	RoutingPort = 7688
	// RaftPort is the default port for Neo4j Raft consensus
	RaftPort = 7000
	// TransactionPort is the default port for Neo4j transaction streaming
	TransactionPort = 7689
	// BackupPort is the default port for Neo4j backup operations
	BackupPort = 6362
	// MetricsPort is the default port for Neo4j Prometheus metrics
	MetricsPort = 2004

	// ExternalDNSHostnameAnnotation is the annotation key external-dns
	// (https://github.com/kubernetes-sigs/external-dns) watches to create
	// matching cloud DNS records for a Service or Ingress.
	ExternalDNSHostnameAnnotation = "external-dns.alpha.kubernetes.io/hostname"

	// Neo4jContainer is the name of the main Neo4j container
	Neo4jContainer = "neo4j"
	// InitContainer is the name of the init container
	InitContainer = "init"

	// DataVolume is the name of the data volume
	DataVolume = "data"
	// LogsVolume is the name of the logs volume
	LogsVolume = "logs"
	// ConfigVolume is the name of the config volume
	ConfigVolume = "config"
	// CertsVolume is the name of the certificates volume
	CertsVolume = "certs"

	// DefaultCPULimit is the default CPU limit for Neo4j containers
	DefaultCPULimit = "1000m"
	// DefaultMemoryLimit is the default memory limit for Neo4j containers
	DefaultMemoryLimit = "2Gi"
	// DefaultCPURequest is the default CPU request for Neo4j containers
	DefaultCPURequest = "500m"
	// DefaultMemoryRequest is the default memory request for Neo4j containers
	DefaultMemoryRequest = "1Gi"

	// DefaultAdminSecret is the default name for admin credentials secret
	DefaultAdminSecret = "neo4j-admin-secret"

	// TLS modes
	CertManagerMode = "cert-manager"

	// Default non-root UID/GID for Neo4j containers
	// operatorVersionEnv is the environment variable that holds the operator version
	operatorVersionEnv = "OPERATOR_VERSION"
)

// Pod hardening defaults live in security_context.go as the single source
// of truth. The two wrappers below keep the CR-override semantics that
// only the cluster CR exposes.

// OperatorUDCPackagingValue returns the value for the NEO4J_UDC_PACKAGING environment variable.
// It reads the OPERATOR_VERSION env var and returns "k8s-<version>", or "k8s-development" if unset.
// buildClusterSSLPolicyTrustConfig emits the trust-anchor and client-auth
// lines of dbms.ssl.policy.cluster.*, gated on
// cluster.Spec.TLS.StrictPeerValidation. Default-true; explicit false opts
// the cluster into the legacy "trust_all + no client auth" posture.
//
// Strict (default):
//   - trust_all=false: peers validated against /ssl/trusted/ca.crt (the
//     projected Secret ca.crt — see buildVolumes).
//   - client_auth=REQUIRE: mutual TLS, matching Neo4j's canonical cluster
//     SSL guidance (operations-manual/current/security/ssl-framework/).
//   - verify_hostname=true: explicit because the Neo4j default differs
//     between 5.26 (false) and 2025.01+ (true). The operator's emitted
//     Certificate already includes every server-pod FQDN as a SAN.
//
// Legacy (StrictPeerValidation=false):
//   - trust_all=true, client_auth=NONE. Neo4j's docs call this "debugging
//     only, since it does not offer security." Kept as an opt-out for
//     installations whose external issuer does not populate ca.crt in
//     the cert-manager Secret.
//
// clusterStrictPeerValidationEnabled mirrors controller-side
// isStrictPeerValidationEnabled but for the resources package, where we
// cannot import the controller package (would be a circular dependency).
// Both must stay in sync — they read the same field with the same default.
func clusterStrictPeerValidationEnabled(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) bool {
	if cluster == nil || cluster.Spec.TLS == nil || cluster.Spec.TLS.Mode != CertManagerMode {
		return false
	}
	if cluster.Spec.TLS.StrictPeerValidation == nil {
		return true
	}
	return *cluster.Spec.TLS.StrictPeerValidation
}

func buildClusterSSLPolicyTrustConfig(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	strict := clusterStrictPeerValidationEnabled(cluster)
	if strict {
		return `dbms.ssl.policy.cluster.trust_all=false
dbms.ssl.policy.cluster.client_auth=REQUIRE
dbms.ssl.policy.cluster.verify_hostname=true
`
	}
	return `# strictPeerValidation=false: legacy posture. trust_all=true accepts
# any peer cert without validation; client_auth=NONE skips mutual TLS.
# Neo4j's own documentation flags trust_all=true as debugging only.
dbms.ssl.policy.cluster.trust_all=true
dbms.ssl.policy.cluster.client_auth=NONE
`
}

func OperatorUDCPackagingValue() string {
	if v := os.Getenv(operatorVersionEnv); v != "" {
		return "k8s-" + v
	}
	return "k8s-development"
}

func podSecurityContextForCluster(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.PodSecurityContext {
	if cluster.Spec.SecurityContext != nil && cluster.Spec.SecurityContext.PodSecurityContext != nil {
		return cluster.Spec.SecurityContext.PodSecurityContext
	}
	return DefaultNeo4jPodSecurityContext()
}

func containerSecurityContextForCluster(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.SecurityContext {
	if cluster.Spec.SecurityContext != nil && cluster.Spec.SecurityContext.ContainerSecurityContext != nil {
		return cluster.Spec.SecurityContext.ContainerSecurityContext
	}
	return DefaultNeo4jContainerSecurityContext()
}

// BuildServerStatefulSetForEnterprise creates a single StatefulSet for all Neo4j servers
// This StatefulSet has multiple replicas (one per server) that self-organize into roles
// Replaces the previous individual StatefulSet per server approach for better management
func BuildServerStatefulSetForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *appsv1.StatefulSet {
	// Create single StatefulSet with replicas equal to number of servers
	sts := buildStatefulSetForEnterprise(cluster, "server", cluster.Spec.Topology.Servers)
	return sts
}

// BuildServerStatefulSetsForEnterprise creates individual StatefulSets for each Neo4j server
// DEPRECATED: Use BuildServerStatefulSetForEnterprise for unified StatefulSet approach
// Each server has its own StatefulSet with a replica count of 1
// First server uses bootstrapping_strategy=me, others use bootstrapping_strategy=other
func BuildServerStatefulSetsForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) []*appsv1.StatefulSet {
	var statefulSets []*appsv1.StatefulSet

	for i := int32(0); i < cluster.Spec.Topology.Servers; i++ {
		// Create individual StatefulSet for each server
		sts := buildStatefulSetForEnterprise(cluster, fmt.Sprintf("server-%d", i), 1)
		statefulSets = append(statefulSets, sts)
	}

	return statefulSets
}

// BuildBackupFromAddresses returns a comma-separated list of
// "pod-fqdn:6362" addresses for all server pods in the cluster.
// These are used as the --from flag of neo4j-admin database backup.
//
// Cluster pods are named {cluster}-server-N (the cluster StatefulSet is
// named {cluster}-server). Use BuildStandaloneBackupFromAddress for
// standalone targets — their pod naming is different.
func BuildBackupFromAddresses(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	servers := int(cluster.Spec.Topology.Servers)
	addrs := make([]string, servers)
	for i := range servers {
		addrs[i] = fmt.Sprintf("%s-server-%d.%s-headless.%s.svc.cluster.local:%d",
			cluster.Name, i, cluster.Name, cluster.Namespace, BackupPort)
	}
	return strings.Join(addrs, ",")
}

// BuildStandaloneBackupFromAddress returns the single "pod-fqdn:6362"
// address for a standalone's pod, used as the --from flag of
// neo4j-admin database backup.
//
// Standalone pods are named {standalone}-0 (the standalone StatefulSet
// is named {standalone}, single replica). Resolution requires the
// headless Service {standalone}-headless to exist — that's what
// reconcileService creates on the standalone controller side.
func BuildStandaloneBackupFromAddress(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) string {
	return fmt.Sprintf("%s-0.%s-headless.%s.svc.cluster.local:%d",
		standalone.Name, standalone.Name, standalone.Namespace, BackupPort)
}

// buildStatefulSetForEnterprise is a helper function to create StatefulSet for individual Neo4j server
func buildStatefulSetForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster, serverName string, replicas int32) *appsv1.StatefulSet {
	adminSecret := DefaultAdminSecret
	if cluster.Spec.Auth != nil && cluster.Spec.Auth.AdminSecret != "" {
		adminSecret = cluster.Spec.Auth.AdminSecret
	}

	// Configure rolling update strategy
	updateStrategy := appsv1.StatefulSetUpdateStrategy{
		Type: appsv1.RollingUpdateStatefulSetStrategyType,
		RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
			// Start with maxUnavailable = 0 to prevent concurrent updates
			Partition: nil, // Will be set during rolling upgrade orchestration
		},
	}

	// Configure upgrade strategy based on cluster spec
	if cluster.Spec.UpgradeStrategy != nil {
		if cluster.Spec.UpgradeStrategy.Strategy == "Recreate" {
			updateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
		}
	}

	// Get labels but remove clustering label from StatefulSet
	// Only pods should have the clustering label, not the StatefulSet itself
	statefulSetLabels := getLabelsForEnterprise(cluster, serverName)
	delete(statefulSetLabels, "neo4j.com/clustering")

	// Add server-specific label
	statefulSetLabels["neo4j.com/server-name"] = serverName

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", cluster.Name, serverName),
			Namespace: cluster.Namespace,
			Labels:    statefulSetLabels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:            &replicas,
			ServiceName:         fmt.Sprintf("%s-headless", cluster.Name),
			PodManagementPolicy: appsv1.ParallelPodManagement, // CRITICAL: Parallel startup for reliable cluster formation (especially with TLS)
			UpdateStrategy:      updateStrategy,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"neo4j.com/cluster":     cluster.Name,
					"neo4j.com/server-name": serverName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      getLabelsForEnterpriseServer(cluster, serverName),
					Annotations: buildMetricsAnnotations(cluster),
				},
				Spec: BuildPodSpecForEnterprise(cluster, serverName, adminSecret),
			},
			VolumeClaimTemplates: buildVolumeClaimTemplatesForEnterprise(cluster),
		},
	}
}

// BuildHeadlessServiceForEnterprise creates a headless service for StatefulSet pod identity
func BuildHeadlessServiceForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.Service {
	labels := getLabelsForEnterprise(cluster, "")

	// Remove clustering label - StatefulSet headless service doesn't need it
	delete(labels, "neo4j.com/clustering")
	delete(labels, "neo4j.com/service-type")

	// Create selector for all cluster pods
	selector := make(map[string]string)
	selector["neo4j.com/cluster"] = cluster.Name

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-headless", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                "None",   // Headless service for StatefulSet
			Selector:                 selector, // Use selector without service-type
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{
					Name:       "bolt",
					Port:       BoltPort,
					TargetPort: intstr.FromInt(BoltPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "http",
					Port:       HTTPPort,
					TargetPort: intstr.FromInt(HTTPPort),
					Protocol:   corev1.ProtocolTCP,
				},
				// Port 5000 (tcp-discovery, V1) intentionally not exposed —
				// active V2 discovery rides on tcp-tx (6000) below.
				{
					Name:       "tcp-tx",
					Port:       DiscoveryPort,
					TargetPort: intstr.FromInt(DiscoveryPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "routing",
					Port:       RoutingPort,
					TargetPort: intstr.FromInt(RoutingPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "raft",
					Port:       RaftPort,
					TargetPort: intstr.FromInt(RaftPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "transaction",
					Port:       TransactionPort,
					TargetPort: intstr.FromInt(TransactionPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "backup",
					Port:       BackupPort,
					TargetPort: intstr.FromInt(BackupPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildDiscoveryServiceForEnterprise creates a ClusterIP service specifically for Neo4j K8s discovery
// This service has the clustering label so Neo4j can discover it, and being a regular ClusterIP service,
// it has endpoints that list all pod IPs, which Neo4j's K8s discovery can query.
// Important: PublishNotReadyAddresses is set to true to ensure pods are discoverable during startup,
// which is critical for Neo4j cluster formation as pods need to discover each other before they're ready
func BuildDiscoveryServiceForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.Service {
	// Minimal labels - just what's needed for discovery
	labels := map[string]string{
		"app.kubernetes.io/name":       "neo4j",
		"app.kubernetes.io/instance":   cluster.Name,
		"app.kubernetes.io/managed-by": "neo4j-operator",
		"neo4j.com/cluster":            cluster.Name,
		"neo4j.com/clustering":         "true", // Critical: This label is required for Neo4j K8s discovery
	}

	// Selector to match pods with clustering label
	selector := map[string]string{
		"neo4j.com/cluster":    cluster.Name,
		"neo4j.com/clustering": "true",
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-discovery", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP, // Regular ClusterIP service
			Selector:                 selector,
			PublishNotReadyAddresses: true, // Allow discovery during startup
			Ports: []corev1.ServicePort{
				{
					Name:       "tcp-discovery",
					Port:       LegacyClusterPort,
					TargetPort: intstr.FromInt(LegacyClusterPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildInternalsServiceForEnterprise creates an internals service for cluster discovery
// This is NOT a headless service as per Neo4j Helm charts best practice
// "headless services have been seen to introduce latency whenever a cluster member restarts"
func BuildInternalsServiceForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.Service {
	// Add specific labels for discovery
	labels := getLabelsForEnterprise(cluster, "")
	labels["neo4j.com/service-type"] = "internals"
	// IMPORTANT: Remove clustering label from ALL services
	// Only pods should have the clustering label for direct discovery
	delete(labels, "neo4j.com/clustering")

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-internals", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			// Regular ClusterIP service (not headless) for discovery
			// This follows Neo4j Helm chart pattern to avoid latency issues
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"neo4j.com/cluster": cluster.Name,
			},
			PublishNotReadyAddresses: true, // Required for Neo4j discovery during startup
			Ports: []corev1.ServicePort{
				{
					Name:       "bolt",
					Port:       BoltPort,
					TargetPort: intstr.FromInt(BoltPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "http",
					Port:       HTTPPort,
					TargetPort: intstr.FromInt(HTTPPort),
					Protocol:   corev1.ProtocolTCP,
				},
				// Port 5000 (tcp-discovery, V1) intentionally not exposed —
				// active V2 discovery rides on tcp-tx (6000) below.
				{
					Name:       "tcp-tx",
					Port:       DiscoveryPort,
					TargetPort: intstr.FromInt(DiscoveryPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "routing",
					Port:       RoutingPort,
					TargetPort: intstr.FromInt(RoutingPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "raft",
					Port:       RaftPort,
					TargetPort: intstr.FromInt(RaftPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "transaction",
					Port:       TransactionPort,
					TargetPort: intstr.FromInt(TransactionPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "backup",
					Port:       BackupPort,
					TargetPort: intstr.FromInt(BackupPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildClientServiceForEnterprise creates a service for client connections
// ApplyExternalDNSAnnotation sets the external-dns hostname annotation
// from spec.service.dnsName when it is set. A user-supplied annotation
// (via spec.service.annotations or directly on the Ingress) wins — we
// don't clobber explicit values because operators may legitimately want a
// different hostname than the typed field, or may set the older
// `external-dns.alpha.kubernetes.io/hostname` directly. Shared by both the
// cluster and standalone code paths.
func ApplyExternalDNSAnnotation(annotations map[string]string, svc *neo4jv1beta1.ServiceSpec) {
	if svc == nil || svc.DNSName == "" {
		return
	}
	if _, exists := annotations[ExternalDNSHostnameAnnotation]; exists {
		return
	}
	annotations[ExternalDNSHostnameAnnotation] = svc.DNSName
}

func BuildClientServiceForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.Service {
	serviceType := corev1.ServiceTypeClusterIP
	if cluster.Spec.Service != nil && cluster.Spec.Service.Type != "" {
		serviceType = corev1.ServiceType(cluster.Spec.Service.Type)
	}

	ports := []corev1.ServicePort{
		{
			Name:       "bolt",
			Port:       BoltPort,
			TargetPort: intstr.FromInt(BoltPort),
			Protocol:   corev1.ProtocolTCP,
		},
		{
			Name:       "http",
			Port:       HTTPPort,
			TargetPort: intstr.FromInt(HTTPPort),
			Protocol:   corev1.ProtocolTCP,
		},
	}

	// Add HTTPS port if TLS is enabled
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == CertManagerMode {
		ports = append(ports, corev1.ServicePort{
			Name:       "https",
			Port:       HTTPSPort,
			TargetPort: intstr.FromInt(HTTPSPort),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	annotations := map[string]string{}
	if cluster.Spec.Service != nil && cluster.Spec.Service.Annotations != nil {
		for k, v := range cluster.Spec.Service.Annotations {
			annotations[k] = v
		}
	}
	ApplyExternalDNSAnnotation(annotations, cluster.Spec.Service)

	labels := getLabelsForEnterprise(cluster, "client")
	// Remove clustering label from client service
	delete(labels, "neo4j.com/clustering")

	selector := make(map[string]string)
	selector["neo4j.com/cluster"] = cluster.Name

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-client", cluster.Name),
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: selector,
			Ports:    ports,
		},
	}

	// Add enhanced features if specified
	if cluster.Spec.Service != nil {
		// LoadBalancer specific configurations
		if cluster.Spec.Service.LoadBalancerIP != "" {
			svc.Spec.LoadBalancerIP = cluster.Spec.Service.LoadBalancerIP
		}
		if len(cluster.Spec.Service.LoadBalancerSourceRanges) > 0 {
			svc.Spec.LoadBalancerSourceRanges = cluster.Spec.Service.LoadBalancerSourceRanges
		}

		// External traffic policy
		if cluster.Spec.Service.ExternalTrafficPolicy != "" {
			svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicy(cluster.Spec.Service.ExternalTrafficPolicy)
		}
	}

	return svc
}

// BuildMetricsServiceForEnterprise creates a service for Prometheus scraping.
func BuildMetricsServiceForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.Service {
	if cluster.Spec.Monitoring == nil || !cluster.Spec.Monitoring.Enabled {
		return nil
	}

	labels := getLabelsForEnterprise(cluster, "metrics")
	labels["neo4j.com/service-type"] = "metrics"

	selector := map[string]string{
		"app.kubernetes.io/name": "neo4j",
		"neo4j.com/cluster":      cluster.Name,
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-metrics", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selector,
			Ports: []corev1.ServicePort{
				{
					Name:       "metrics",
					Port:       MetricsPort,
					TargetPort: intstr.FromInt(MetricsPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildConfigMapForEnterprise creates a ConfigMap with Neo4j configuration
func BuildConfigMapForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.ConfigMap {
	config := buildNeo4jConfigForEnterprise(cluster)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-config", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    getLabelsForEnterprise(cluster, "config"),
		},
		Data: map[string]string{
			"neo4j.conf": config,
			"startup.sh": buildStartupScriptForEnterprise(cluster),
			"health.sh":  buildHealthScript(cluster),
		},
	}
}

// BuildCertificateForEnterprise creates an enhanced Certificate for TLS
func BuildCertificateForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *certv1.Certificate {
	if cluster.Spec.TLS == nil || cluster.Spec.TLS.Mode != CertManagerMode {
		return nil
	}

	// Create DNS names for the certificate
	dnsNames := []string{
		fmt.Sprintf("%s-client", cluster.Name),
		fmt.Sprintf("%s-client.%s", cluster.Name, cluster.Namespace),
		fmt.Sprintf("%s-client.%s.svc", cluster.Name, cluster.Namespace),
		fmt.Sprintf("%s-client.%s.svc.cluster.local", cluster.Name, cluster.Namespace),
		fmt.Sprintf("%s-internals", cluster.Name),
		fmt.Sprintf("%s-internals.%s", cluster.Name, cluster.Namespace),
		fmt.Sprintf("%s-internals.%s.svc", cluster.Name, cluster.Namespace),
		fmt.Sprintf("%s-internals.%s.svc.cluster.local", cluster.Name, cluster.Namespace),
		// Add headless service DNS names
		fmt.Sprintf("%s-headless", cluster.Name),
		fmt.Sprintf("%s-headless.%s", cluster.Name, cluster.Namespace),
		fmt.Sprintf("%s-headless.%s.svc", cluster.Name, cluster.Namespace),
		fmt.Sprintf("%s-headless.%s.svc.cluster.local", cluster.Name, cluster.Namespace),
	}

	// Add individual StatefulSet pods (servers)
	for i := int32(0); i < cluster.Spec.Topology.Servers; i++ {
		podName := fmt.Sprintf("%s-server-%d", cluster.Name, i)
		dnsNames = append(dnsNames,
			podName,
			fmt.Sprintf("%s.%s-internals", podName, cluster.Name),
			fmt.Sprintf("%s.%s-internals.%s", podName, cluster.Name, cluster.Namespace),
			fmt.Sprintf("%s.%s-internals.%s.svc", podName, cluster.Name, cluster.Namespace),
			fmt.Sprintf("%s.%s-internals.%s.svc.cluster.local", podName, cluster.Name, cluster.Namespace),
			// Add headless service DNS names for pod
			fmt.Sprintf("%s.%s-headless", podName, cluster.Name),
			fmt.Sprintf("%s.%s-headless.%s", podName, cluster.Name, cluster.Namespace),
			fmt.Sprintf("%s.%s-headless.%s.svc", podName, cluster.Name, cluster.Namespace),
			fmt.Sprintf("%s.%s-headless.%s.svc.cluster.local", podName, cluster.Name, cluster.Namespace),
		)
	}

	// Include the public DNS name (spec.service.dnsName) so TLS connections
	// to the external hostname pass hostname verification. external-dns
	// manages the matching DNS record from the same field; without this SAN
	// addition, drivers connecting via bolt+s://<dnsName>:7687 would fail
	// hostname verification against a cert whose SANs only cover internal
	// service / pod FQDNs.
	if cluster.Spec.Service != nil && cluster.Spec.Service.DNSName != "" {
		dnsNames = append(dnsNames, cluster.Spec.Service.DNSName)
	}

	// Build certificate spec.
	//
	// CommonName: use the bare cluster name. The x509 subject CN is
	// limited to 64 bytes per RFC 5280 and cert-manager's admission
	// webhook enforces this hard cap. The previous form
	// `<cluster>-client.<namespace>.svc.cluster.local` blew past 64 bytes
	// for moderately-sized cluster/namespace pairs (e.g. a 20-char cluster
	// in a 27-char namespace produces 74 bytes, which the webhook
	// rejected — see CI run 27005373519). The bare cluster name is bounded
	// by maxClusterNameLength=56 in the validator, so this form is always
	// safe. The CN is informational only; modern TLS clients (Neo4j Bolt
	// driver, JVM SSL stack used for intra-cluster handshakes, browsers)
	// validate hostnames against SANs (RFC 6125), and every FQDN the
	// cluster speaks on is enumerated in `dnsNames` above.
	certSpec := certv1.CertificateSpec{
		SecretName: fmt.Sprintf("%s-tls-secret", cluster.Name),
		IssuerRef: cmmeta.IssuerReference{
			Name: cluster.Spec.TLS.IssuerRef.Name,
			Kind: cluster.Spec.TLS.IssuerRef.Kind,
		},
		CommonName: cluster.Name,
		DNSNames:   dnsNames,
		// SecretTemplate propagates ownership labels onto the TLS Secret
		// cert-manager issues. Without it the Secret has no operator-
		// owned metadata and audit tooling can't tell which CR produced
		// it. Per the November 2025 security review #6 recommendation
		// to annotate Secrets for ownership + reconciliation scope.
		SecretTemplate: &certv1.CertificateSecretTemplate{
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "neo4j-operator",
				"app.kubernetes.io/component":  "tls",
				"neo4j.com/owner-kind":         "Neo4jEnterpriseCluster",
				"neo4j.com/owner-name":         cluster.Name,
			},
		},
	}

	// Add issuer group if specified
	if cluster.Spec.TLS.IssuerRef.Group != "" {
		certSpec.IssuerRef.Group = cluster.Spec.TLS.IssuerRef.Group
	}

	// Set certificate duration if specified
	if cluster.Spec.TLS.Duration != nil {
		if duration, err := time.ParseDuration(*cluster.Spec.TLS.Duration); err == nil {
			certSpec.Duration = &metav1.Duration{Duration: duration}
		}
	}

	// Set renewal before expiry if specified
	if cluster.Spec.TLS.RenewBefore != nil {
		if renewBefore, err := time.ParseDuration(*cluster.Spec.TLS.RenewBefore); err == nil {
			certSpec.RenewBefore = &metav1.Duration{Duration: renewBefore}
		}
	}

	// Set certificate subject if specified
	if cluster.Spec.TLS.Subject != nil {
		certSpec.Subject = &certv1.X509Subject{
			Organizations:       cluster.Spec.TLS.Subject.Organizations,
			Countries:           cluster.Spec.TLS.Subject.Countries,
			OrganizationalUnits: cluster.Spec.TLS.Subject.OrganizationalUnits,
			Localities:          cluster.Spec.TLS.Subject.Localities,
			Provinces:           cluster.Spec.TLS.Subject.Provinces,
		}
	}

	// Set certificate usages. When the user supplies an explicit list we
	// honour their custom usages, but ALWAYS ensure server-auth and
	// client-auth are present — Neo4j needs server-auth for incoming
	// Bolt/HTTPS/cluster TLS and client-auth for the mutual TLS the
	// operator emits on cluster links under strict peer validation
	// (client_auth=REQUIRE). The TLS validator already rejects a
	// missing required EKU at apply time; this is the defence-in-depth
	// layer that catches any CR slipping past validation (older CR
	// applied before this check shipped, custom admission controller,
	// etc.) and silently augments the usages list rather than producing
	// a cert that can't satisfy Neo4j's runtime requirements.
	if len(cluster.Spec.TLS.Usages) > 0 {
		present := make(map[certv1.KeyUsage]bool, len(cluster.Spec.TLS.Usages))
		certSpec.Usages = make([]certv1.KeyUsage, 0, len(cluster.Spec.TLS.Usages)+2)
		for _, usage := range cluster.Spec.TLS.Usages {
			u := certv1.KeyUsage(usage)
			if present[u] {
				continue
			}
			present[u] = true
			certSpec.Usages = append(certSpec.Usages, u)
		}
		for _, required := range []certv1.KeyUsage{certv1.UsageServerAuth, certv1.UsageClientAuth} {
			if !present[required] {
				certSpec.Usages = append(certSpec.Usages, required)
			}
		}
	} else {
		// Default usages for Neo4j TLS
		certSpec.Usages = []certv1.KeyUsage{
			certv1.UsageDigitalSignature,
			certv1.UsageKeyEncipherment,
			certv1.UsageServerAuth,
			certv1.UsageClientAuth,
		}
	}

	return &certv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-tls", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    getLabelsForEnterprise(cluster, "tls"),
		},
		Spec: certSpec,
	}
}

// BuildExternalSecretForTLS creates an ExternalSecret for TLS certificates
func BuildExternalSecretForTLS(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) map[string]any {
	if cluster.Spec.TLS == nil || cluster.Spec.TLS.ExternalSecrets == nil || !cluster.Spec.TLS.ExternalSecrets.Enabled {
		return nil
	}
	return buildExternalSecret(cluster, cluster.Spec.TLS.ExternalSecrets, "tls")
}

// BuildExternalSecretForAuth creates an ExternalSecret for authentication secrets
func BuildExternalSecretForAuth(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) map[string]any {
	if cluster.Spec.Auth == nil || cluster.Spec.Auth.ExternalSecrets == nil || !cluster.Spec.Auth.ExternalSecrets.Enabled {
		return nil
	}
	return buildExternalSecret(cluster, cluster.Spec.Auth.ExternalSecrets, "auth")
}

// buildExternalSecret is a helper function to create ExternalSecrets for both TLS and Auth
func buildExternalSecret(cluster *neo4jv1beta1.Neo4jEnterpriseCluster, esConfig *neo4jv1beta1.ExternalSecretsConfig, secretType string) map[string]any {
	// Build data array
	var data []map[string]any
	for _, item := range esConfig.Data {
		secretData := map[string]any{
			"secretKey": item.SecretKey,
		}

		if item.RemoteRef != nil {
			remoteRef := map[string]any{
				"key": item.RemoteRef.Key,
			}

			if item.RemoteRef.Property != "" {
				remoteRef["property"] = item.RemoteRef.Property
			}

			if item.RemoteRef.Version != "" {
				remoteRef["version"] = item.RemoteRef.Version
			}

			secretData["remoteRef"] = remoteRef
		}

		data = append(data, secretData)
	}

	// Set default refresh interval if not specified
	refreshInterval := esConfig.RefreshInterval
	if refreshInterval == "" {
		refreshInterval = "1h"
	}

	return map[string]any{
		"apiVersion": "external-secrets.io/v1beta1",
		"kind":       "ExternalSecret",
		"metadata": map[string]any{
			"name":      fmt.Sprintf("%s-%s-external-secret", cluster.Name, secretType),
			"namespace": cluster.Namespace,
			"labels":    getLabelsForEnterprise(cluster, "external-secret"),
		},
		"spec": map[string]any{
			"secretStoreRef": map[string]any{
				"name": esConfig.SecretStoreRef.Name,
				"kind": esConfig.SecretStoreRef.Kind,
			},
			"target": map[string]any{
				"name":           fmt.Sprintf("%s-%s-secret", cluster.Name, secretType),
				"creationPolicy": "Owner",
			},
			"refreshInterval": refreshInterval,
			"data":            data,
		},
	}
}

// BuildDiscoveryServiceAccountForEnterprise creates a ServiceAccount for Kubernetes discovery
func BuildDiscoveryServiceAccountForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getDiscoveryServiceAccountNameForEnterprise(cluster),
			Namespace: cluster.Namespace,
			Labels:    getLabelsForEnterprise(cluster, "discovery-service-account"),
		},
	}
}

// BuildDiscoveryRoleForEnterprise creates a Role for Kubernetes discovery
func BuildDiscoveryRoleForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getDiscoveryRoleNameForEnterprise(cluster),
			Namespace: cluster.Namespace,
			Labels:    getLabelsForEnterprise(cluster, "discovery-role"),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"services", "endpoints"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}

// BuildDiscoveryRoleBindingForEnterprise creates a RoleBinding for Kubernetes discovery
func BuildDiscoveryRoleBindingForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getDiscoveryRoleBindingNameForEnterprise(cluster),
			Namespace: cluster.Namespace,
			Labels:    getLabelsForEnterprise(cluster, "discovery-role-binding"),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      getDiscoveryServiceAccountNameForEnterprise(cluster),
				Namespace: cluster.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     getDiscoveryRoleNameForEnterprise(cluster),
		},
	}
}

// BuildIngressForEnterprise creates an Ingress for external access
func BuildIngressForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *networkingv1.Ingress {
	if cluster.Spec.Service == nil || cluster.Spec.Service.Ingress == nil || !cluster.Spec.Service.Ingress.Enabled {
		return nil
	}

	ingressSpec := cluster.Spec.Service.Ingress

	// Build TLS configuration
	var tls []networkingv1.IngressTLS
	if ingressSpec.TLSSecretName != "" {
		tls = []networkingv1.IngressTLS{
			{
				Hosts:      []string{ingressSpec.Host},
				SecretName: ingressSpec.TLSSecretName,
			},
		}
	}

	// Build HTTP paths
	paths := []networkingv1.HTTPIngressPath{
		{
			Path:     "/",
			PathType: func() *networkingv1.PathType { pt := networkingv1.PathTypePrefix; return &pt }(),
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: fmt.Sprintf("%s-client", cluster.Name),
					Port: networkingv1.ServiceBackendPort{
						Number: HTTPPort,
					},
				},
			},
		},
	}

	annotations := map[string]string{}
	for k, v := range ingressSpec.Annotations {
		annotations[k] = v
	}
	ApplyExternalDNSAnnotation(annotations, cluster.Spec.Service)

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-ingress", cluster.Name),
			Namespace:   cluster.Namespace,
			Labels:      getLabelsForEnterprise(cluster, "ingress"),
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressSpec.ClassName,
			TLS:              tls,
			Rules: []networkingv1.IngressRule{
				{
					Host: ingressSpec.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: paths,
						},
					},
				},
			},
		},
	}
}

// Helper functions

// getLabelsForEnterpriseServer returns labels for individual server pods
func getLabelsForEnterpriseServer(cluster *neo4jv1beta1.Neo4jEnterpriseCluster, serverName string) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       "neo4j",
		"app.kubernetes.io/instance":   cluster.Name,
		"app.kubernetes.io/version":    cluster.Spec.Image.Tag,
		"app.kubernetes.io/component":  "database",
		"app.kubernetes.io/part-of":    "neo4j-cluster",
		"app.kubernetes.io/managed-by": "neo4j-operator",
		"neo4j.com/cluster":            cluster.Name,
		"neo4j.com/server-name":        serverName,
		"neo4j.com/clustering":         "true", // Required for Neo4j discovery
		"neo4j.com/service-type":       "internals",
	}

	// Note: cluster spec doesn't have Labels field in current API

	return labels
}

// GetLabelsForPVC returns minimal, stable labels for PVC VolumeClaimTemplates.
// Intentionally excludes version (immutable after PVC creation) and dynamic clustering labels.
func GetLabelsForPVC(instanceName, role string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "neo4j",
		"app.kubernetes.io/instance":   instanceName,
		"app.kubernetes.io/managed-by": "neo4j-operator",
		"neo4j.com/cluster":            instanceName,
		"neo4j.com/role":               role,
	}
}

// ServerPodSelector returns labels that uniquely identify the server pods of
// an Enterprise cluster — excluding sibling pods such as the backup pod, which
// does not set app.kubernetes.io/instance.
//
// This selector is the canonical way for controllers to List server pods; any
// change here must keep the returned labels as a subset of those emitted by
// getLabelsForEnterpriseServer, and the contract is asserted in
// cluster_selectors_test.go.
func ServerPodSelector(clusterName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance":  clusterName,
		"app.kubernetes.io/component": "database",
	}
}

// StandalonePodSelector returns labels that match the pods of a
// Neo4jEnterpriseStandalone. Must stay in sync with the pod template labels
// set by Neo4jEnterpriseStandaloneReconciler.createStatefulSet.
func StandalonePodSelector(standaloneName string) map[string]string {
	return map[string]string{"app": standaloneName}
}

// PVCSelectorByInstance returns labels that match every PVC of a given
// Enterprise or Standalone instance. Must stay in sync with GetLabelsForPVC.
func PVCSelectorByInstance(instanceName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     "neo4j",
		"app.kubernetes.io/instance": instanceName,
	}
}

func getLabelsForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster, role string) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       "neo4j",
		"app.kubernetes.io/instance":   cluster.Name,
		"app.kubernetes.io/version":    cluster.Spec.Image.Tag,
		"app.kubernetes.io/component":  "database",
		"app.kubernetes.io/part-of":    "neo4j-cluster",
		"app.kubernetes.io/managed-by": "neo4j-operator",
		"neo4j.com/cluster":            cluster.Name,
		"neo4j.com/clustering":         "true",
		"neo4j.com/service-type":       "internals",
	}

	if role != "" {
		labels["neo4j.com/role"] = role
	}

	return labels
}

func buildMetricsAnnotations(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) map[string]string {
	if cluster.Spec.Monitoring == nil || !cluster.Spec.Monitoring.Enabled {
		return nil
	}

	return map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   strconv.Itoa(MetricsPort),
		"prometheus.io/path":   "/metrics",
	}
}

func BuildPodSpecForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster, serverName, adminSecret string) corev1.PodSpec {
	// Environment variables
	env := []corev1.EnvVar{
		{
			Name:  "NEO4J_EDITION",
			Value: "enterprise",
		},
		{
			Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
			Value: "yes",
		},
		{
			Name:  "NEO4J_UDC_PACKAGING",
			Value: OperatorUDCPackagingValue(),
		},
		{
			Name: "DB_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: adminSecret,
					},
					Key: "username",
				},
			},
		},
		{
			Name: "DB_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: adminSecret,
					},
					Key: "password",
				},
			},
		},
	}

	// Add JVM settings for optimal performance
	// These are production-ready defaults that can be overridden via cluster.Spec.Env
	jvmSettings := buildJVMSettings(cluster)
	if jvmSettings != "" {
		env = append(env, corev1.EnvVar{
			Name:  "NEO4J_server_jvm_additional",
			Value: jvmSettings,
		})
	}

	// Add server-specific environment variable
	env = append(env, corev1.EnvVar{
		Name:  "NEO4J_SERVER_NAME",
		Value: serverName,
	})

	// NOTE: NEO4J_PLUGINS for fleet-management is NOT baked into the static template here.
	// It is applied by the cluster controller via a live StatefulSet patch in
	// reconcileAuraFleetManagement, so it merges cleanly with plugins added by the
	// Neo4jPlugin controller rather than overwriting them.

	// NOTE: Property sharding config is handled via neo4j.conf, not environment variables

	// Add LDAP system account credentials from Secret (never in ConfigMap)
	if authEnvVars := BuildAuthEnvVars(cluster.Spec.Auth); len(authEnvVars) > 0 {
		env = append(env, authEnvVars...)
	}

	// Add custom environment variables (can override JVM settings if needed)
	// Filter out NEO4J_AUTH and NEO4J_ACCEPT_LICENSE_AGREEMENT as they are managed by the operator
	if cluster.Spec.Env != nil {
		for _, e := range cluster.Spec.Env {
			// Skip auth-related and license environment variables that are managed by the operator
			if e.Name == "NEO4J_AUTH" {
				// Log warning that NEO4J_AUTH is ignored
				continue
			}
			if e.Name == "NEO4J_ACCEPT_LICENSE_AGREEMENT" {
				// Skip duplicate - already set by operator
				continue
			}
			env = append(env, e)
		}
	}

	// Compute the merged set of trusted CAs (legacy spec.auth.trustStore folded
	// into spec.trustedCASecrets) up front so both the volumeMounts block below
	// and the volumes/initContainers block further down see the same value.
	var legacyTrustStore *neo4jv1beta1.SecretKeyRef
	if cluster.Spec.Auth != nil {
		legacyTrustStore = cluster.Spec.Auth.TrustStore
	}
	trustedCAs := CollectTrustedCASecrets(legacyTrustStore, cluster.Spec.TrustedCASecrets)

	// Volume mounts
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      DataVolume,
			MountPath: "/data",
		},
		{
			Name:      ConfigVolume,
			MountPath: "/conf",
		},
		{
			Name:      LogsVolume,
			MountPath: "/logs",
		},
		{
			Name:      "plugins",
			MountPath: "/plugins",
		},
	}

	// Add TLS volume mount
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == CertManagerMode {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      CertsVolume,
			MountPath: "/ssl",
			ReadOnly:  true,
		})
	}

	// Add truststore volume mount when any trusted CA is configured (either
	// the legacy spec.auth.trustStore or the new spec.trustedCASecrets list).
	if len(trustedCAs) > 0 {
		volumeMounts = append(volumeMounts, TrustStoreVolumeMount)
	}

	// User-supplied extra volume mounts (must reference volumes in spec.extraVolumes
	// or one of the built-in volumes). Validated for path collisions upstream.
	if len(cluster.Spec.ExtraVolumeMounts) > 0 {
		volumeMounts = append(volumeMounts, cluster.Spec.ExtraVolumeMounts...)
	}

	// Build container
	neo4jContainer := corev1.Container{
		Name:            Neo4jContainer,
		Image:           fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag),
		ImagePullPolicy: corev1.PullPolicy(cluster.Spec.Image.PullPolicy),
		Env:             env,
		// Project user-supplied env-from-Secret / env-from-ConfigMap bundles
		// onto the container. Common use-case is surfacing cloud creds
		// (AWS_ACCESS_KEY_ID etc.) for Neo4j's CloudSeedProvider so
		// CREATE DATABASE ... OPTIONS { seedURI } can authenticate.
		EnvFrom:         cluster.Spec.ExtraEnvFrom,
		SecurityContext: containerSecurityContextForCluster(cluster),
		VolumeMounts:    volumeMounts,
		Ports: []corev1.ContainerPort{
			{
				Name:          "bolt",
				ContainerPort: BoltPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "http",
				ContainerPort: HTTPPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "https",
				ContainerPort: HTTPSPort,
				Protocol:      corev1.ProtocolTCP,
			},
			// Port 5000 (tcp-discovery, V1) intentionally not exposed —
			// active V2 discovery rides on tcp-tx (6000) below.
			{
				Name:          "tcp-tx",
				ContainerPort: DiscoveryPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "routing",
				ContainerPort: RoutingPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "raft",
				ContainerPort: RaftPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "transaction",
				ContainerPort: TransactionPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "backup",
				ContainerPort: BackupPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		ReadinessProbe: buildReadinessProbe(cluster),
		LivenessProbe:  buildLivenessProbe(cluster),
		StartupProbe:   buildStartupProbe(cluster),
		Command: []string{
			"/bin/bash",
			"-c",
			"/conf/startup.sh",
		},
	}

	if cluster.Spec.Monitoring != nil && cluster.Spec.Monitoring.Enabled {
		neo4jContainer.Ports = append(neo4jContainer.Ports, corev1.ContainerPort{
			Name:          "metrics",
			ContainerPort: MetricsPort,
			Protocol:      corev1.ProtocolTCP,
		})
	}

	// Set resource limits
	if cluster.Spec.Resources != nil {
		neo4jContainer.Resources = *cluster.Spec.Resources
	} else {
		neo4jContainer.Resources = corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(DefaultCPULimit),
				corev1.ResourceMemory: resource.MustParse(DefaultMemoryLimit),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(DefaultCPURequest),
				corev1.ResourceMemory: resource.MustParse(DefaultMemoryRequest),
			},
		}
	}

	// Volumes
	volumes := []corev1.Volume{
		{
			Name: ConfigVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-config", cluster.Name),
					},
					DefaultMode: func() *int32 { mode := int32(0o755); return &mode }(),
				},
			},
		},
		{
			Name: LogsVolume,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "plugins",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// Add TLS volume.
	//
	// When strict peer validation is on (the default), we project the
	// Secret with an explicit Items list that places ca.crt at both
	// /ssl/ca.crt and /ssl/trusted/ca.crt — the second path is where
	// Neo4j's cluster SSL policy reads the trust anchor when
	// trust_all=false (operations-manual/current/security/ssl-framework/).
	// Items[] is REQUIRED here: KeyToPath has no per-item `optional` flag,
	// so listing ca.crt makes the kubelet refuse to mount the volume if
	// the issuer didn't populate it — which is the desired loud failure
	// (the reconcile-time preflight verifyTLSSecretHasCA catches this
	// earlier and surfaces a clear status before the Pod ever attempts
	// to start).
	//
	// When strict peer validation is OFF (opt-out), we MUST NOT require
	// ca.crt — the whole point of the opt-out is to support issuers that
	// don't populate it. Emit a flat Secret mount (no Items projection)
	// so whatever keys the issuer DOES populate land at /ssl/<key> and
	// missing keys are simply absent. trust_all=true ignores
	// /ssl/trusted/ entirely under this posture.
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == CertManagerMode {
		source := &corev1.SecretVolumeSource{
			SecretName: fmt.Sprintf("%s-tls-secret", cluster.Name),
			// Owner + group read only (0440) so the private key at
			// /ssl/tls.key is not world-readable inside the pod.
			// Neo4j runs as UID/GID 7474 with FSGroup 7474 (see
			// security_context.Neo4jPodUID) so owner=7474, group=7474
			// both have read; CIS Kubernetes baseline and Pod Security
			// "restricted" both flag world-readable key files. Applied
			// to ALL projected files (tls.crt, ca.crt) — those are
			// public material so 0440 doesn't lose anything for them
			// either, and a single DefaultMode keeps the strict/loose
			// projection branches symmetric.
			DefaultMode: func() *int32 { mode := int32(0o440); return &mode }(),
		}
		if clusterStrictPeerValidationEnabled(cluster) {
			source.Items = []corev1.KeyToPath{
				{Key: "tls.crt", Path: "tls.crt"},
				{Key: "tls.key", Path: "tls.key"},
				{Key: "ca.crt", Path: "ca.crt"},
				{Key: "ca.crt", Path: "trusted/ca.crt"},
			}
		}
		volumes = append(volumes, corev1.Volume{
			Name:         CertsVolume,
			VolumeSource: corev1.VolumeSource{Secret: source},
		})
	}

	// trustedCAs was computed up-front (above the volumeMounts block) — wire
	// up Secret-backed volumes + the writable EmptyDir for the truststore.
	if len(trustedCAs) > 0 {
		volumes = append(volumes, BuildTrustStoreVolumes(trustedCAs)...)
	}

	// User-supplied extra volumes (escape hatch for arbitrary mounts —
	// per-policy SSL truststores for cross-cluster replication, plugin JARs,
	// custom configs, etc.). Wired straight through; mount paths are checked
	// by the validator against operator-managed paths.
	if len(cluster.Spec.ExtraVolumes) > 0 {
		volumes = append(volumes, cluster.Spec.ExtraVolumes...)
	}

	// Build init containers
	var initContainers []corev1.Container
	if len(trustedCAs) > 0 {
		image := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
		initContainers = append(initContainers, BuildTrustStoreInitContainer(image, trustedCAs))
	}

	// Build pod spec — backups are driven by the Neo4jBackup CRD (Job-per-CR),
	// never sidecars or a persistent backup pod.
	podSpec := corev1.PodSpec{
		ServiceAccountName: getDiscoveryServiceAccountNameForEnterprise(cluster),
		SecurityContext:    podSecurityContextForCluster(cluster),
		InitContainers:     initContainers,
		Containers:         []corev1.Container{neo4jContainer},
		Volumes:            volumes,
	}

	// Add node selector if specified
	if cluster.Spec.NodeSelector != nil {
		podSpec.NodeSelector = cluster.Spec.NodeSelector
	}

	// Add tolerations if specified
	if cluster.Spec.Tolerations != nil {
		podSpec.Tolerations = cluster.Spec.Tolerations
	}

	// Add affinity if specified
	if cluster.Spec.Affinity != nil {
		podSpec.Affinity = cluster.Spec.Affinity
	}

	// Wire image pull secrets from cluster spec
	if refs := clusterImagePullSecrets(cluster); len(refs) > 0 {
		podSpec.ImagePullSecrets = refs
	}

	// --- Plugin Management ---
	// NOTE: Plugins are now managed through the Neo4jPlugin CRD instead of embedded configuration.
	// The Neo4jPlugin controller handles plugin installation and management separately.

	return podSpec
}

// clusterImagePullSecrets converts the cluster's image pull secret names to []corev1.LocalObjectReference.
func clusterImagePullSecrets(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) []corev1.LocalObjectReference {
	return ImagePullSecretsFromNames(cluster.Spec.Image.PullSecrets)
}

// ImagePullSecretsFromNames is the exported variant of the cluster/standalone
// pull-secret accessor: given a slice of Secret names, return the
// LocalObjectReference list that goes into a PodSpec's
// ImagePullSecrets field. Returns nil for empty/all-blank input so the
// caller can drop the field entirely on PodSpec.
//
// Lives at the resources/ layer so the backup, restore, plugin, and
// other Job-spawning controllers can all pull the same shape without
// reaching into typed cluster/standalone helpers. Empty strings are
// skipped — they'd be invalid Secret names anyway and silently break
// kubelet's image-pull resolution.
func ImagePullSecretsFromNames(names []string) []corev1.LocalObjectReference {
	if len(names) == 0 {
		return nil
	}
	refs := make([]corev1.LocalObjectReference, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		refs = append(refs, corev1.LocalObjectReference{Name: name})
	}
	return refs
}

// StorageClassNamePtr returns a pointer to the given storage class name, or nil
// when the name is empty. A nil StorageClassName makes the PVC inherit the
// cluster's default StorageClass; a pointer to "" would instead DISABLE dynamic
// provisioning (bind only to a pre-existing class-less PV), which is almost
// never what a user blanking the field intends.
func StorageClassNamePtr(className string) *string {
	if className == "" {
		return nil
	}
	return &className
}

func buildVolumeClaimTemplatesForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) []corev1.PersistentVolumeClaim {
	return []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   DataVolume,
				Labels: GetLabelsForPVC(cluster.Name, "server"),
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				StorageClassName: StorageClassNamePtr(cluster.Spec.Storage.ClassName),
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(cluster.Spec.Storage.Size),
					},
				},
			},
		},
	}
}

func buildNeo4jConfigForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	// Calculate optimal memory settings for Neo4j 5.26+
	memoryConfig := GetMemoryConfigForCluster(cluster)

	config := fmt.Sprintf(`# Neo4j Enterprise Configuration (5.26+ / 2025.x.x)

# Server settings
server.default_listen_address=0.0.0.0
server.bolt.listen_address=0.0.0.0:7687
server.http.listen_address=0.0.0.0:7474

# Paths
server.directories.data=/data
server.directories.logs=/logs
server.directories.plugins=/plugins

# Memory settings (optimized for Neo4j 5.26+ and container resources)
server.memory.heap.initial_size=%s
server.memory.heap.max_size=%s
server.memory.pagecache.size=%s

# Basic logging (using default settings)

# Disable strict validation to allow experimental settings
server.config.strict_validation.enabled=false

# Cloud storage integration settings (5.26+ / 2025.x.x)
# dbms.integrations.cloud_storage.azb.blob_endpoint_suffix=blob.core.windows.net
# dbms.integrations.cloud_storage.azb.authority_endpoint=

# Database format - use block format (default in 5.26+ / 2025.x.x)
# Note: standard and high_limit formats are deprecated
db.format=block

# Enterprise clustering configuration for Neo4j 5.x
# Note: advertised addresses will be set dynamically by startup script
# Port 5000: V1 discovery protocol (tcp-discovery) — DEPRECATED, not used by this operator
# Port 6000: V2 discovery + cluster catchup/transaction protocol (tcp-tx) — active
# Port 7000: RAFT consensus (raft)
server.cluster.listen_address=0.0.0.0:6000
server.routing.listen_address=0.0.0.0:7688
server.cluster.raft.listen_address=0.0.0.0:7000
server.backup.enabled=true
server.backup.listen_address=0.0.0.0:6362

# Note: Single RAFT and cluster discovery settings are dynamically added by startup script
`, memoryConfig.HeapInitialSize, memoryConfig.HeapMaxSize, memoryConfig.PageCacheSize)

	// NOTE: Property sharding configuration moved to end of config file

	// Add transaction memory limits for stability
	// These prevent OOM kills from runaway queries
	config += fmt.Sprintf(`
# Transaction Memory Limits (prevents OOM from heavy queries)
# Global transaction memory limit (defaults to 70%% of heap if not set)
dbms.memory.transaction.total.max=%s
# Maximum memory per transaction (defaults to 10%% of global limit)
db.memory.transaction.max=%s
# Per-database transaction memory limit (optional, defaults to global limit)
# db.memory.transaction.total.max=%s

# Bolt thread pool configuration for better connection handling
server.bolt.thread_pool_min_size=5
server.bolt.thread_pool_max_size=400
server.bolt.thread_pool_keep_alive=5m
`,
		calculateTransactionMemoryLimit(memoryConfig.HeapMaxSize, cluster.Spec.Config),
		calculatePerTransactionLimit(memoryConfig.HeapMaxSize, cluster.Spec.Config),
		calculatePerDatabaseLimit(memoryConfig.HeapMaxSize, cluster.Spec.Config))

	// Add TLS configuration if enabled
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == CertManagerMode {
		config += `
# TLS Configuration for Neo4j 5.26+
server.https.enabled=true
server.https.listen_address=0.0.0.0:7473
server.https.advertised_address=${HOSTNAME}:7473

# SSL Policy Configuration
# Base certificate directory
server.directories.certificates=/ssl

# Bolt SSL Policy
dbms.ssl.policy.bolt.enabled=true
dbms.ssl.policy.bolt.base_directory=/ssl
dbms.ssl.policy.bolt.private_key=tls.key
dbms.ssl.policy.bolt.public_certificate=tls.crt
dbms.ssl.policy.bolt.client_auth=NONE
dbms.ssl.policy.bolt.tls_versions=TLSv1.3,TLSv1.2

# HTTPS SSL Policy
dbms.ssl.policy.https.enabled=true
dbms.ssl.policy.https.base_directory=/ssl
dbms.ssl.policy.https.private_key=tls.key
dbms.ssl.policy.https.public_certificate=tls.crt
dbms.ssl.policy.https.client_auth=NONE
dbms.ssl.policy.https.tls_versions=TLSv1.3,TLSv1.2

# Cluster SSL Policy (for intra-cluster communication)
dbms.ssl.policy.cluster.enabled=true
dbms.ssl.policy.cluster.base_directory=/ssl
dbms.ssl.policy.cluster.private_key=tls.key
dbms.ssl.policy.cluster.public_certificate=tls.crt
` + buildClusterSSLPolicyTrustConfig(cluster) + `dbms.ssl.policy.cluster.tls_versions=TLSv1.3,TLSv1.2

# Enable TLS for connectors
server.bolt.tls_level=REQUIRED
`
	}

	// Metrics-subsystem hardening: unconditionally disable JMX and CSV
	// regardless of spec.monitoring.enabled. Neo4j's upstream defaults
	// turn BOTH on, which is a security + housekeeping problem:
	//   - JMX exposes an unauthenticated MBeans surface (any pod that
	//     reaches the JVM remote-management port can dump metrics or
	//     issue management ops). We use Prometheus; JMX is redundant.
	//   - CSV writes one file per metric per interval into the pod's
	//     filesystem — useless under Kubernetes since the pod is
	//     ephemeral and the files disappear on restart.
	// Setting these here (not inside BuildMonitoringConfig) means even
	// users with no spec.monitoring block get the secure defaults.
	// Users who genuinely need JMX or CSV can still re-enable via
	// spec.config (which is appended last).
	config += `
# Metrics subsystem hardening (issue #128 follow-up — see ops-manual /monitoring/metrics/expose/)
server.metrics.jmx.enabled=false
server.metrics.csv.enabled=false

`
	// Seed-from-URI providers — register every modern provider so all
	// documented URI schemes resolve out of the box. ServerSeedProvider
	// is version-gated (introduced in Neo4j 2026.04); deprecated
	// S3SeedProvider is always excluded — CloudSeedProvider handles s3://
	// via the SDK default credential chain, so legacy `seedCredentials`
	// is unnecessary. Users override via spec.config.
	config += fmt.Sprintf("# Seed-from-URI providers (S3SeedProvider deliberately excluded — use CloudSeedProvider)\ndbms.databases.seed_from_uri_providers=%s\n\n", SeedFromURIProvidersConfigValue(cluster.Spec.Image.Tag))

	if cluster.Spec.Monitoring != nil && cluster.Spec.Monitoring.Enabled {
		config += "\n# Query Monitoring and Metrics\n"
		config += BuildMonitoringConfig(cluster.Spec.Monitoring)
	}

	// Audit logging — emitted AFTER monitoring so audit-driven values
	// override monitoring defaults on the keys they share. User
	// spec.config still wins (appended last) per the project's
	// "user override always allowed" precedence rule.
	config += BuildAuditConfig(cluster.Spec.Audit)

	// Authentication/Authorization configuration from typed auth fields
	// Generated keys are tracked so they are excluded from custom config merge below
	var authGeneratedKeys []string
	if cluster.Spec.Auth != nil {
		authResult := BuildAuthConfig(cluster.Spec.Auth)
		if authResult.Config != "" {
			config += "\n# Authentication/Authorization Configuration\n"
			config += authResult.Config
			authGeneratedKeys = authResult.GeneratedKeys
		}
	}

	// Add custom configuration (excluding memory settings and auth-generated keys)
	if cluster.Spec.Config != nil {
		// Keys already set by the operator — user's spec.config values are skipped for these
		excludeKeys := map[string]bool{
			"server.memory.heap.initial_size": true,
			"server.memory.heap.max_size":     true,
			"server.memory.pagecache.size":    true,
			"server.bolt.tls_level":           true,
			"server.directories.certificates": true,
		}
		for _, key := range authGeneratedKeys {
			excludeKeys[key] = true
		}

		// Sort keys to ensure deterministic order and prevent hash oscillation
		var keys []string
		for key := range cluster.Spec.Config {
			if excludeKeys[key] {
				continue
			}
			// Belt-and-suspenders: SSL policy keys are rejected by the
			// validator at apply time, but if a CR was applied before the
			// validator was in place (or with a future custom admission
			// controller that bypasses our reconcile-time validation), we
			// still must not let user values for these keys override the
			// operator-managed TLS posture. Skip every dbms.ssl.policy.*
			// key. Note that server.config.strict_validation.enabled=false
			// is set elsewhere in this file, so without this filter Neo4j
			// would silently let a later spec.config line shadow our
			// strict-mode emission (issue: spec.config could override
			// strict TLS defaults).
			if strings.HasPrefix(key, "dbms.ssl.policy.") {
				continue
			}
			keys = append(keys, key)
		}
		// Sort keys alphabetically for consistent ordering
		sort.Strings(keys)

		// Add configuration in sorted order
		for _, key := range keys {
			config += fmt.Sprintf("%s=%s\n", key, cluster.Spec.Config[key])
		}
	}

	// Aura Fleet Management configuration
	if cluster.Spec.AuraFleetManagement != nil && cluster.Spec.AuraFleetManagement.Enabled {
		config += "\n# Aura Fleet Management\n"
		config += "dbms.security.procedures.unrestricted=fleetManagement.*\n"
		config += "dbms.security.procedures.allowlist=fleetManagement.*\n"
	}

	// Property sharding configuration - placed at the very end to avoid startup script overwrites
	if cluster.Spec.PropertySharding != nil && cluster.Spec.PropertySharding.Enabled {
		config += "\n# Property Sharding Configuration (CRITICAL: placed at end to avoid script overwrites)\n"

		propertyShardingConfig := buildPropertyShardingConfig(cluster)

		// Sort keys to ensure deterministic order
		var propertyShardingKeys []string
		for key := range propertyShardingConfig {
			propertyShardingKeys = append(propertyShardingKeys, key)
		}
		sort.Strings(propertyShardingKeys)

		// Add property sharding configuration in sorted order
		for _, key := range propertyShardingKeys {
			config += fmt.Sprintf("%s=%s\n", key, propertyShardingConfig[key])
		}
	}

	return DedupeNeo4jConf(config)
}

// Additive list keys: the operator emits these for plugins (GDS/Bloom/APOC) and
// Aura Fleet Management, and the user may also set them in spec.config. A plain
// last-wins dedupe would DROP the operator's contribution (e.g. Aura's
// fleetManagement.* lost when the user sets gds.*,apoc.*), so DedupeNeo4jConf
// MERGES all occurrences into the union of their comma-separated tokens instead.
var additiveConfKeys = map[string]bool{
	"dbms.security.procedures.unrestricted": true,
	"dbms.security.procedures.allowlist":    true,
	"dbms.security.procedures.denylist":     true,
	"dbms.security.http_auth_allowlist":     true,
	"server.unmanaged_extension_classes":    true,
}

// Neo4jSettingEnvVarName converts a neo4j.conf setting key to its Neo4j Docker
// environment-variable form: the NEO4J_ prefix, a literal '_' doubled to '__',
// then '.' replaced with '_', with case preserved. E.g.
// dbms.security.procedures.unrestricted -> NEO4J_dbms_security_procedures_unrestricted
// and dbms.security.http_auth_allowlist  -> NEO4J_dbms_security_http__auth__allowlist.
// The Neo4j entrypoint reverses this ('__' -> '_', then '_' -> '.'), so the casing
// and underscore escaping both matter — an upper-cased name maps to an invalid
// (upper-cased) setting and is silently ignored. Matches the convention the
// operator already uses for the LDAP system-account env vars.
func Neo4jSettingEnvVarName(key string) string {
	return "NEO4J_" + strings.ReplaceAll(strings.ReplaceAll(key, "_", "__"), ".", "_")
}

// IsAdditiveConfKey reports whether a neo4j.conf setting is an additive,
// comma-separated list (e.g. dbms.security.procedures.unrestricted) whose values
// from multiple sources (operator, user, several plugins) must be unioned rather
// than overwritten — overwriting silently drops the other sources' entries.
func IsAdditiveConfKey(key string) bool { return additiveConfKeys[key] }

// MergeConfListValues returns the comma-separated union of two list values,
// de-duplicated and in first-seen order (a's tokens first, then b's new tokens).
// Used for additive keys both in neo4j.conf and in NEO4J_*-style env vars.
func MergeConfListValues(a, b string) string { return unionCSV(a, b) }

// DedupeNeo4jConf collapses duplicate setting keys in a rendered neo4j.conf so
// the same key is never declared twice. The operator assembles the conf from
// several sections (base → monitoring → audit → auth → user spec.config), and a
// key emitted by more than one section (classically db.logs.query.enabled /
// db.logs.query.threshold, from monitoring AND user spec.config) would appear
// twice. Neo4j 5.26 tolerated that (last wins); CalVer (2025.x+) FAILS to start
// with "<key> declared multiple times".
//
// Resolution rules (so operator-set config is never silently lost):
//   - Scalar keys: keep the LAST occurrence — matching the documented precedence
//     (audit over monitoring; user spec.config over operator defaults) and the
//     old 5.26 last-wins behavior. An operator key with no user duplicate is
//     never dropped (it has no later occurrence).
//   - Additive list keys (additiveConfKeys): MERGE the union of all occurrences'
//     tokens, so the operator's allowlist isn't lost to a user override.
//   - server.jvm.additional / dbms.jvm.additional: left untouched — Neo4j
//     intentionally accepts repeated occurrences and concatenates them.
//
// Comment and blank lines are preserved in place.
func DedupeNeo4jConf(conf string) string {
	lines := strings.Split(conf, "\n")

	settingKV := func(line string) (key, value string, ok bool) {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			return "", "", false
		}
		eq := strings.IndexByte(t, '=')
		if eq <= 0 {
			return "", "", false
		}
		k := strings.TrimSpace(t[:eq])
		if k == "server.jvm.additional" || k == "dbms.jvm.additional" {
			return "", "", false // repeatable — never dedupe
		}
		return k, strings.TrimSpace(t[eq+1:]), true
	}

	lastIdx := make(map[string]int)
	mergedTokens := make(map[string][]string)
	seenToken := make(map[string]map[string]bool)
	for i, line := range lines {
		k, v, ok := settingKV(line)
		if !ok {
			continue
		}
		lastIdx[k] = i
		if additiveConfKeys[k] {
			if seenToken[k] == nil {
				seenToken[k] = map[string]bool{}
			}
			for _, tok := range strings.Split(v, ",") {
				tok = strings.TrimSpace(tok)
				if tok == "" || seenToken[k][tok] {
					continue
				}
				seenToken[k][tok] = true
				mergedTokens[k] = append(mergedTokens[k], tok)
			}
		}
	}

	out := lines[:0:0]
	for i, line := range lines {
		k, _, ok := settingKV(line)
		if !ok {
			out = append(out, line)
			continue
		}
		if lastIdx[k] != i {
			continue // earlier occurrence of a key resolved later — drop it
		}
		if additiveConfKeys[k] {
			out = append(out, fmt.Sprintf("%s=%s", k, strings.Join(mergedTokens[k], ",")))
		} else {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// UpsertNeo4jConfSettings merges externally-provided settings (e.g. a
// Neo4jPlugin's required security settings) into an already-rendered neo4j.conf
// WITHOUT creating duplicate keys — CalVer Neo4j refuses to start on a duplicate
// declaration. It is idempotent (re-applying the same settings yields identical
// output) so it doesn't churn the ConfigMap:
//
//   - Additive list keys (additiveConfKeys, e.g. dbms.security.procedures.*):
//     the setting's tokens are unioned into the existing line in place, so the
//     operator/user allowlist is preserved and the plugin's entries are added;
//     appended if the key is absent.
//   - Other (scalar) keys: added only if absent, so an operator/user value is
//     never clobbered (matching the plugin path's "add if not present" intent).
//
// Keys are processed in sorted order for deterministic output.
func UpsertNeo4jConfSettings(conf string, settings map[string]string) string {
	if len(settings) == 0 {
		return conf
	}
	lines := strings.Split(conf, "\n")

	keyIdx := make(map[string]int)
	keyVal := make(map[string]string)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		eq := strings.IndexByte(t, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(t[:eq])
		if k == "server.jvm.additional" || k == "dbms.jvm.additional" {
			continue // repeatable — leave untouched
		}
		keyIdx[k] = i
		keyVal[k] = strings.TrimSpace(t[eq+1:])
	}

	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := settings[k]
		idx, present := keyIdx[k]
		switch {
		case additiveConfKeys[k] && present:
			merged := unionCSV(keyVal[k], v)
			lines[idx] = fmt.Sprintf("%s=%s", k, merged)
			keyVal[k] = merged
		case !present:
			lines = append(lines, fmt.Sprintf("%s=%s", k, v))
			keyIdx[k] = len(lines) - 1
			keyVal[k] = v
		default:
			// scalar key already present — preserve the existing value
		}
	}
	return strings.Join(lines, "\n")
}

// unionCSV returns the comma-separated union of a and b, de-duplicated and in
// first-seen order (a's tokens first, then b's new tokens).
func unionCSV(a, b string) string {
	seen := make(map[string]bool)
	var toks []string
	for _, s := range []string{a, b} {
		for _, tok := range strings.Split(s, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" || seen[tok] {
				continue
			}
			seen[tok] = true
			toks = append(toks, tok)
		}
	}
	return strings.Join(toks, ",")
}

// BuildMonitoringConfig generates Neo4j config lines for monitoring, metrics exposure, and query logging.
func BuildMonitoringConfig(mon *neo4jv1beta1.MonitoringSpec) string {
	slowThreshold := "5s"
	explainPlan := false
	queryLogLevel := "INFO"
	obfuscateLiterals := false
	if mon != nil {
		if mon.SlowQueryThreshold != "" {
			slowThreshold = mon.SlowQueryThreshold
		}
		explainPlan = mon.ExplainPlan
		if mon.QueryLogLevel != "" {
			queryLogLevel = mon.QueryLogLevel
		}
		obfuscateLiterals = mon.ObfuscateLiterals
	}

	lines := []string{
		"# Prometheus metrics exposure",
		"server.metrics.prometheus.enabled=true",
		fmt.Sprintf("server.metrics.prometheus.endpoint=0.0.0.0:%d", MetricsPort),
		"",
		"# Query logging",
		fmt.Sprintf("db.logs.query.enabled=%s", queryLogLevel),
		fmt.Sprintf("db.logs.query.threshold=%s", slowThreshold),
		fmt.Sprintf("db.logs.query.plan_description_enabled=%t", explainPlan),
		"db.logs.query.parameter_logging_enabled=true",
		fmt.Sprintf("db.logs.query.obfuscate_literals=%t", obfuscateLiterals),
		"",
	}

	// Optional metrics filter
	if mon != nil && mon.MetricsFilter != "" {
		lines = append(lines, fmt.Sprintf("server.metrics.filter=%s", mon.MetricsFilter))
	}

	// Optional metrics prefix
	if mon != nil && mon.MetricsPrefix != "" {
		lines = append(lines, fmt.Sprintf("server.metrics.prefix=%s", mon.MetricsPrefix))
	}

	return strings.Join(lines, "\n") + "\n"
}

// BuildAuditConfig generates Neo4j config lines for compliance-oriented
// audit logging. Emits AFTER BuildMonitoringConfig in the rendered
// neo4j.conf so audit-driven values override the monitoring defaults on
// the keys they both touch (`db.logs.query.obfuscate_literals`,
// `db.logs.query.parameter_logging_enabled`). User-supplied spec.config
// keys still win over both — they're appended last.
//
// Returns an empty string when audit is nil so the caller can
// unconditionally concatenate this into the config without an "if".
//
// Field-by-field emission rules:
//
//   - LogSuccessfulAuthentication: emit only when explicitly set
//     (nil → Neo4j default applies).
//
//   - ObfuscateQueryLiterals: secure-by-default — when audit.Enabled=true
//     AND this field is nil, emit `true`. Otherwise emit the explicit
//     value if set; nothing if unset and Enabled=false.
//
//   - ParameterLogging: emit only when explicitly set.
//
// This means a CR with `spec.audit.enabled: true` and no other audit
// fields gets the secure-by-default obfuscation behavior with one flag,
// while users who want finer control set the individual *bool fields
// directly.
func BuildAuditConfig(audit *neo4jv1beta1.AuditSpec) string {
	if audit == nil {
		return ""
	}

	var lines []string

	if audit.LogSuccessfulAuthentication != nil {
		lines = append(lines,
			fmt.Sprintf("dbms.security.log_successful_authentication=%t", *audit.LogSuccessfulAuthentication))
	}

	// ObfuscateQueryLiterals: explicit value wins; otherwise default to
	// true if the user opted in to the audit block (Enabled=true).
	switch {
	case audit.ObfuscateQueryLiterals != nil:
		lines = append(lines,
			fmt.Sprintf("db.logs.query.obfuscate_literals=%t", *audit.ObfuscateQueryLiterals))
	case audit.Enabled:
		lines = append(lines, "db.logs.query.obfuscate_literals=true")
	}

	if audit.ParameterLogging != nil {
		lines = append(lines,
			fmt.Sprintf("db.logs.query.parameter_logging_enabled=%t", *audit.ParameterLogging))
	}

	if len(lines) == 0 {
		return ""
	}

	// Section header makes the rendered conf easier to read when the user
	// inspects it via `kubectl get configmap ... -o yaml`.
	header := []string{"# Audit logging (compliance) — overrides spec.monitoring on shared keys"}
	return strings.Join(append(header, lines...), "\n") + "\n\n"
}

// IsNeo4jVersion202512OrHigher checks if the Neo4j version supports property sharding.
// Property sharding (Infinigraph) was introduced in 2025.12; calver only — no semver version supports it.
// See: https://neo4j.com/docs/operations-manual/current/scalability/sharded-property-databases/overview/
func IsNeo4jVersion202512OrHigher(imageTag string) bool {
	if imageTag == "" {
		return false
	}

	version, err := neo4j.ParseVersion(imageTag)
	if err != nil || !version.IsCalver {
		return false
	}

	if version.Major > 2025 {
		return true
	}

	return version.Major == 2025 && version.Minor >= 12
}

// IsNeo4jVersion202604OrHigher checks if the Neo4j version ships ServerSeedProvider
// (the `server://` URI-scheme seed provider for catchup-style seeding from an
// existing server). Introduced in 2026.04; CalVer-only. Older versions either
// don't have the class or registered it under a different name, so adding
// "ServerSeedProvider" to `dbms.databases.seed_from_uri_providers` on a pre-2026.04
// install can cause Neo4j to log a warning (or refuse to start, depending on the
// release).
func IsNeo4jVersion202604OrHigher(imageTag string) bool {
	if imageTag == "" {
		return false
	}

	version, err := neo4j.ParseVersion(imageTag)
	if err != nil || !version.IsCalver {
		return false
	}

	if version.Major > 2026 {
		return true
	}

	return version.Major == 2026 && version.Minor >= 4
}

// SeedFromURIProvidersConfigValue returns the value the operator should emit for
// `dbms.databases.seed_from_uri_providers` given a Neo4j image tag. The base
// list registers every modern provider so all documented URI schemes
// (s3/gs/azb via CloudSeedProvider; file:// via FileSeedProvider;
// ftp/http/https via URLConnectionSeedProvider) resolve out of the box.
//
// `ServerSeedProvider` is appended only on Neo4j 2026.04+ — it was introduced
// in that release; pre-2026.04 the class isn't registered in
// META-INF/services and Neo4j will log "provider not found" warnings or
// refuse to bootstrap. Deprecated `S3SeedProvider` is deliberately excluded
// across all versions — CloudSeedProvider handles s3:// via the SDK default
// credential chain, making the legacy `seedCredentials` field unnecessary.
//
// Users can override the entire list via spec.config (cluster) or via
// configLines spec equivalent (standalone) — both builders append user keys
// last so anything the user sets wins.
func SeedFromURIProvidersConfigValue(imageTag string) string {
	providers := "CloudSeedProvider,FileSeedProvider,URLConnectionSeedProvider"
	if IsNeo4jVersion202604OrHigher(imageTag) {
		providers += ",ServerSeedProvider"
	}
	return providers
}

// buildPropertyShardingConfig merges required property sharding settings with user overrides
func buildPropertyShardingConfig(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) map[string]string {
	config := map[string]string{
		"internal.dbms.sharded_property_database.enabled":                     "true",
		"internal.dbms.sharded_property_database.allow_external_shard_access": "false",
		"db.query.default_language":                                           "CYPHER_25",
	}

	if cluster.Spec.PropertySharding != nil && cluster.Spec.PropertySharding.Config != nil {
		for key, value := range cluster.Spec.PropertySharding.Config {
			config[key] = value
		}
	}

	return config
}

func buildStartupScriptForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	// Unified startup script for all deployments
	return `#!/bin/bash
set -e

echo "Starting Neo4j Enterprise in cluster mode..."

# Set proper NEO4J_AUTH format (username/password)
export NEO4J_AUTH="${DB_USERNAME}/${DB_PASSWORD}"

# Extract server index from pod hostname BEFORE overriding HOSTNAME.
# StatefulSet pod hostnames follow the pattern: {cluster-name}-server-{ordinal}
# e.g. "my-cluster-server-0" -> SERVER_INDEX="0"
# NEO4J_SERVER_NAME is a static value ("server") and cannot be used for index extraction.
SERVER_INDEX="${HOSTNAME##*-}"

# Set fully qualified domain name for clustering
export HOSTNAME_FQDN="${HOSTNAME}.` + cluster.Name + `-headless.` + cluster.Namespace + `.svc.cluster.local"
echo "Pod hostname: ${HOSTNAME}"
echo "Pod FQDN: ${HOSTNAME_FQDN}"
echo "Server name: ${NEO4J_SERVER_NAME}"
echo "Server index: ${SERVER_INDEX}"

# Override the HOSTNAME variable with FQDN for Neo4j configuration
export HOSTNAME="${HOSTNAME_FQDN}"

# Create writable config directory
mkdir -p /tmp/neo4j-config

# Copy base config
cp /conf/neo4j.conf /tmp/neo4j-config/neo4j.conf

# Add FQDN-based advertised addresses
# Port assignment (same for 5.26.x and all CalVer releases):
#   5000 = tcp-discovery: legacy V1 discovery port (DEPRECATED, not used by this operator)
#   6000 = tcp-tx: V2 discovery + cluster catchup traffic (server.cluster.advertised_address)
#   7000 = raft: RAFT consensus (server.cluster.raft.advertised_address)
cat >> /tmp/neo4j-config/neo4j.conf << EOF

# Advertised addresses using pod FQDN (applies to all supported versions)
server.default_advertised_address=${HOSTNAME_FQDN}
server.cluster.advertised_address=${HOSTNAME_FQDN}:6000
server.routing.advertised_address=${HOSTNAME_FQDN}:7688
server.cluster.raft.advertised_address=${HOSTNAME_FQDN}:7000
EOF

# Cluster configuration based on topology
TOTAL_SERVERS=` + fmt.Sprintf("%d", cluster.Spec.Topology.Servers) + `

echo "Cluster topology: ${TOTAL_SERVERS} servers"
echo "Server index: ${SERVER_INDEX}"

# Neo4jEnterpriseCluster uses server-based clustering
# Minimum: 2 servers (servers self-organize for database hosting)
echo "Multi-server cluster: using LIST discovery with static pod FQDNs"

` + buildBootstrapStrategyShellBlock(cluster) + `

cat >> /tmp/neo4j-config/neo4j.conf << EOF

# Multi-node cluster using LIST discovery with static pod FQDNs via headless service.
# LIST discovery provides deterministic peer addresses (one per pod) unlike K8S ClusterIP
# which returns a single VIP. This ensures all TOTAL_SERVERS members are discovered
# before RAFT elects the bootstrap server, preventing split-brain formation.
` + buildVersionSpecificDiscoveryConfig(cluster) + `
EOF

# Only set minimum_initial_system_primaries_count on INITIAL cluster formation.
# On pod restarts (data already exists), skip this so the server rejoins immediately
# without waiting for all peers to be visible (avoids blocking StatefulSet rolling updates).
if [ ! -d "/data/databases/system" ]; then
    echo "Initial formation: setting ` + getMinInitialPrimariesSetting(cluster) + `=${TOTAL_SERVERS}"
    echo "` + getMinInitialPrimariesSetting(cluster) + `=${TOTAL_SERVERS}" >> /tmp/neo4j-config/neo4j.conf
else
    echo "Restart detected (/data/databases/system exists) - skipping minimum primaries count"
fi

# Add server mode constraint if specified
` + buildServerModeConstraintConfig(cluster) + `

# Set NEO4J config directory
export NEO4J_CONF=/tmp/neo4j-config

# Start Neo4j
exec /startup/docker-entrypoint.sh neo4j
`
}

// buildServerModeConstraintConfig generates server mode constraint configuration
func buildServerModeConstraintConfig(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	config := ""

	// Check if we have per-server role hints
	if len(cluster.Spec.Topology.ServerRoles) > 0 {
		// Build per-server role configuration
		config += `
# Per-server mode constraint configuration based on role hints
# Check if this server has a specific role hint
SERVER_MODE_CONSTRAINT="NONE"
`
		// Add conditional logic for each server role hint
		for _, roleHint := range cluster.Spec.Topology.ServerRoles {
			config += fmt.Sprintf(`if [ "$SERVER_INDEX" = "%d" ]; then
    SERVER_MODE_CONSTRAINT="%s"
    echo "Server %d: Setting mode constraint to %s based on role hint"
fi
`, roleHint.ServerIndex, roleHint.ModeConstraint, roleHint.ServerIndex, roleHint.ModeConstraint)
		}

		config += `
# Apply the server mode constraint if not NONE
if [ "$SERVER_MODE_CONSTRAINT" != "NONE" ]; then
cat >> /tmp/neo4j-config/neo4j.conf << EOF
# Server mode constraint for this specific server
initial.server.mode_constraint=$SERVER_MODE_CONSTRAINT
EOF
fi
`
	} else if cluster.Spec.Topology.ServerModeConstraint != "" && cluster.Spec.Topology.ServerModeConstraint != "NONE" {
		// Fall back to global server mode constraint
		config = fmt.Sprintf(`
# Global server mode constraint configuration
cat >> /tmp/neo4j-config/neo4j.conf << EOF
# Constrain all servers to %s mode
initial.server.mode_constraint=%s
EOF
`, cluster.Spec.Topology.ServerModeConstraint, cluster.Spec.Topology.ServerModeConstraint)
	}

	return config
}

// isCalverImage returns true when the image tag is a CalVer release (2025.x+).
// Uses proper version parsing rather than a simple string prefix so it remains
// correct for future CalVer years (2026.x, 2027.x, …).
func isCalverImage(tag string) bool {
	v, err := neo4j.ParseVersion(tag)
	if err != nil {
		return false
	}
	return v.IsCalver
}

// buildVersionSpecificDiscoveryConfig generates the full discovery block for neo4j.conf.
//
// Source: Neo4j Ops Manual — Cluster Server Discovery
//
//	5.26.x docs: https://neo4j.com/docs/operations-manual/5/clustering/setup/discovery/
//	2025.x+ docs: https://neo4j.com/docs/operations-manual/current/clustering/setup/discovery/
//
// Port 6000 (tcp-tx) is the V2 cluster communication port used by both versions.
// Port 5000 (tcp-discovery) was the V1 discovery port — deprecated, never used here.
//
// 5.26.x (SemVer) — V2 is opt-in, V1 is the default:
//   - dbms.cluster.discovery.resolver_type=LIST
//   - dbms.cluster.discovery.version=V2_ONLY  ← must be set explicitly
//   - dbms.cluster.discovery.v2.endpoints=<pod-fqdns>:6000
//   - internal.dbms.cluster.discovery.system_bootstrapping_strategy (server-0=me, rest=other)
//
// 2025.x+ (CalVer, including 2026.x+) — V2 is the only supported protocol:
//   - dbms.cluster.discovery.resolver_type=LIST  ← still required
//   - dbms.cluster.endpoints=<pod-fqdns>:6000    ← renamed from dbms.cluster.discovery.v2.endpoints
//   - NO dbms.cluster.discovery.version flag     ← not recognised; V2 is always active
func buildVersionSpecificDiscoveryConfig(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	calver := isCalverImage(cluster.Spec.Image.Tag)

	addrs := make([]string, cluster.Spec.Topology.Servers)
	for i := int32(0); i < cluster.Spec.Topology.Servers; i++ {
		addrs[i] = fmt.Sprintf("%s-server-%d.%s-headless.%s.svc.cluster.local:6000",
			cluster.Name, i, cluster.Name, cluster.Namespace)
	}
	endpointList := strings.Join(addrs, ",")

	if calver {
		// CalVer 2025.x+: per the official Neo4j clustering docs, LIST discovery requires
		// BOTH resolver_type=LIST AND dbms.cluster.endpoints (the renamed v2.endpoints).
		// V2 is the only supported protocol; dbms.cluster.discovery.version is not needed.
		// See: https://neo4j.com/docs/operations-manual/current/clustering/setup/discovery/
		return `# CalVer (2025.x+): LIST discovery — resolver_type + dbms.cluster.endpoints
dbms.cluster.discovery.resolver_type=LIST
dbms.cluster.endpoints=` + endpointList + `
dbms.routing.default_router=SERVER
initial.dbms.automatically_enable_free_servers=true`
	}

	// SemVer 5.26.x: must explicitly enable V2_ONLY and use the v2.endpoints key.
	// The internal bootstrapping_strategy hint steers server-0 to bootstrap first,
	// avoiding a race where two nodes simultaneously attempt to form a cluster.
	return `# SemVer 5.26.x: LIST discovery with explicit V2_ONLY mode
dbms.cluster.discovery.resolver_type=LIST
dbms.cluster.discovery.version=V2_ONLY
dbms.cluster.discovery.v2.endpoints=` + endpointList + `

# Bootstrapping strategy: server-0 (me) bootstraps; all others (other) join.
internal.dbms.cluster.discovery.system_bootstrapping_strategy=${BOOTSTRAP_STRATEGY}

initial.dbms.automatically_enable_free_servers=true

# Cluster formation optimization
dbms.cluster.raft.binding_timeout=1d
dbms.cluster.raft.membership.join_timeout=10m
dbms.routing.default_router=SERVER

# Discovery resolution timeout
internal.dbms.cluster.discovery.resolution_timeout=1d`
}

// getMinInitialPrimariesSetting returns the config key for the
// "minimum primaries before bootstrap" guard. The key is the same
// in both Neo4j 5.26.x and 2025.x CalVer.
func getMinInitialPrimariesSetting(_ *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	return "dbms.cluster.minimum_initial_system_primaries_count"
}

// buildBootstrapStrategyShellBlock returns the shell snippet that assigns
// BOOTSTRAP_STRATEGY (me on server-0, other elsewhere) before
// buildVersionSpecificDiscoveryConfig inserts ${BOOTSTRAP_STRATEGY} into
// the SemVer-only internal.dbms.cluster.discovery.system_bootstrapping_strategy
// directive.
//
// CalVer (2025.x+) does not honour system_bootstrapping_strategy at all — the
// V2 discovery protocol elects a bootstrapper from the LIST endpoints
// without an explicit hint — so on CalVer the variable would be assigned
// and never consumed. Return an empty string so the dead assignment isn't
// even emitted into the startup script.
func buildBootstrapStrategyShellBlock(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	if isCalverImage(cluster.Spec.Image.Tag) {
		return `# CalVer (2025.x+): V2 discovery elects a bootstrapper from the LIST endpoints;
# internal.dbms.cluster.discovery.system_bootstrapping_strategy is SemVer-only,
# so no BOOTSTRAP_STRATEGY assignment is emitted here.`
	}
	return `# ME/OTHER bootstrap strategy: server-0 bootstraps, all others join.
# With Parallel pod management all pods start simultaneously. Using LIST discovery
# with static pod FQDNs (via the headless service DNS) and minimum_initial_system_primaries_count
# set to TOTAL_SERVERS ensures all servers discover each other before RAFT election.
# Server-0 (me) is preferred bootstrapper; all others (other) join when ready.
if [ "$SERVER_INDEX" = "0" ]; then
    echo "Server 0: Using bootstrapping strategy 'me' (preferred cluster bootstrapper)"
    BOOTSTRAP_STRATEGY="me"
else
    echo "Server ${SERVER_INDEX}: Using bootstrapping strategy 'other' (joining cluster)"
    BOOTSTRAP_STRATEGY="other"
fi
echo "Configuring cluster with bootstrap strategy: ${BOOTSTRAP_STRATEGY}"`
}

// ValidateServerRoleHints validates server role hints configuration
func ValidateServerRoleHints(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) []string {
	var errors []string

	if len(cluster.Spec.Topology.ServerRoles) == 0 {
		return errors // No validation needed if no role hints specified
	}

	serverCount := cluster.Spec.Topology.Servers
	usedIndices := make(map[int32]bool)

	for _, roleHint := range cluster.Spec.Topology.ServerRoles {
		// Check server index is within valid range
		if roleHint.ServerIndex < 0 || roleHint.ServerIndex >= serverCount {
			errors = append(errors, fmt.Sprintf("server role hint index %d is out of range (0-%d)", roleHint.ServerIndex, serverCount-1))
		}

		// Check for duplicate server indices
		if usedIndices[roleHint.ServerIndex] {
			errors = append(errors, fmt.Sprintf("duplicate server role hint for server index %d", roleHint.ServerIndex))
		}
		usedIndices[roleHint.ServerIndex] = true

		// Validate mode constraint value (this should be caught by CRD validation, but double-check)
		validModes := map[string]bool{"NONE": true, "PRIMARY": true, "SECONDARY": true}
		if !validModes[roleHint.ModeConstraint] {
			errors = append(errors, fmt.Sprintf("invalid mode constraint '%s' for server %d (valid values: NONE, PRIMARY, SECONDARY)", roleHint.ModeConstraint, roleHint.ServerIndex))
		}
	}

	// Warn if all servers are constrained to SECONDARY (cluster would have no primaries)
	allSecondary := true
	allPrimary := true
	for _, roleHint := range cluster.Spec.Topology.ServerRoles {
		if roleHint.ModeConstraint != "SECONDARY" {
			allSecondary = false
		}
		if roleHint.ModeConstraint != "PRIMARY" {
			allPrimary = false
		}
	}

	// Check if we have role hints for all servers
	if int32(len(cluster.Spec.Topology.ServerRoles)) == serverCount {
		if allSecondary {
			errors = append(errors, "all servers are constrained to SECONDARY mode - cluster would have no primary servers available")
		}
		if allPrimary && serverCount > 1 {
			// This is actually valid, but might want to warn about no dedicated secondaries
			// Not adding this as an error since it's a valid configuration
		}
	}

	return errors
}

func buildHealthScript(_ *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	// Enhanced health check for cluster deployments
	return `#!/bin/bash
# Health check script for Neo4j clustering

# Check if Neo4j process is running
if ! (pgrep -f "EnterpriseEntryPoint" > /dev/null || pgrep -f "Neo4jEnterprise" > /dev/null); then
    echo "Neo4j process not running"
    exit 1
fi

# Try HTTP port check
if (echo > /dev/tcp/localhost/7474) >/dev/null 2>&1; then
    echo "Neo4j HTTP port responding - healthy"
    exit 0
fi

# If HTTP not responding, check if we're in cluster formation process
if grep -q "Resolved endpoints" /logs/neo4j.log 2>/dev/null || \
   grep -q "Starting.*cluster" /logs/neo4j.log 2>/dev/null || \
   grep -q "Waiting for.*servers" /logs/neo4j.log 2>/dev/null || \
   grep -q "minimum_initial_system_primaries_count" /logs/neo4j.log 2>/dev/null || \
   grep -q "cluster formation barrier" /logs/neo4j.log 2>/dev/null; then
    echo "Neo4j in cluster formation process - allowing more time"
    exit 0
fi

# If process is running but no clustering activity, fail
echo "Neo4j process running but HTTP port not responding and no cluster activity detected"
exit 1
`
}

// buildReadinessProbe creates a readiness probe
func buildReadinessProbe(_ *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/bin/bash",
					"-c",
					"/conf/health.sh",
				},
			},
		},
		InitialDelaySeconds: 45, // Allow time for cluster discovery and joining
		PeriodSeconds:       15, // Less frequent checks during startup
		TimeoutSeconds:      5,
		FailureThreshold:    8, // Allow up to 2 minutes after initial delay for cluster rejoin scenarios
	}
}

// buildLivenessProbe creates a liveness probe
func buildLivenessProbe(_ *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/bin/bash",
					"-c",
					"/conf/health.sh",
				},
			},
		},
		InitialDelaySeconds: 120, // Allow sufficient time for joining pods to connect
		PeriodSeconds:       60,  // Less frequent checks to avoid interrupting cluster operations
		TimeoutSeconds:      10,
		FailureThreshold:    3,
	}
}

// buildJVMSettings builds optimized JVM settings for Neo4j
func buildJVMSettings(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	// Check if user has already set JVM settings via environment variable
	for _, env := range cluster.Spec.Env {
		if env.Name == "NEO4J_server_jvm_additional" {
			// User has explicitly set JVM settings, don't override
			return ""
		}
	}

	// Check if user has set via config
	if val, exists := cluster.Spec.Config["server.jvm.additional"]; exists && val != "" {
		return val
	}

	// Build production-ready JVM settings
	var jvmFlags []string

	// Use G1GC for better pause times with large heaps
	jvmFlags = append(jvmFlags, "-XX:+UseG1GC")

	// Target max GC pause time
	jvmFlags = append(jvmFlags, "-XX:MaxGCPauseMillis=200")

	// Enable parallel reference processing for better GC performance
	jvmFlags = append(jvmFlags, "-XX:+ParallelRefProcEnabled")

	// G1GC tuning for Neo4j workloads
	jvmFlags = append(jvmFlags, "-XX:+UnlockExperimentalVMOptions")
	jvmFlags = append(jvmFlags, "-XX:+UnlockDiagnosticVMOptions")
	jvmFlags = append(jvmFlags, "-XX:G1NewSizePercent=2")
	jvmFlags = append(jvmFlags, "-XX:G1MaxNewSizePercent=10")

	// Adaptive IHOP (Initiating Heap Occupancy Percent)
	jvmFlags = append(jvmFlags, "-XX:+G1UseAdaptiveIHOP")
	jvmFlags = append(jvmFlags, "-XX:InitiatingHeapOccupancyPercent=45")

	// Enable compressed OOPs for heaps up to 32GB (saves ~30% memory)
	// Automatically enabled for heaps < 32GB but explicit is better
	jvmFlags = append(jvmFlags, "-XX:+UseCompressedOops")
	jvmFlags = append(jvmFlags, "-XX:+UseCompressedClassPointers")

	// String deduplication can help with Neo4j's string-heavy workloads
	jvmFlags = append(jvmFlags, "-XX:+UseStringDeduplication")

	// Exit on OOM for better container behavior
	jvmFlags = append(jvmFlags, "-XX:+ExitOnOutOfMemoryError")

	// Add JVM truststore flags whenever any trusted CA is configured (either
	// the legacy spec.auth.trustStore or the new spec.trustedCASecrets list).
	hasTrustedCAs := len(cluster.Spec.TrustedCASecrets) > 0 ||
		(cluster.Spec.Auth != nil && cluster.Spec.Auth.TrustStore != nil)
	if hasTrustedCAs {
		jvmFlags = append(jvmFlags,
			"-Djavax.net.ssl.trustStore=/truststore/truststore.jks",
			"-Djavax.net.ssl.trustStorePassword=changeit",
		)
	}

	// Optional: Enable GC logging for debugging (commented out by default)
	// jvmFlags = append(jvmFlags, "-Xlog:gc*:file=/logs/gc.log:time,uptime,level,tags:filecount=5,filesize=10m")

	return strings.Join(jvmFlags, " ")
}

// buildStartupProbe creates a startup probe for initial cluster formation
func buildStartupProbe(_ *neo4jv1beta1.Neo4jEnterpriseCluster) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/bin/bash",
					"-c",
					"/conf/health.sh",
				},
			},
		},
		InitialDelaySeconds: 30, // Start checking after 30 seconds
		PeriodSeconds:       10, // Check every 10 seconds during startup
		TimeoutSeconds:      5,
		FailureThreshold:    60, // Allow up to 10 minutes for startup (60 * 10s)
		SuccessThreshold:    1,
	}
}

// calculateTransactionMemoryLimit calculates the global transaction memory limit
// Defaults to 70% of heap to leave room for other operations
func calculateTransactionMemoryLimit(heapSize string, config map[string]string) string {
	// Check if user has explicitly set it
	if val, exists := config["dbms.memory.transaction.total.max"]; exists && val != "" {
		return val
	}

	// Parse heap size and calculate 70%
	heapBytes := parseMemoryString(heapSize)
	if heapBytes == 0 {
		return "2g" // Safe default
	}

	transactionMemory := int64(float64(heapBytes) * 0.7)
	return formatMemorySizeForNeo4j(transactionMemory)
}

// calculatePerTransactionLimit calculates the per-transaction memory limit
// Defaults to 10% of the global transaction limit
func calculatePerTransactionLimit(heapSize string, config map[string]string) string {
	// Check if user has explicitly set it
	if val, exists := config["db.memory.transaction.max"]; exists && val != "" {
		return val
	}

	// Get the global limit first
	globalLimit := calculateTransactionMemoryLimit(heapSize, config)
	globalBytes := parseMemoryString(globalLimit)
	if globalBytes == 0 {
		return "256m" // Safe default
	}

	perTransactionMemory := int64(float64(globalBytes) * 0.1)
	// Minimum 256MB per transaction
	if perTransactionMemory < 256*1024*1024 {
		perTransactionMemory = 256 * 1024 * 1024
	}
	return formatMemorySizeForNeo4j(perTransactionMemory)
}

// calculatePerDatabaseLimit calculates the per-database transaction memory limit
// Defaults to 50% of global limit to allow multiple databases
func calculatePerDatabaseLimit(heapSize string, config map[string]string) string {
	// Check if user has explicitly set it
	if val, exists := config["db.memory.transaction.total.max"]; exists && val != "" {
		return val
	}

	// Get the global limit
	globalLimit := calculateTransactionMemoryLimit(heapSize, config)
	globalBytes := parseMemoryString(globalLimit)
	if globalBytes == 0 {
		return "1g" // Safe default
	}

	perDatabaseMemory := int64(float64(globalBytes) * 0.5)
	return formatMemorySizeForNeo4j(perDatabaseMemory)
}

// parseMemoryString parses Neo4j memory string to bytes
func parseMemoryString(memStr string) int64 {
	if memStr == "" {
		return 0
	}

	memStr = strings.ToLower(strings.TrimSpace(memStr))

	var multiplier int64
	var numStr string

	switch {
	case strings.HasSuffix(memStr, "g"):
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(memStr, "g")
	case strings.HasSuffix(memStr, "m"):
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(memStr, "m")
	case strings.HasSuffix(memStr, "k"):
		multiplier = 1024
		numStr = strings.TrimSuffix(memStr, "k")
	default:
		// Try to parse as raw number (bytes)
		if num, err := strconv.ParseInt(memStr, 10, 64); err == nil {
			return num
		}
		return 0
	}

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0
	}

	return int64(num * float64(multiplier))
}

// formatMemorySizeForNeo4j formats bytes to Neo4j memory string
func formatMemorySizeForNeo4j(bytes int64) string {
	const (
		GB = 1024 * 1024 * 1024
		MB = 1024 * 1024
		KB = 1024
	)

	switch {
	case bytes >= GB && bytes%GB == 0:
		return fmt.Sprintf("%dg", bytes/GB)
	case bytes >= GB:
		return fmt.Sprintf("%.1fg", float64(bytes)/float64(GB))
	case bytes >= MB && bytes%MB == 0:
		return fmt.Sprintf("%dm", bytes/MB)
	case bytes >= MB:
		return fmt.Sprintf("%.1fm", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%dk", bytes/KB)
	default:
		return fmt.Sprintf("%d", bytes)
	}
}

// Helper functions for Kubernetes discovery resources
func getDiscoveryServiceAccountNameForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	return fmt.Sprintf("%s-discovery", cluster.Name)
}

func getDiscoveryRoleNameForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	return fmt.Sprintf("%s-discovery", cluster.Name)
}

func getDiscoveryRoleBindingNameForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	return fmt.Sprintf("%s-discovery", cluster.Name)
}

// AuthConfigResult holds the generated neo4j.conf auth config and the list of keys it produced.
type AuthConfigResult struct {
	// Config is the generated neo4j.conf lines for authentication/authorization
	Config string
	// GeneratedKeys lists all config keys that were generated (for dedup with spec.config)
	GeneratedKeys []string
}

// BuildAuthConfig converts typed AuthSpec fields into neo4j.conf configuration lines.
// Sensitive values (LDAP system password) are NOT included — they are injected via env vars.
func BuildAuthConfig(auth *neo4jv1beta1.AuthSpec) AuthConfigResult {
	if auth == nil {
		return AuthConfigResult{}
	}

	var lines []string
	var keys []string

	// Resolve provider lists
	authnProviders := auth.AuthenticationProviders
	authzProviders := auth.AuthorizationProviders

	if len(authnProviders) > 0 {
		lines = append(lines, fmt.Sprintf("dbms.security.authentication_providers=%s", strings.Join(authnProviders, ",")))
		keys = append(keys, "dbms.security.authentication_providers")
	}
	if len(authzProviders) > 0 {
		lines = append(lines, fmt.Sprintf("dbms.security.authorization_providers=%s", strings.Join(authzProviders, ",")))
		keys = append(keys, "dbms.security.authorization_providers")
	}

	// LDAP configuration
	if auth.LDAP != nil {
		ldapLines, ldapKeys := buildLDAPConfig(auth.LDAP)
		lines = append(lines, ldapLines...)
		keys = append(keys, ldapKeys...)
	}

	// OIDC multi-provider configuration
	if len(auth.OIDC) > 0 {
		// Sort provider names for deterministic output
		var names []string
		for name := range auth.OIDC {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			provider := auth.OIDC[name]
			oidcLines, oidcKeys := buildOIDCProviderConfig(name, &provider)
			lines = append(lines, oidcLines...)
			keys = append(keys, oidcKeys...)
		}
	}

	// Auth cache TTL
	if auth.AuthCacheTTL != "" {
		lines = append(lines, fmt.Sprintf("dbms.security.auth_cache_ttl=%s", auth.AuthCacheTTL))
		keys = append(keys, "dbms.security.auth_cache_ttl")
	}

	if len(lines) == 0 {
		return AuthConfigResult{}
	}

	return AuthConfigResult{
		Config:        strings.Join(lines, "\n") + "\n",
		GeneratedKeys: keys,
	}
}

// buildLDAPConfig generates neo4j.conf lines for LDAP configuration.
// System account credentials are excluded — they are injected via env vars.
func buildLDAPConfig(ldap *neo4jv1beta1.Neo4jLDAPSpec) ([]string, []string) {
	var lines []string
	var keys []string

	addLine := func(key, value string) {
		lines = append(lines, fmt.Sprintf("%s=%s", key, value))
		keys = append(keys, key)
	}

	addLine("dbms.security.ldap.host", ldap.Host)

	// Default use_starttls=true for plain ldap:// hosts as a
	// secure-by-default per the Neo4j security checklist
	// ("Configure your LDAP system with encryption via StartTLS.").
	// Neo4j's own default is false, which silently leaves the bind
	// unencrypted on plain ldap:// — the most common LDAP misconfig
	// in production deployments.
	//
	// Skip the default flip for ldaps:// hosts (already encrypted at
	// the protocol level — StartTLS is a no-op there and emitting the
	// line would just add noise to the rendered config).
	//
	// Dev setups using mock LDAP without TLS must explicitly set
	// useStartTLS: false to opt out — that's the deliberate path; a
	// silent unencrypted bind in prod is not.
	switch {
	case ldap.UseStartTLS != nil:
		addLine("dbms.security.ldap.use_starttls", fmt.Sprintf("%t", *ldap.UseStartTLS))
	case strings.HasPrefix(strings.ToLower(strings.TrimSpace(ldap.Host)), "ldap://"):
		addLine("dbms.security.ldap.use_starttls", "true")
	}

	// Authentication settings
	if ldap.Authentication != nil {
		auth := ldap.Authentication
		if auth.UserDNTemplate != "" {
			addLine("dbms.security.ldap.authentication.user_dn_template", auth.UserDNTemplate)
		}
		if auth.SearchForAttribute != nil {
			addLine("dbms.security.ldap.authentication.search_for_attribute", fmt.Sprintf("%t", *auth.SearchForAttribute))
		}
		if auth.Attribute != "" {
			addLine("dbms.security.ldap.authentication.attribute", auth.Attribute)
		}
		if auth.CacheEnabled != nil {
			addLine("dbms.security.ldap.authentication.cache_enabled", fmt.Sprintf("%t", *auth.CacheEnabled))
		}
	}

	// Authorization settings
	if ldap.Authorization != nil {
		authz := ldap.Authorization
		if authz.UserSearchBase != "" {
			addLine("dbms.security.ldap.authorization.user_search_base", authz.UserSearchBase)
		}
		if authz.UserSearchFilter != "" {
			addLine("dbms.security.ldap.authorization.user_search_filter", authz.UserSearchFilter)
		}
		if len(authz.GroupMembershipAttributes) > 0 {
			addLine("dbms.security.ldap.authorization.group_membership_attributes", strings.Join(authz.GroupMembershipAttributes, ","))
		}
		if len(authz.GroupToRoleMapping) > 0 {
			addLine("dbms.security.ldap.authorization.group_to_role_mapping", serializeGroupToRoleMapping(authz.GroupToRoleMapping))
		}
		if authz.AccessPermittedGroup != "" {
			addLine("dbms.security.ldap.authorization.access_permitted_group", authz.AccessPermittedGroup)
		}
		if authz.UseSystemAccount != nil && *authz.UseSystemAccount {
			addLine("dbms.security.ldap.authorization.use_system_account", "true")
			// NOTE: system_username and system_password are injected via env vars, not here
		}
		if authz.NestedGroupsEnabled != nil {
			addLine("dbms.security.ldap.authorization.nested_groups_enabled", fmt.Sprintf("%t", *authz.NestedGroupsEnabled))
		}
		if authz.NestedGroupsSearchFilter != "" {
			addLine("dbms.security.ldap.authorization.nested_groups_search_filter", authz.NestedGroupsSearchFilter)
		}
	}

	// Debug logging
	if ldap.DebugGroupLogging != nil {
		addLine("dbms.security.logs.ldap.groups_at_debug_level_enabled", fmt.Sprintf("%t", *ldap.DebugGroupLogging))
	}

	return lines, keys
}

// buildOIDCProviderConfig generates neo4j.conf lines for a single OIDC provider.
func buildOIDCProviderConfig(name string, provider *neo4jv1beta1.Neo4jOIDCProviderSpec) ([]string, []string) {
	var lines []string
	var keys []string
	prefix := fmt.Sprintf("dbms.security.oidc.%s", name)

	addLine := func(suffix, value string) {
		key := fmt.Sprintf("%s.%s", prefix, suffix)
		lines = append(lines, fmt.Sprintf("%s=%s", key, value))
		keys = append(keys, key)
	}

	if provider.DisplayName != "" {
		addLine("display_name", provider.DisplayName)
	}
	if provider.WellKnownDiscoveryURI != "" {
		addLine("well_known_discovery_uri", provider.WellKnownDiscoveryURI)
	}
	if provider.AuthEndpoint != "" {
		addLine("auth_endpoint", provider.AuthEndpoint)
	}
	if provider.TokenEndpoint != "" {
		addLine("token_endpoint", provider.TokenEndpoint)
	}
	if provider.JWKSURI != "" {
		addLine("jwks_uri", provider.JWKSURI)
	}
	if provider.UserInfoURI != "" {
		addLine("user_info_uri", provider.UserInfoURI)
	}
	if provider.Issuer != "" {
		addLine("issuer", provider.Issuer)
	}
	addLine("audience", provider.Audience)
	if provider.AuthFlow != "" {
		addLine("auth_flow", provider.AuthFlow)
	}

	// Claims
	if provider.Claims != nil {
		if provider.Claims.Username != "" {
			addLine("claims.username", provider.Claims.Username)
		}
		if provider.Claims.Groups != "" {
			addLine("claims.groups", provider.Claims.Groups)
		}
	}

	if provider.GetGroupsFromUserInfo != nil {
		addLine("get_groups_from_user_info", fmt.Sprintf("%t", *provider.GetGroupsFromUserInfo))
	}
	if provider.GetUsernameFromUserInfo != nil {
		addLine("get_username_from_user_info", fmt.Sprintf("%t", *provider.GetUsernameFromUserInfo))
	}

	if len(provider.GroupToRoleMapping) > 0 {
		addLine("authorization.group_to_role_mapping", serializeGroupToRoleMapping(provider.GroupToRoleMapping))
	}

	if provider.AuthParams != "" {
		addLine("auth_params", provider.AuthParams)
	}
	if provider.TokenParams != "" {
		addLine("token_params", provider.TokenParams)
	}

	return lines, keys
}

// serializeGroupToRoleMapping converts a map[string]string to Neo4j's semicolon-separated format:
// "group1"=role1,role2;"group2"=role3
func serializeGroupToRoleMapping(mapping map[string]string) string {
	// Sort keys for deterministic output
	var groupDNs []string
	for dn := range mapping {
		groupDNs = append(groupDNs, dn)
	}
	sort.Strings(groupDNs)

	var parts []string
	for _, dn := range groupDNs {
		roles := mapping[dn]
		parts = append(parts, fmt.Sprintf(`"%s"=%s`, dn, roles))
	}
	return strings.Join(parts, ";")
}

// BuildAuthEnvVars returns env vars for secret injection (LDAP system account credentials).
// These are injected as env vars so sensitive values never appear in the ConfigMap.
func BuildAuthEnvVars(auth *neo4jv1beta1.AuthSpec) []corev1.EnvVar {
	if auth == nil || auth.LDAP == nil || auth.LDAP.Authorization == nil {
		return nil
	}
	authz := auth.LDAP.Authorization
	if authz.UseSystemAccount == nil || !*authz.UseSystemAccount || authz.SystemAccountSecretRef == "" {
		return nil
	}

	return []corev1.EnvVar{
		{
			Name: "NEO4J_dbms_security_ldap_authorization_system__username",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: authz.SystemAccountSecretRef,
					},
					Key: "username",
				},
			},
		},
		{
			Name: "NEO4J_dbms_security_ldap_authorization_system__password",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: authz.SystemAccountSecretRef,
					},
					Key: "password",
				},
			},
		},
	}
}

// trustedCASecretVolumeName returns the volume name for a single trusted-CA Secret.
// We namespace these so they don't collide with the truststore EmptyDir or each other.
func trustedCASecretVolumeName(secretName string) string {
	return "trusted-ca-" + secretName
}

// trustedCASecretMountPath returns the mount path for a single trusted-CA Secret
// inside the init container.
func trustedCASecretMountPath(secretName string) string {
	return "/trusted-ca/" + secretName
}

// CollectTrustedCASecrets folds the legacy singular `spec.auth.trustStore`
// field into the new plural `spec.trustedCASecrets` list, returning the merged
// set the resource builder should wire up. The legacy entry is normalised to
// a TrustedCASecret with key default "ca.crt" if not specified. Duplicate
// names are de-duplicated, with the explicit list winning over the legacy
// field.
func CollectTrustedCASecrets(legacy *neo4jv1beta1.SecretKeyRef, plural []neo4jv1beta1.TrustedCASecret) []neo4jv1beta1.TrustedCASecret {
	out := make([]neo4jv1beta1.TrustedCASecret, 0, len(plural)+1)
	seen := map[string]struct{}{}
	for _, ca := range plural {
		if ca.Name == "" {
			continue
		}
		if _, dup := seen[ca.Name]; dup {
			continue
		}
		seen[ca.Name] = struct{}{}
		out = append(out, ca)
	}
	if legacy != nil && legacy.Name != "" {
		if _, dup := seen[legacy.Name]; !dup {
			out = append(out, neo4jv1beta1.TrustedCASecret{Name: legacy.Name, Key: legacy.Key})
		}
	}
	return out
}

// BuildTrustStoreInitContainer creates an init container that seeds a writable
// JKS truststore from the JDK's default cacerts (preserving trust in public
// CAs) and then imports each supplied PEM CA Secret with the Secret name as
// keytool alias.
//
// The resulting truststore lives at `/truststore/truststore.jks` (password
// `changeit`, the JVM default). JVM additional flags wired by the resource
// builder point Neo4j at this file.
func BuildTrustStoreInitContainer(image string, cas []neo4jv1beta1.TrustedCASecret) corev1.Container {
	if len(cas) == 0 {
		return corev1.Container{}
	}

	mounts := []corev1.VolumeMount{
		{Name: "truststore", MountPath: "/truststore"},
	}

	// Seed from JDK cacerts so public CAs (Let's Encrypt, etc.) keep working.
	// Then import each user-supplied CA with a unique alias.
	cmd := `set -euo pipefail
SRC="${JAVA_HOME}/lib/security/cacerts"
DST=/truststore/truststore.jks
cp "$SRC" "$DST"
chmod 0644 "$DST"
`
	for _, ca := range cas {
		key := ca.Key
		if key == "" {
			key = "ca.crt"
		}
		mountPath := trustedCASecretMountPath(ca.Name)
		mounts = append(mounts, corev1.VolumeMount{
			Name:      trustedCASecretVolumeName(ca.Name),
			MountPath: mountPath,
			ReadOnly:  true,
		})
		// Use the Secret name as the keytool alias. Names are unique by validation.
		cmd += fmt.Sprintf("keytool -import -trustcacerts -keystore \"$DST\" -storepass changeit -noprompt -alias %q -file %q\n",
			ca.Name, mountPath+"/"+key)
	}

	return corev1.Container{
		Name:         "truststore-init",
		Image:        image,
		Command:      []string{"/bin/bash", "-c", cmd},
		VolumeMounts: mounts,
	}
}

// BuildTrustStoreVolumes returns the volumes needed for JVM truststore support:
// one Secret-backed volume per trusted CA, plus the writable EmptyDir that
// holds the assembled truststore.
func BuildTrustStoreVolumes(cas []neo4jv1beta1.TrustedCASecret) []corev1.Volume {
	if len(cas) == 0 {
		return nil
	}
	vols := make([]corev1.Volume, 0, len(cas)+1)
	for _, ca := range cas {
		vols = append(vols, corev1.Volume{
			Name: trustedCASecretVolumeName(ca.Name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: ca.Name,
				},
			},
		})
	}
	vols = append(vols, corev1.Volume{
		Name: "truststore",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})
	return vols
}

// TrustStoreVolumeMount is the volume mount for the truststore in the main container
var TrustStoreVolumeMount = corev1.VolumeMount{
	Name:      "truststore",
	MountPath: "/truststore",
	ReadOnly:  true,
}
