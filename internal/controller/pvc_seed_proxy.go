/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// pvcSeedProxyPort is the in-cluster HTTP port served by the busybox
	// httpd proxy that fronts the backup PVC. Constant so the URL builder
	// and the Deployment definition agree.
	pvcSeedProxyPort = 8080

	// pvcSeedProxyContainerName matches the busybox image's container
	// name in the proxy Deployment template.
	pvcSeedProxyContainerName = "httpd"
)

// pvcSeedProxyName returns the canonical resource name for the HTTP-proxy
// Deployment + Service used to expose a backup PVC to Neo4j cluster pods
// during a restore. One proxy per owning CR keeps lifecycle management
// simple (owner-reference GC when the CR is deleted).
//
// `ownerName` is the suffix — typically the sharded DB name (for sharded
// seedBackupRef restores) or the Neo4jRestore name (for standard-DB
// cluster PVC restores). The 63-char DNS label limit applies; callers
// must ensure ownerName + "backup-seed-proxy-" stays under it.
func pvcSeedProxyName(ownerName string) string {
	return "backup-seed-proxy-" + ownerName
}

// pvcSeedProxyURL builds the HTTP URL that Neo4j's URLConnectionSeedProvider
// will fetch for a `.backup` artifact. The proxy mounts the backup PVC at
// /backup, so the URL path is `/<backupsPath>/<filename>` — the per-CR
// directory under /backup followed by the captured artifact filename.
//
// Examples:
//
//	http://backup-seed-proxy-products.ns.svc.cluster.local:8080/products-backup/products-g000-T21-04-42.backup
//	http://backup-seed-proxy-inventory-restore.ns.svc.cluster.local:8080/inventory-backup/inventory-2026-06-08T01-18-06.backup
func pvcSeedProxyURL(ownerName, namespace, backupsPath, filename string) string {
	return fmt.Sprintf(
		"http://%s.%s.svc.cluster.local:%d/%s/%s",
		pvcSeedProxyName(ownerName),
		namespace,
		pvcSeedProxyPort,
		backupsPath,
		filename,
	)
}

// ensurePVCSeedProxyResources creates (idempotently) the Deployment + Service
// that expose `backupPVCName` over HTTP so Neo4j cluster pods can fetch
// `.backup` files via URLConnectionSeedProvider.
//
// One proxy per `owner` CR with `owner` set as the controller owner reference
// — Kubernetes GCs the proxy when the owner is deleted. Idempotent: when the
// Deployment + Service already exist, reconciles read-only (no spec drift fix
// to avoid restarting pods).
//
// Returns (proxyAvailable bool, err error). proxyAvailable=true means the
// Deployment reports at least one Ready replica; caller can build URLs and
// pass them to Neo4j. False + nil err means the proxy is still rolling out
// — caller should requeue. err != nil for permanent failures (missing PVC
// name, namespace mismatch).
//
// `ownerName` overrides the proxy resource-name suffix when needed (e.g.
// the Neo4jRestore controller wants the proxy named after the restore CR,
// not the cluster). Defaults to `owner.GetName()` when empty.
func ensurePVCSeedProxyResources(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	owner client.Object,
	ownerName string,
	backupPVCName string,
) (proxyAvailable bool, err error) {
	if backupPVCName == "" {
		return false, fmt.Errorf("PVC seed proxy requires a backup PVC name; got empty")
	}
	if ownerName == "" {
		ownerName = owner.GetName()
	}
	// DNS-1035 label limit — a too-long owner name previously failed deep
	// inside Service creation with an opaque error (#219).
	if name := pvcSeedProxyName(ownerName); len(name) > 63 {
		return false, fmt.Errorf("PVC seed proxy name %q exceeds the 63-character Service name limit; shorten the owning CR's name (max %d chars)", name, 63-len("backup-seed-proxy-"))
	}
	namespace := owner.GetNamespace()
	logger := log.FromContext(ctx).WithValues("proxy", pvcSeedProxyName(ownerName))

	// Service first — kubelet starts the Pod's CoreDNS entry from the
	// Service spec, so creating Service before Deployment minimises the
	// window where DNS resolution fails.
	if err := ensurePVCSeedProxyService(ctx, c, scheme, owner, ownerName); err != nil {
		return false, fmt.Errorf("ensure proxy Service: %w", err)
	}

	depKey := types.NamespacedName{Name: pvcSeedProxyName(ownerName), Namespace: namespace}
	existing := &appsv1.Deployment{}
	getErr := c.Get(ctx, depKey, existing)
	if getErr != nil && !apierrors.IsNotFound(getErr) {
		return false, fmt.Errorf("get proxy Deployment: %w", getErr)
	}
	if apierrors.IsNotFound(getErr) {
		dep := buildPVCSeedProxyDeployment(owner, ownerName, backupPVCName)
		if err := controllerutil.SetControllerReference(owner, dep, scheme); err != nil {
			return false, fmt.Errorf("set owner reference on proxy Deployment: %w", err)
		}
		if err := c.Create(ctx, dep); err != nil {
			return false, fmt.Errorf("create proxy Deployment: %w", err)
		}
		logger.Info("Created PVC seed proxy Deployment", "backupPVC", backupPVCName)
		return false, nil // freshly created; not yet ready
	}

	return existing.Status.ReadyReplicas > 0, nil
}

// teardownPVCSeedProxyResources deletes the proxy Deployment + Service (and
// its NetworkPolicy) once the seed is no longer needed (#219). The proxy
// serves the ENTIRE backup PVC unauthenticated inside the cluster, and its
// owner CR (a Neo4jRestore users keep as a record, or a long-lived sharded
// DB) typically outlives the restore by months — owner-ref GC alone left it
// running indefinitely. Idempotent; NotFound is success.
func teardownPVCSeedProxyResources(ctx context.Context, c client.Client, namespace, ownerName string) error {
	name := pvcSeedProxyName(ownerName)
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	if err := c.Delete(ctx, dep, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete proxy Deployment: %w", err)
	}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	if err := c.Delete(ctx, svc); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete proxy Service: %w", err)
	}
	np := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	if err := c.Delete(ctx, np); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete proxy NetworkPolicy: %w", err)
	}
	return nil
}

// ensurePVCSeedProxyNetworkPolicy restricts proxy ingress to the target
// cluster's server pods on the proxy port (#219): without it, busybox httpd
// serves the ENTIRE backup PVC (directory listings included) to any pod in
// the cluster. Effective only on enforcing CNIs; harmless no-op elsewhere.
// targetClusterName may be empty (unknown target) — then no policy is
// emitted, preserving previous behavior.
func ensurePVCSeedProxyNetworkPolicy(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, ownerName, targetClusterName string) error {
	if targetClusterName == "" {
		return nil
	}
	name := pvcSeedProxyName(ownerName)
	key := types.NamespacedName{Name: name, Namespace: owner.GetNamespace()}
	existing := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, existing); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: owner.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/name":       "backup-seed-proxy",
				"app.kubernetes.io/managed-by": "neo4j-operator",
				"app.kubernetes.io/instance":   ownerName,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "backup-seed-proxy",
					"app.kubernetes.io/instance": ownerName,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"neo4j.com/cluster": targetClusterName},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: func() *corev1.Protocol { p := corev1.ProtocolTCP; return &p }(),
							Port:     func() *intstr.IntOrString { v := intstr.FromInt(pvcSeedProxyPort); return &v }(),
						},
					},
				},
			},
		},
	}
	if err := controllerutil.SetControllerReference(owner, np, scheme); err != nil {
		return fmt.Errorf("set owner reference on proxy NetworkPolicy: %w", err)
	}
	return c.Create(ctx, np)
}

// ensurePVCSeedProxyService creates (idempotent) the ClusterIP Service in
// front of the proxy Deployment. The Service name matches pvcSeedProxyName
// so DNS resolution inside the cluster points at it.
func ensurePVCSeedProxyService(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	owner client.Object,
	ownerName string,
) error {
	svcKey := types.NamespacedName{Name: pvcSeedProxyName(ownerName), Namespace: owner.GetNamespace()}
	existing := &corev1.Service{}
	if err := c.Get(ctx, svcKey, existing); err == nil {
		return nil // already exists
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcSeedProxyName(ownerName),
			Namespace: owner.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/name":       "backup-seed-proxy",
				"app.kubernetes.io/managed-by": "neo4j-operator",
				"app.kubernetes.io/instance":   ownerName,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/name":     "backup-seed-proxy",
				"app.kubernetes.io/instance": ownerName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       pvcSeedProxyPort,
					TargetPort: intstr.FromInt(pvcSeedProxyPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
	if err := controllerutil.SetControllerReference(owner, svc, scheme); err != nil {
		return fmt.Errorf("set owner reference on proxy Service: %w", err)
	}
	return c.Create(ctx, svc)
}

// buildPVCSeedProxyDeployment renders the Deployment that runs busybox httpd
// against the backup PVC. busybox over nginx because tiny (~5 MiB), no
// config file needed, serves static files + directory listings out of the
// box with sensible defaults for `.backup` (octet-stream).
//
// Pod template:
//   - mounts the backup PVC RO at /backup,
//   - uid/gid 1000 + readOnlyRootFilesystem on the httpd container,
//   - exposes :8080.
func buildPVCSeedProxyDeployment(owner client.Object, ownerName, backupPVCName string) *appsv1.Deployment {
	replicas := int32(1)
	labels := map[string]string{
		"app.kubernetes.io/name":       "backup-seed-proxy",
		"app.kubernetes.io/managed-by": "neo4j-operator",
		"app.kubernetes.io/instance":   ownerName,
	}
	readOnlyRoot := true
	allowPrivilegeEscalation := false
	runAsNonRoot := true
	runAsUser := int64(1000)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcSeedProxyName(ownerName),
			Namespace: owner.GetNamespace(),
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    pvcSeedProxyContainerName,
						Image:   "busybox:1.36",
						Command: []string{"sh", "-c", fmt.Sprintf("httpd -f -v -p %d -h /backup", pvcSeedProxyPort)},
						Ports: []corev1.ContainerPort{{
							ContainerPort: pvcSeedProxyPort,
							Name:          "http",
							Protocol:      corev1.ProtocolTCP,
						}},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "backup",
							MountPath: "/backup",
							ReadOnly:  true,
						}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("16Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             &runAsNonRoot,
							RunAsUser:                &runAsUser,
							ReadOnlyRootFilesystem:   &readOnlyRoot,
							AllowPrivilegeEscalation: &allowPrivilegeEscalation,
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.FromInt(pvcSeedProxyPort),
								},
							},
							InitialDelaySeconds: 2,
							PeriodSeconds:       5,
						},
					}},
					Volumes: []corev1.Volume{{
						Name: "backup",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: backupPVCName,
								ReadOnly:  true,
							},
						},
					}},
				},
			},
		},
	}
}
