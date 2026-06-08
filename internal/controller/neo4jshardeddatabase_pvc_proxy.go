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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
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
// during a sharded restore. One proxy per sharded-DB CR keeps lifecycle
// management simple (owner-reference GC when the sharded DB is deleted).
//
// The 63-char DNS label limit applies; long sharded-DB names may need to
// be truncated by callers. For now we just return the full string; the
// validator already caps sharded-DB names short of the limit.
func pvcSeedProxyName(shardedDBName string) string {
	return "backup-seed-proxy-" + shardedDBName
}

// pvcSeedProxyURLForShard builds the HTTP URL that Neo4j's
// URLConnectionSeedProvider will fetch for one shard's `.backup` file.
//
// The proxy mounts the backup PVC at /backup, so URLs include the
// per-run subdirectory (`{backupsPath}`) and the exact artifact filename
// captured by F3 (`ShardArtifact.Filename`).
//
// Example: `http://backup-seed-proxy-products.test-ns.svc.cluster.local:8080/products-backup-backup/products-g000-2025-06-11T21-04-42.backup`
func pvcSeedProxyURLForShard(shardedDBName, namespace, backupsPath, filename string) string {
	return fmt.Sprintf(
		"http://%s.%s.svc.cluster.local:%d/%s/%s",
		pvcSeedProxyName(shardedDBName),
		namespace,
		pvcSeedProxyPort,
		backupsPath,
		filename,
	)
}

// ensurePVCSeedProxy creates (or updates) the Deployment + Service that
// expose the named backup PVC over HTTP so Neo4j cluster pods can fetch
// per-shard `.backup` files via URLConnectionSeedProvider.
//
// One proxy is created per Neo4jShardedDatabase CR with the sharded DB
// CR as owner — so the proxy is GC'd by Kubernetes when the sharded DB
// is deleted. Idempotent: subsequent reconciles with the same args
// no-op (create-if-not-exists semantics) so we don't trigger pod
// restarts on every reconcile.
//
// The proxy uses busybox httpd, which serves static files and directory
// listings from a chroot. Container runs read-only with the PVC mounted
// at /backup; httpd serves from there.
//
// Returns (proxyAvailable bool, err error). proxyAvailable=true means
// the Deployment reports at least one Ready replica; caller can construct
// per-shard URLs and pass them to Neo4j's CREATE DATABASE OPTIONS. False
// + nil err means the proxy is still rolling out — caller should requeue.
// err != nil for permanent failures (missing PVC reference, etc.).
func (r *Neo4jShardedDatabaseReconciler) ensurePVCSeedProxy(
	ctx context.Context,
	shardedDB *neo4jv1beta1.Neo4jShardedDatabase,
	backupPVCName string,
) (proxyAvailable bool, err error) {
	logger := log.FromContext(ctx).WithValues("proxy", pvcSeedProxyName(shardedDB.Name))

	if backupPVCName == "" {
		return false, fmt.Errorf("PVC seed proxy requires a backup PVC name; got empty")
	}

	depName := pvcSeedProxyName(shardedDB.Name)
	depKey := types.NamespacedName{Name: depName, Namespace: shardedDB.Namespace}

	// Service first — kubelet starts the Pod's CoreDNS entry from the
	// Service spec, so creating Service before Deployment minimises the
	// window where DNS resolution fails.
	if err := r.ensurePVCSeedProxyService(ctx, shardedDB); err != nil {
		return false, fmt.Errorf("ensure proxy Service: %w", err)
	}

	existing := &appsv1.Deployment{}
	getErr := r.Get(ctx, depKey, existing)
	if getErr != nil && !apierrors.IsNotFound(getErr) {
		return false, fmt.Errorf("get proxy Deployment: %w", getErr)
	}
	if apierrors.IsNotFound(getErr) {
		dep := r.buildPVCSeedProxyDeployment(shardedDB, backupPVCName)
		if err := r.Create(ctx, dep); err != nil {
			return false, fmt.Errorf("create proxy Deployment: %w", err)
		}
		logger.Info("Created PVC seed proxy Deployment", "backupPVC", backupPVCName)
		return false, nil // freshly created; not yet ready
	}

	// Already exists. Check Ready count rather than reconciling spec
	// drift — the user shouldn't be editing the proxy resources, and
	// reconciling spec would trigger pod restarts (which we minimise).
	return existing.Status.ReadyReplicas > 0, nil
}

// ensurePVCSeedProxyService creates (idempotent) the ClusterIP Service in
// front of the proxy Deployment. The Service name matches
// pvcSeedProxyName so DNS resolution inside the cluster points at it.
func (r *Neo4jShardedDatabaseReconciler) ensurePVCSeedProxyService(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase) error {
	svcKey := types.NamespacedName{Name: pvcSeedProxyName(shardedDB.Name), Namespace: shardedDB.Namespace}
	existing := &corev1.Service{}
	if err := r.Get(ctx, svcKey, existing); err == nil {
		return nil // already exists
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcSeedProxyName(shardedDB.Name),
			Namespace: shardedDB.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "backup-seed-proxy",
				"app.kubernetes.io/managed-by": "neo4j-operator",
				"app.kubernetes.io/instance":   shardedDB.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/name":     "backup-seed-proxy",
				"app.kubernetes.io/instance": shardedDB.Name,
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
	if err := controllerutil.SetControllerReference(shardedDB, svc, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference on proxy Service: %w", err)
	}
	return r.Create(ctx, svc)
}

// buildPVCSeedProxyDeployment renders the Deployment that runs busybox
// httpd against the backup PVC. busybox is chosen over nginx because
// it's tiny (~5 MiB), needs no config file, and serves static files +
// directory listings out of the box.
//
// The Pod template:
//   - mounts the backup PVC RO at /backup,
//   - sets uid/gid 1000 + readOnlyRootFilesystem for the httpd container
//     (busybox httpd doesn't need writable root),
//   - exposes :8080.
func (r *Neo4jShardedDatabaseReconciler) buildPVCSeedProxyDeployment(shardedDB *neo4jv1beta1.Neo4jShardedDatabase, backupPVCName string) *appsv1.Deployment {
	replicas := int32(1)
	labels := map[string]string{
		"app.kubernetes.io/name":       "backup-seed-proxy",
		"app.kubernetes.io/managed-by": "neo4j-operator",
		"app.kubernetes.io/instance":   shardedDB.Name,
	}
	readOnlyRoot := true
	allowPrivilegeEscalation := false
	runAsNonRoot := true
	runAsUser := int64(1000)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcSeedProxyName(shardedDB.Name),
			Namespace: shardedDB.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  pvcSeedProxyContainerName,
						Image: "busybox:1.36",
						// `-f` foreground, `-v` verbose, `-p` port, `-h`
						// document root. busybox httpd serves directory
						// listings + file fetches with sensible Content-Type
						// for `.backup` (octet-stream) without config.
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
	_ = controllerutil.SetControllerReference(shardedDB, dep, r.Scheme)
	return dep
}
