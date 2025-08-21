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
	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

const (
	// BoltPort is the default port for Neo4j Bolt protocol
	BoltPort = 7687
	// HTTPPort is the default port for Neo4j HTTP API
	HTTPPort = 7474
	// HTTPSPort is the default port for Neo4j HTTPS API
	HTTPSPort = 7473
	// ClusterPort is the default port for Neo4j cluster communication
	ClusterPort = 5000
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

	// Neo4jContainer is the name of the main Neo4j container
	Neo4jContainer = "neo4j"
	// SidecarContainer is the name of the sidecar container
	SidecarContainer = "prometheus-exporter"
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
)

// BuildServerStatefulSetForEnterprise creates a single StatefulSet for all Neo4j servers
// This StatefulSet has multiple replicas (one per server) that self-organize into roles
// Replaces the previous individual StatefulSet per server approach for better management
func BuildServerStatefulSetForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *appsv1.StatefulSet {
	// Create single StatefulSet with replicas equal to number of servers
	sts := buildStatefulSetForEnterprise(cluster, "server", cluster.Spec.Topology.Servers)
	return sts
}

// BuildServerStatefulSetsForEnterprise creates individual StatefulSets for each Neo4j server
// DEPRECATED: Use BuildServerStatefulSetForEnterprise for unified StatefulSet approach
// Each server has its own StatefulSet with a replica count of 1
// First server uses bootstrapping_strategy=me, others use bootstrapping_strategy=other
func BuildServerStatefulSetsForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) []*appsv1.StatefulSet {
	var statefulSets []*appsv1.StatefulSet

	for i := int32(0); i < cluster.Spec.Topology.Servers; i++ {
		// Create individual StatefulSet for each server
		sts := buildStatefulSetForEnterprise(cluster, fmt.Sprintf("server-%d", i), 1)
		statefulSets = append(statefulSets, sts)
	}

	return statefulSets
}

// BuildBackupStatefulSet creates a single, centralized backup StatefulSet for the cluster
// This is more efficient than having backup sidecars in each server pod
func BuildBackupStatefulSet(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *appsv1.StatefulSet {
	// Only create backup StatefulSet if backups are configured
	if cluster.Spec.Backups == nil {
		return nil
	}

	return buildCentralizedBackupStatefulSet(cluster)
}

// buildStatefulSetForEnterprise is a helper function to create StatefulSet for individual Neo4j server
func buildStatefulSetForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, serverName string, replicas int32) *appsv1.StatefulSet {
	adminSecret := DefaultAdminSecret

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
					Labels: getLabelsForEnterpriseServer(cluster, serverName),
					Annotations: map[string]string{
						"prometheus.io/scrape": "true",
						"prometheus.io/port":   "2004",
						"prometheus.io/path":   "/metrics",
					},
				},
				Spec: BuildPodSpecForEnterprise(cluster, serverName, adminSecret),
			},
			VolumeClaimTemplates: buildVolumeClaimTemplatesForEnterprise(cluster),
		},
	}
}

// BuildHeadlessServiceForEnterprise creates a headless service for StatefulSet pod identity
func BuildHeadlessServiceForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.Service {
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
				{
					Name:       "tcp-discovery",
					Port:       ClusterPort,
					TargetPort: intstr.FromInt(ClusterPort),
					Protocol:   corev1.ProtocolTCP,
				},
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
func BuildDiscoveryServiceForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.Service {
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
					Port:       ClusterPort,
					TargetPort: intstr.FromInt(ClusterPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildInternalsServiceForEnterprise creates an internals service for cluster discovery
// This is NOT a headless service as per Neo4j Helm charts best practice
// "headless services have been seen to introduce latency whenever a cluster member restarts"
func BuildInternalsServiceForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.Service {
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
				{
					Name:       "tcp-discovery",
					Port:       ClusterPort,
					TargetPort: intstr.FromInt(ClusterPort),
					Protocol:   corev1.ProtocolTCP,
				},
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
func BuildClientServiceForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.Service {
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

	annotations := make(map[string]string)
	if cluster.Spec.Service != nil && cluster.Spec.Service.Annotations != nil {
		annotations = cluster.Spec.Service.Annotations
	}

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
			svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyType(cluster.Spec.Service.ExternalTrafficPolicy)
		}
	}

	return svc
}

// BuildConfigMapForEnterprise creates a ConfigMap with Neo4j configuration
func BuildConfigMapForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.ConfigMap {
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
func BuildCertificateForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *certv1.Certificate {
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

	// Build certificate spec
	certSpec := certv1.CertificateSpec{
		SecretName: fmt.Sprintf("%s-tls-secret", cluster.Name),
		IssuerRef: cmmeta.ObjectReference{
			Name: cluster.Spec.TLS.IssuerRef.Name,
			Kind: cluster.Spec.TLS.IssuerRef.Kind,
		},
		CommonName: fmt.Sprintf("%s-client.%s.svc.cluster.local", cluster.Name, cluster.Namespace),
		DNSNames:   dnsNames,
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

	// Set certificate usages if specified
	if len(cluster.Spec.TLS.Usages) > 0 {
		certSpec.Usages = make([]certv1.KeyUsage, len(cluster.Spec.TLS.Usages))
		for i, usage := range cluster.Spec.TLS.Usages {
			certSpec.Usages[i] = certv1.KeyUsage(usage)
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
func BuildExternalSecretForTLS(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) map[string]interface{} {
	if cluster.Spec.TLS == nil || cluster.Spec.TLS.ExternalSecrets == nil || !cluster.Spec.TLS.ExternalSecrets.Enabled {
		return nil
	}
	return buildExternalSecret(cluster, cluster.Spec.TLS.ExternalSecrets, "tls")
}

// BuildExternalSecretForAuth creates an ExternalSecret for authentication secrets
func BuildExternalSecretForAuth(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) map[string]interface{} {
	if cluster.Spec.Auth == nil || cluster.Spec.Auth.ExternalSecrets == nil || !cluster.Spec.Auth.ExternalSecrets.Enabled {
		return nil
	}
	return buildExternalSecret(cluster, cluster.Spec.Auth.ExternalSecrets, "auth")
}

// buildExternalSecret is a helper function to create ExternalSecrets for both TLS and Auth
func buildExternalSecret(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, esConfig *neo4jv1alpha1.ExternalSecretsConfig, secretType string) map[string]interface{} {
	// Build data array
	var data []map[string]interface{}
	for _, item := range esConfig.Data {
		secretData := map[string]interface{}{
			"secretKey": item.SecretKey,
		}

		if item.RemoteRef != nil {
			remoteRef := map[string]interface{}{
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

	return map[string]interface{}{
		"apiVersion": "external-secrets.io/v1beta1",
		"kind":       "ExternalSecret",
		"metadata": map[string]interface{}{
			"name":      fmt.Sprintf("%s-%s-external-secret", cluster.Name, secretType),
			"namespace": cluster.Namespace,
			"labels":    getLabelsForEnterprise(cluster, "external-secret"),
		},
		"spec": map[string]interface{}{
			"secretStoreRef": map[string]interface{}{
				"name": esConfig.SecretStoreRef.Name,
				"kind": esConfig.SecretStoreRef.Kind,
			},
			"target": map[string]interface{}{
				"name":           fmt.Sprintf("%s-%s-secret", cluster.Name, secretType),
				"creationPolicy": "Owner",
			},
			"refreshInterval": refreshInterval,
			"data":            data,
		},
	}
}

// BuildDiscoveryServiceAccountForEnterprise creates a ServiceAccount for Kubernetes discovery
func BuildDiscoveryServiceAccountForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getDiscoveryServiceAccountNameForEnterprise(cluster),
			Namespace: cluster.Namespace,
			Labels:    getLabelsForEnterprise(cluster, "discovery-service-account"),
		},
	}
}

// BuildDiscoveryRoleForEnterprise creates a Role for Kubernetes discovery
func BuildDiscoveryRoleForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *rbacv1.Role {
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
func BuildDiscoveryRoleBindingForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *rbacv1.RoleBinding {
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

// BuildServiceAccountForEnterprise creates a ServiceAccount for cloud identity
func BuildServiceAccountForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.ServiceAccount {
	if cluster.Spec.Backups == nil || cluster.Spec.Backups.Cloud == nil ||
		cluster.Spec.Backups.Cloud.Identity.AutoCreate == nil ||
		!cluster.Spec.Backups.Cloud.Identity.AutoCreate.Enabled {
		return nil
	}

	annotations := make(map[string]string)
	// Add cloud-specific annotations based on provider
	switch cluster.Spec.Backups.Cloud.Provider {
	case "gcp":
		annotations["iam.gke.io/gcp-service-account"] = fmt.Sprintf("%s-backup@PROJECT.iam.gserviceaccount.com", cluster.Name)
	case "aws":
		annotations["eks.amazonaws.com/role-arn"] = fmt.Sprintf("arn:aws:iam::ACCOUNT:role/%s-backup-role", cluster.Name)
	case "azure":
		annotations["azure.workload.identity/client-id"] = fmt.Sprintf("%s-backup-identity", cluster.Name)
	}

	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:        getServiceAccountNameForEnterprise(cluster),
			Namespace:   cluster.Namespace,
			Labels:      getLabelsForEnterprise(cluster, "service-account"),
			Annotations: annotations,
		},
	}
}

// BuildIngressForEnterprise creates an Ingress for external access
func BuildIngressForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *networkingv1.Ingress {
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

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-ingress", cluster.Name),
			Namespace:   cluster.Namespace,
			Labels:      getLabelsForEnterprise(cluster, "ingress"),
			Annotations: ingressSpec.Annotations,
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
func getLabelsForEnterpriseServer(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, serverName string) map[string]string {
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

func getLabelsForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, role string) map[string]string {
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

// buildBackupSidecarStatefulSet creates a separate StatefulSet for backup sidecar
// buildCentralizedBackupStatefulSet creates a single backup StatefulSet for the entire cluster
func buildCentralizedBackupStatefulSet(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *appsv1.StatefulSet {
	adminSecret := DefaultAdminSecret
	if cluster.Spec.Auth != nil && cluster.Spec.Auth.AdminSecret != "" {
		adminSecret = cluster.Spec.Auth.AdminSecret
	}

	labels := getLabelsForEnterprise(cluster, "backup")
	labels["neo4j.com/component"] = "backup"

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-backup", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:            &[]int32{1}[0],
			ServiceName:         fmt.Sprintf("%s-backup-headless", cluster.Name),
			PodManagementPolicy: appsv1.OrderedReadyPodManagement,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"neo4j.com/cluster":   cluster.Name,
					"neo4j.com/component": "backup",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"neo4j.com/cluster":      cluster.Name,
						"neo4j.com/component":    "backup",
						"neo4j.com/instance":     cluster.Name,
						"app.kubernetes.io/name": "neo4j",
					},
				},
				Spec: buildCentralizedBackupPodSpec(cluster, adminSecret),
			},
			VolumeClaimTemplates: buildBackupVolumeClaimTemplates(cluster),
		},
	}
}

// buildCentralizedBackupPodSpec creates the pod spec for centralized backup
func buildCentralizedBackupPodSpec(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, adminSecret string) corev1.PodSpec {
	// Environment variables for centralized backup
	env := []corev1.EnvVar{
		{
			Name:  "NEO4J_CLUSTER_NAME",
			Value: cluster.Name,
		},
		{
			Name:  "NEO4J_BOLT_URI",
			Value: fmt.Sprintf("bolt://%s-client:7687", cluster.Name),
		},
		{
			Name: "NEO4J_USERNAME",
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
			Name: "NEO4J_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: adminSecret,
					},
					Key: "password",
				},
			},
		},
		{
			Name:  "NEO4J_EDITION",
			Value: "enterprise",
		},
		{
			Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
			Value: "yes",
		},
	}

	// Build resources for centralized backup - single instance for whole cluster
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse("100m"),  // Lower CPU - only one instance
			"memory": resource.MustParse("256Mi"), // Lower memory - optimized
		},
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse("500m"),
			"memory": resource.MustParse("1Gi"),
		},
	}

	// Backup script with advanced functionality
	backupScript := `#!/bin/bash
set -e

echo "Centralized backup service started for cluster $NEO4J_CLUSTER_NAME"
echo "Connecting to cluster via $NEO4J_BOLT_URI"

# Wait for cluster to be ready
echo "Waiting for Neo4j cluster to be available..."
while ! cypher-shell -a $NEO4J_BOLT_URI -u $NEO4J_USERNAME -p $NEO4J_PASSWORD "SHOW SERVERS" 2>/dev/null; do
    echo "Cluster not ready, waiting..."
    sleep 10
done

echo "Neo4j cluster is ready, starting backup monitoring"

# Function to perform backup
perform_backup() {
    local backup_type=${1:-FULL}
    local backup_name="backup-$(date +%Y%m%d_%H%M%S)"
    local backup_path="/backups/$backup_name"

    echo "Starting $backup_type backup to $backup_path"

    # Create backup directory
    mkdir -p "$backup_path"

    # Perform backup using neo4j-admin
    neo4j-admin database backup \
        --to-path="$backup_path" \
        --type="$backup_type" \
        --include-metadata=all \
        --verbose

    echo "Backup completed: $backup_path"

    # Clean up old backups (keep last 10)
    cd /backups
    ls -t | tail -n +11 | xargs -r rm -rf
}

# Monitor for backup requests and scheduled backups
while true; do
    # Check for manual backup requests via file system
    if [ -f /backup-requests/backup.request ]; then
        echo "Processing backup request"
        backup_type=$(cat /backup-requests/backup.request | jq -r '.type // "FULL"')
        perform_backup "$backup_type"
        rm -f /backup-requests/backup.request
        echo "COMPLETED" > /backup-requests/backup.status
    fi

    # Scheduled backup check (daily at 2 AM if current time matches)
    current_hour=$(date +%H)
    current_minute=$(date +%M)
    if [ "$current_hour" = "02" ] && [ "$current_minute" = "00" ]; then
        echo "Performing scheduled daily backup"
        perform_backup "FULL"
    fi

    sleep 60
done`

	return corev1.PodSpec{
		ServiceAccountName: getServiceAccountNameForEnterprise(cluster),
		SecurityContext: &corev1.PodSecurityContext{
			FSGroup: &[]int64{7474}[0], // Neo4j user group
		},
		Containers: []corev1.Container{
			{
				Name:            "backup",
				Image:           fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag),
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env:             env,
				Resources:       resources,
				Command:         []string{"/bin/bash", "-c"},
				Args:            []string{backupScript},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "backup-storage",
						MountPath: "/backups",
					},
					{
						Name:      "backup-requests",
						MountPath: "/backup-requests",
					},
				},
			},
		},
		Volumes: []corev1.Volume{
			{
				Name: "backup-requests",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		},
	}
}

// buildBackupVolumeClaimTemplates creates PVC templates for backup storage
func buildBackupVolumeClaimTemplates(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) []corev1.PersistentVolumeClaim {
	if cluster.Spec.Backups == nil {
		return nil
	}

	// Use backup-specific storage or fall back to cluster storage
	storageClassName := cluster.Spec.Storage.ClassName
	storageSize := cluster.Spec.Storage.Size

	// Check for backup-specific storage configuration in cluster storage spec
	if cluster.Spec.Storage.BackupStorage != nil {
		if cluster.Spec.Storage.BackupStorage.ClassName != "" {
			storageClassName = cluster.Spec.Storage.BackupStorage.ClassName
		}
		if cluster.Spec.Storage.BackupStorage.Size != "" {
			storageSize = cluster.Spec.Storage.BackupStorage.Size
		}
	}

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "backup-storage",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}

	if storageClassName != "" {
		pvc.Spec.StorageClassName = &storageClassName
	}

	return []corev1.PersistentVolumeClaim{pvc}
}

func BuildPodSpecForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, serverName, adminSecret string) corev1.PodSpec {
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

	// Build container
	neo4jContainer := corev1.Container{
		Name:            Neo4jContainer,
		Image:           fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag),
		ImagePullPolicy: corev1.PullPolicy(cluster.Spec.Image.PullPolicy),
		Env:             env,
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
			{
				Name:          "tcp-discovery",
				ContainerPort: ClusterPort,
				Protocol:      corev1.ProtocolTCP,
			},
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

	// Add TLS volume
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == CertManagerMode {
		volumes = append(volumes, corev1.Volume{
			Name: CertsVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: fmt.Sprintf("%s-tls-secret", cluster.Name),
				},
			},
		})
	}

	// Build pod spec - backup is now handled by centralized StatefulSet, not sidecars
	podSpec := corev1.PodSpec{
		ServiceAccountName: getDiscoveryServiceAccountNameForEnterprise(cluster),
		SecurityContext: &corev1.PodSecurityContext{
			FSGroup: func() *int64 { gid := int64(7474); return &gid }(),
		},
		Containers: []corev1.Container{neo4jContainer}, // Only Neo4j container, no backup sidecar
		Volumes:    volumes,
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

	// --- Plugin Management ---
	// NOTE: Plugins are now managed through the Neo4jPlugin CRD instead of embedded configuration.
	// The Neo4jPlugin controller handles plugin installation and management separately.

	// --- Query Monitoring: Add Prometheus exporter sidecar ---
	if cluster.Spec.QueryMonitoring != nil && cluster.Spec.QueryMonitoring.Enabled {
		exporterContainer := corev1.Container{
			Name:  "prometheus-exporter",
			Image: "neo4j/prometheus-exporter:4.0.0",
			Args:  []string{"--neo4j.uri=bolt://localhost:7687", "--neo4j.user=neo4j", "--neo4j.password=$(NEO4J_AUTH)"},
			Env: []corev1.EnvVar{
				{
					Name: "NEO4J_AUTH",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: adminSecret,
							},
							Key: "password",
						},
					},
				},
			},
			Ports: []corev1.ContainerPort{{
				Name:          "metrics",
				ContainerPort: 2004,
				Protocol:      corev1.ProtocolTCP,
			}},
		}
		podSpec.Containers = append(podSpec.Containers, exporterContainer)
	}

	return podSpec
}

func buildVolumeClaimTemplatesForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) []corev1.PersistentVolumeClaim {
	return []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   DataVolume,
				Labels: getLabelsForEnterprise(cluster, ""),
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				StorageClassName: &cluster.Spec.Storage.ClassName,
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(cluster.Spec.Storage.Size),
					},
				},
			},
		},
	}
}

func getServiceAccountNameForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	if cluster.Spec.Backups != nil &&
		cluster.Spec.Backups.Cloud != nil &&
		cluster.Spec.Backups.Cloud.Identity.ServiceAccount != "" {
		return cluster.Spec.Backups.Cloud.Identity.ServiceAccount
	}

	if cluster.Spec.Backups != nil &&
		cluster.Spec.Backups.Cloud != nil &&
		cluster.Spec.Backups.Cloud.Identity.AutoCreate != nil &&
		cluster.Spec.Backups.Cloud.Identity.AutoCreate.Enabled {
		return fmt.Sprintf("%s-cloud-identity", cluster.Name)
	}

	return "default"
}

func buildNeo4jConfigForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
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
server.cluster.listen_address=0.0.0.0:5000
# server.discovery.listen_address=0.0.0.0:6000 - ignored in V2_ONLY mode
server.routing.listen_address=0.0.0.0:7688
server.cluster.raft.listen_address=0.0.0.0:7000

# Note: Single RAFT and cluster discovery settings are dynamically added by startup script
`, memoryConfig.HeapInitialSize, memoryConfig.HeapMaxSize, memoryConfig.PageCacheSize)

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
# CRITICAL: trust_all=true is required for reliable TLS cluster formation
# This allows nodes to trust each other's certificates during initial handshake
dbms.ssl.policy.cluster.enabled=true
dbms.ssl.policy.cluster.base_directory=/ssl
dbms.ssl.policy.cluster.private_key=tls.key
dbms.ssl.policy.cluster.public_certificate=tls.crt
dbms.ssl.policy.cluster.trust_all=true
dbms.ssl.policy.cluster.client_auth=NONE
dbms.ssl.policy.cluster.tls_versions=TLSv1.3,TLSv1.2

# Enable TLS for connectors
server.bolt.tls_level=OPTIONAL
`
	}

	// Add custom configuration (excluding memory settings already added above)
	if cluster.Spec.Config != nil {
		// Memory settings that are already set by memoryConfig
		excludeKeys := map[string]bool{
			"server.memory.heap.initial_size": true,
			"server.memory.heap.max_size":     true,
			"server.memory.pagecache.size":    true,
		}

		// Sort keys to ensure deterministic order and prevent hash oscillation
		var keys []string
		for key := range cluster.Spec.Config {
			if !excludeKeys[key] {
				keys = append(keys, key)
			}
		}
		// Sort keys alphabetically for consistent ordering
		sort.Strings(keys)

		// Add configuration in sorted order
		for _, key := range keys {
			config += fmt.Sprintf("%s=%s\n", key, cluster.Spec.Config[key])
		}
	}

	return config
}

// isNeo4jVersion526OrHigher checks if the Neo4j version is 5.26 or higher
func isNeo4jVersion526OrHigher(imageTag string) bool {
	// Support for various 5.26+ versions
	supportedVersions := []string{"5.26", "5.27", "5.28", "5.29", "5.30", "5.31", "5.32", "5.33", "5.34", "5.35"}

	for _, version := range supportedVersions {
		if strings.Contains(imageTag, version) {
			return true
		}
	}

	return false
}

// getKubernetesDiscoveryParameter returns the correct Kubernetes discovery parameter based on Neo4j version
//
// CRITICAL: This function implements the fix for Neo4j V2_ONLY discovery configuration.
// V2_ONLY mode disables the discovery port (6000) and only uses the cluster port (5000).
// Therefore, we must use 'tcp-discovery' port name, not 'tcp-tx'.
//
// Version-specific behavior:
// - Neo4j 5.26.x: Uses dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery + V2_ONLY
// - Neo4j 2025.x: Uses dbms.kubernetes.discovery.service_port_name=tcp-discovery (V2_ONLY is default)
// - Both versions: Must use tcp-discovery port (5000) for cluster formation
func getKubernetesDiscoveryParameter(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	// Extract version from image tag
	imageTag := cluster.Spec.Image.Tag

	// For Neo4j 5.x (semver): Always use V2_ONLY discovery for 5.26+
	// For Neo4j 2025.x+ (calver): use dbms.kubernetes.discovery.service_port_name
	if strings.HasPrefix(imageTag, "5.") {
		// For Neo4j 5.26+, always use V2_ONLY discovery
		// CRITICAL: Must use tcp-discovery (port 5000) not tcp-tx (port 6000)
		// because V2_ONLY mode disables the discovery port (6000)
		if isNeo4jVersion526OrHigher(imageTag) {
			return `dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery
dbms.cluster.discovery.version=V2_ONLY
dbms.kubernetes.cluster_domain=cluster.local`
		}
		// For other 5.x versions (pre-5.26) - not supported by this operator
		return `dbms.kubernetes.service_port_name=discovery
dbms.cluster.discovery.version=V2_ONLY
dbms.kubernetes.cluster_domain=cluster.local`
	} else if strings.HasPrefix(imageTag, "2025.") || strings.Contains(imageTag, "2025") {
		// For Neo4j 2025.x+ (calver), use the new parameter name
		// V2_ONLY is default in 2025.x, so don't set it explicitly
		// CRITICAL: Must use tcp-discovery (port 5000) - same as 5.26.x
		return `dbms.kubernetes.discovery.service_port_name=tcp-discovery
dbms.kubernetes.cluster_domain=cluster.local`
	}

	// Default to 5.26+ configuration for maximum compatibility
	return `dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery
dbms.cluster.discovery.version=V2_ONLY
dbms.kubernetes.cluster_domain=cluster.local`
}

func buildStartupScriptForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	// Get discovery parameters for Neo4j version
	kubernetesDiscoveryParam := getKubernetesDiscoveryParameter(cluster)

	// Unified startup script for all deployments
	return `#!/bin/bash
set -e

echo "Starting Neo4j Enterprise in cluster mode..."

# Set proper NEO4J_AUTH format (username/password)
export NEO4J_AUTH="${DB_USERNAME}/${DB_PASSWORD}"

# Extract server index from NEO4J_SERVER_NAME (format: server-N)
SERVER_INDEX="${NEO4J_SERVER_NAME##*-}"

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
cat >> /tmp/neo4j-config/neo4j.conf << EOF

# Neo4j 5.x advertised addresses with FQDN
server.default_advertised_address=${HOSTNAME_FQDN}
server.cluster.advertised_address=${HOSTNAME_FQDN}:5000
# server.discovery.advertised_address=${HOSTNAME_FQDN}:6000 - ignored in V2_ONLY mode
server.routing.advertised_address=${HOSTNAME_FQDN}:7688
server.cluster.raft.advertised_address=${HOSTNAME_FQDN}:7000
EOF

# Cluster configuration based on topology
TOTAL_SERVERS=` + fmt.Sprintf("%d", cluster.Spec.Topology.Servers) + `

echo "Cluster topology: ${TOTAL_SERVERS} servers"
echo "Server index: ${SERVER_INDEX}"

# Neo4jEnterpriseCluster uses server-based clustering
# Minimum: 2 servers (servers self-organize for database hosting)
echo "Multi-server cluster: using Kubernetes discovery"

# Use Kubernetes service discovery with label selectors
echo "Configuring Kubernetes service discovery with label selectors"

# Wait for discovery service to be ready before starting Neo4j
echo "Waiting for discovery service to be ready..."
DISCOVERY_SERVICE="` + cluster.Name + `-discovery.` + cluster.Namespace + `.svc.cluster.local"
MAX_WAIT=60
WAIT_SECONDS=0
while [ $WAIT_SECONDS -lt $MAX_WAIT ]; do
    if getent hosts $DISCOVERY_SERVICE >/dev/null 2>&1; then
        echo "Discovery service $DISCOVERY_SERVICE is ready"
        break
    fi
    echo "Discovery service not ready yet, waiting... (${WAIT_SECONDS}s/${MAX_WAIT}s)"
    sleep 2
    WAIT_SECONDS=$((WAIT_SECONDS + 2))
done

if [ $WAIT_SECONDS -ge $MAX_WAIT ]; then
    echo "Warning: Discovery service not ready after ${MAX_WAIT}s, proceeding anyway"
fi

# Wait for service to be reachable
echo "Checking discovery service readiness..."
ENDPOINT_CHECK_COUNT=0
MAX_ENDPOINT_CHECKS=10
while [ $ENDPOINT_CHECK_COUNT -lt $MAX_ENDPOINT_CHECKS ]; do
    # Check if the service DNS resolves (indicates service exists)
    if getent hosts $DISCOVERY_SERVICE >/dev/null 2>&1; then
        echo "Discovery service is reachable, proceeding"
        break
    fi
    echo "Discovery service not reachable, waiting... (${ENDPOINT_CHECK_COUNT}/${MAX_ENDPOINT_CHECKS})"
    sleep 3
    ENDPOINT_CHECK_COUNT=$((ENDPOINT_CHECK_COUNT + 1))
done

# Use parallel bootstrap discovery with coordination for cluster formation
echo "Starting parallel cluster formation - all servers coordinate bootstrap"

# Set minimum servers for proper cluster formation
# With Parallel pod management, all server pods start simultaneously (with small stagger)
# All servers coordinate to establish primary/secondary roles automatically
# This works reliably even with TLS enabled due to trust_all=true in cluster SSL policy
# Servers self-organize, use fixed minimum of 1 for bootstrap

# Configure bootstrapping strategy based on server index
if [ "$SERVER_INDEX" = "0" ]; then
    echo "Server 0: Using bootstrapping strategy 'me' (first server)"
    BOOTSTRAP_STRATEGY="me"
else
    echo "Server $SERVER_INDEX: Using bootstrapping strategy 'other' (joining existing cluster)"
    BOOTSTRAP_STRATEGY="other"
fi

# Configure cluster discovery and bootstrapping
cat >> /tmp/neo4j-config/neo4j.conf << EOF

# Multi-node cluster using Kubernetes service discovery (Neo4j 5.26+ standard pattern)
dbms.cluster.discovery.resolver_type=K8S
dbms.kubernetes.label_selector=neo4j.com/cluster=` + cluster.Name + `,neo4j.com/clustering=true
` + kubernetesDiscoveryParam + `

# Bootstrapping strategy for this server
internal.dbms.cluster.discovery.system_bootstrapping_strategy=$BOOTSTRAP_STRATEGY

# Cluster formation - minimum servers always 1 for bootstrap
# Servers self-organize into primary/secondary roles, don't pre-assign roles
dbms.cluster.minimum_initial_system_primaries_count=1
initial.dbms.automatically_enable_free_servers=true

# Cluster formation optimization
dbms.cluster.raft.binding_timeout=1d
dbms.cluster.raft.membership.join_timeout=10m
dbms.routing.default_router=SERVER

# Discovery resolution timeout to handle service propagation delays
internal.dbms.cluster.discovery.resolution_timeout=1d
EOF

# Add server mode constraint if specified
` + buildServerModeConstraintConfig(cluster) + `

# Set NEO4J config directory
export NEO4J_CONF=/tmp/neo4j-config

# Start Neo4j
exec /startup/docker-entrypoint.sh neo4j
`
}

// buildServerModeConstraintConfig generates server mode constraint configuration
func buildServerModeConstraintConfig(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
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

// ValidateServerRoleHints validates server role hints configuration
func ValidateServerRoleHints(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) []string {
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

func buildHealthScript(_ *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	// Enhanced health check for cluster deployments
	return `#!/bin/bash
# Health check script for Neo4j clustering

# Check if Neo4j process is running
if ! pgrep -f "neo4j.*EnterpriseEntryPoint" > /dev/null; then
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
func buildReadinessProbe(_ *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.Probe {
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
func buildLivenessProbe(_ *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.Probe {
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
func buildJVMSettings(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
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

	// Optional: Enable GC logging for debugging (commented out by default)
	// jvmFlags = append(jvmFlags, "-Xlog:gc*:file=/logs/gc.log:time,uptime,level,tags:filecount=5,filesize=10m")

	return strings.Join(jvmFlags, " ")
}

// buildStartupProbe creates a startup probe for initial cluster formation
func buildStartupProbe(_ *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.Probe {
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
func getDiscoveryServiceAccountNameForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	return fmt.Sprintf("%s-discovery", cluster.Name)
}

func getDiscoveryRoleNameForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	return fmt.Sprintf("%s-discovery", cluster.Name)
}

func getDiscoveryRoleBindingNameForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	return fmt.Sprintf("%s-discovery", cluster.Name)
}
