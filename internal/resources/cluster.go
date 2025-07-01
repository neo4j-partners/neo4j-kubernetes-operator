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
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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

// BuildPrimaryStatefulSetForEnterprise creates a StatefulSet for Neo4j primary nodes
func BuildPrimaryStatefulSetForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *appsv1.StatefulSet {
	return buildStatefulSetForEnterprise(cluster, "primary", cluster.Spec.Topology.Primaries)
}

// BuildSecondaryStatefulSetForEnterprise creates a StatefulSet for Neo4j secondary nodes
func BuildSecondaryStatefulSetForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *appsv1.StatefulSet {
	return buildStatefulSetForEnterprise(cluster, "secondary", cluster.Spec.Topology.Secondaries)
}

// buildStatefulSetForEnterprise is a helper function to create StatefulSets for both primary and secondary nodes
func buildStatefulSetForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, role string, replicas int32) *appsv1.StatefulSet {
	adminSecret := DefaultAdminSecret

	// Configure rolling update strategy
	updateStrategy := appsv1.StatefulSetUpdateStrategy{
		Type: appsv1.RollingUpdateStatefulSetStrategyType,
		RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
			// Start with maxUnavailable = 0 to prevent concurrent updates
			// Secondaries can be updated more aggressively, but we use the same strategy for simplicity
			Partition: nil, // Will be set during rolling upgrade orchestration
		},
	}

	// Configure upgrade strategy based on cluster spec
	if cluster.Spec.UpgradeStrategy != nil {
		if cluster.Spec.UpgradeStrategy.Strategy == "Recreate" {
			updateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
		}
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", cluster.Name, role),
			Namespace: cluster.Namespace,
			Labels:    getLabelsForEnterprise(cluster, role),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:       &replicas,
			ServiceName:    fmt.Sprintf("%s-headless", cluster.Name),
			UpdateStrategy: updateStrategy,
			Selector: &metav1.LabelSelector{
				MatchLabels: getLabelsForEnterprise(cluster, role),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: getLabelsForEnterprise(cluster, role),
					Annotations: map[string]string{
						"prometheus.io/scrape": "true",
						"prometheus.io/port":   "2004",
						"prometheus.io/path":   "/metrics",
					},
				},
				Spec: BuildPodSpecForEnterprise(cluster, role, adminSecret),
			},
			VolumeClaimTemplates: buildVolumeClaimTemplatesForEnterprise(cluster),
		},
	}
}

// BuildHeadlessServiceForEnterprise creates a headless service for cluster discovery
func BuildHeadlessServiceForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-headless", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    getLabelsForEnterprise(cluster, ""),
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  getLabelsForEnterprise(cluster, ""),
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
					Name:       "cluster",
					Port:       ClusterPort,
					TargetPort: intstr.FromInt(ClusterPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "discovery",
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

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-client", cluster.Name),
			Namespace:   cluster.Namespace,
			Labels:      getLabelsForEnterprise(cluster, "client"),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: getLabelsForEnterprise(cluster, ""),
			Ports:    ports,
		},
	}
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
			"health.sh":  buildHealthScript(),
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
		fmt.Sprintf("%s-headless", cluster.Name),
		fmt.Sprintf("%s-headless.%s", cluster.Name, cluster.Namespace),
		fmt.Sprintf("%s-headless.%s.svc", cluster.Name, cluster.Namespace),
		fmt.Sprintf("%s-headless.%s.svc.cluster.local", cluster.Name, cluster.Namespace),
	}

	// Add individual StatefulSet pods
	for i := int32(0); i < cluster.Spec.Topology.Primaries; i++ {
		podName := fmt.Sprintf("%s-primary-%d", cluster.Name, i)
		dnsNames = append(dnsNames,
			podName,
			fmt.Sprintf("%s.%s-headless", podName, cluster.Name),
			fmt.Sprintf("%s.%s-headless.%s", podName, cluster.Name, cluster.Namespace),
			fmt.Sprintf("%s.%s-headless.%s.svc", podName, cluster.Name, cluster.Namespace),
			fmt.Sprintf("%s.%s-headless.%s.svc.cluster.local", podName, cluster.Name, cluster.Namespace),
		)
	}

	for i := int32(0); i < cluster.Spec.Topology.Secondaries; i++ {
		podName := fmt.Sprintf("%s-secondary-%d", cluster.Name, i)
		dnsNames = append(dnsNames,
			podName,
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

func getLabelsForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, role string) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       "neo4j",
		"app.kubernetes.io/instance":   cluster.Name,
		"app.kubernetes.io/version":    cluster.Spec.Image.Tag,
		"app.kubernetes.io/component":  "database",
		"app.kubernetes.io/part-of":    "neo4j-cluster",
		"app.kubernetes.io/managed-by": "neo4j-operator",
		"neo4j.com/cluster":            cluster.Name,
	}

	if role != "" {
		labels["neo4j.com/role"] = role
	}

	return labels
}

func BuildPodSpecForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, role, adminSecret string) corev1.PodSpec {
	// Environment variables
	env := []corev1.EnvVar{
		{
			Name:  "NEO4J_EDITION",
			Value: "enterprise",
		},
		{
			Name:  "NEO4J_CLUSTER_ROLE",
			Value: role,
		},
		{
			Name:  "NEO4J_CLUSTER_NAME",
			Value: cluster.Name,
		},
		{
			Name:  "NEO4J_NAMESPACE",
			Value: cluster.Namespace,
		},
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
		{
			Name:  "NEO4J_CLUSTER_PRIMARIES",
			Value: strconv.Itoa(int(cluster.Spec.Topology.Primaries)),
		},
		{
			Name:  "NEO4J_CLUSTER_SECONDARIES",
			Value: strconv.Itoa(int(cluster.Spec.Topology.Secondaries)),
		},
	}

	// Add custom environment variables
	if cluster.Spec.Env != nil {
		env = append(env, cluster.Spec.Env...)
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
				Name:          "cluster",
				ContainerPort: ClusterPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "discovery",
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
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/bash",
						"-c",
						"/conf/health.sh",
					},
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/bash",
						"-c",
						"/conf/health.sh",
					},
				},
			},
			InitialDelaySeconds: 60,
			PeriodSeconds:       30,
			TimeoutSeconds:      10,
			FailureThreshold:    5,
		},
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

	// Build pod spec
	podSpec := corev1.PodSpec{
		ServiceAccountName: getServiceAccountNameForEnterprise(cluster),
		SecurityContext: &corev1.PodSecurityContext{
			FSGroup: func() *int64 { gid := int64(7474); return &gid }(),
		},
		Containers: []corev1.Container{neo4jContainer},
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

	// --- Plugin Management: Add init containers for plugins ---
	var initContainers []corev1.Container
	for _, plugin := range cluster.Spec.Plugins {
		if plugin.Enabled && plugin.Source != nil && plugin.Source.URL != "" {
			initContainers = append(initContainers, corev1.Container{
				Name:    "install-plugin-" + plugin.Name,
				Image:   "alpine:3.18",
				Command: []string{"/bin/sh", "-c"},
				Args: []string{
					"apk add --no-cache curl && " +
						"echo Downloading plugin: " + plugin.Source.URL + " && " +
						"curl -L --fail --retry 3 -o /plugins/" + plugin.Name + ".jar '" + plugin.Source.URL + "'",
				},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "plugins",
					MountPath: "/plugins",
				}},
			})
		}
	}
	if len(initContainers) > 0 {
		podSpec.InitContainers = initContainers
	}

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
	config := `# Neo4j Enterprise Configuration

# Server settings
server.default_listen_address=0.0.0.0
server.bolt.listen_address=0.0.0.0:7687
server.http.listen_address=0.0.0.0:7474

# Neo4j 5.x Enterprise clustering
server.cluster.advertised_address=$(hostname -f):5000
server.cluster.listen_address=0.0.0.0:5000
server.discovery.advertised_address=$(hostname -f):6000
server.discovery.listen_address=0.0.0.0:6000
server.routing.advertised_address=$(hostname -f):7688
server.routing.listen_address=0.0.0.0:7688

# Cluster discovery configuration for Kubernetes
dbms.cluster.discovery.resolver_type=K8S
dbms.kubernetes.label_selector=app.kubernetes.io/name=` + cluster.Name + `,app.kubernetes.io/instance=` + cluster.Name + `
dbms.kubernetes.discovery.service_port_name=cluster

# Cluster membership (minimum 3 primaries for quorum)
dbms.cluster.minimum_core_cluster_size_at_formation=3
dbms.cluster.minimum_core_cluster_size_at_runtime=3

# Paths
server.directories.data=/data
server.directories.logs=/logs

# Memory settings
server.memory.heap.initial_size=1G
server.memory.heap.max_size=1G
server.memory.pagecache.size=512M

# Security
server.unmanaged_extension_classes=n10s.endpoint=/rdf

# Logging
server.logs.user.rotationKeepNumber=5
server.logs.user.rotationSize=20M
`

	// Add TLS configuration if enabled
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == CertManagerMode {
		config += `
# TLS Configuration
server.https.enabled=true
server.https.listen_address=0.0.0.0:7473
server.bolt.tls_level=OPTIONAL
server.https.advertised_address=$(hostname -f):7473

# SSL certificates
server.directories.certificates=/ssl
server.ssl.policy.bolt.enabled=true
server.ssl.policy.bolt.base_directory=/ssl
server.ssl.policy.bolt.private_key=tls.key
server.ssl.policy.bolt.public_certificate=tls.crt
server.ssl.policy.https.enabled=true
server.ssl.policy.https.base_directory=/ssl
server.ssl.policy.https.private_key=tls.key
server.ssl.policy.https.public_certificate=tls.crt
`
	}

	// Add custom configuration
	if cluster.Spec.Config != nil {
		for key, value := range cluster.Spec.Config {
			config += fmt.Sprintf("%s=%s\n", key, value)
		}
	}

	return config
}

func buildStartupScriptForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	return `#!/bin/bash
set -e

echo "Starting Neo4j Enterprise..."

# Wait for DNS resolution of headless service
echo "Waiting for DNS resolution..."
until nslookup ` + cluster.Name + `-headless.` + cluster.Namespace + `.svc.cluster.local; do
    echo "Waiting for ` + cluster.Name + `-headless.` + cluster.Namespace + `.svc.cluster.local to be resolvable..."
    sleep 2
done

# Set server ID based on pod ordinal for Neo4j 5.x clustering
export NEO4J_dbms__cluster__server__id="${HOSTNAME##*-}"

# Start Neo4j
exec /docker-entrypoint.sh neo4j
`
}

func buildHealthScript() string {
	return `#!/bin/bash
# Health check script for Neo4j

# Check if Neo4j is responding
if ! curl -f -s -X GET http://localhost:7474/db/neo4j/tx/commit \
    -H "Content-Type: application/json" \
    -d '{"statements":[{"statement":"CALL dbms.cluster.overview() YIELD role RETURN role"}]}' > /dev/null 2>&1; then
    echo "Neo4j is not responding"
    exit 1
fi

echo "Neo4j is healthy"
exit 0
`
}
