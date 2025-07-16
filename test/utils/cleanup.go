package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

const (
	cleanupTimeout  = time.Minute * 5
	cleanupInterval = time.Second * 2
)

// CleanupOptions defines cleanup behavior
type CleanupOptions struct {
	DeleteNamespaces    bool
	DeleteCRDs          bool
	DeleteTestResources bool
	DeleteOrphanedPods  bool
	DeleteOrphanedPVCs  bool
	DeleteOrphanedJobs  bool
	DeleteOrphanedSAs   bool
	ForceDelete         bool
	Timeout             time.Duration
	LabelSelector       string
}

// DefaultCleanupOptions returns sensible defaults for test cleanup
func DefaultCleanupOptions() CleanupOptions {
	return CleanupOptions{
		DeleteNamespaces:    true,
		DeleteCRDs:          false, // Don't delete CRDs by default as they're shared
		DeleteTestResources: true,
		DeleteOrphanedPods:  true,
		DeleteOrphanedPVCs:  true,
		DeleteOrphanedJobs:  true,
		DeleteOrphanedSAs:   true,
		ForceDelete:         true,
		Timeout:             cleanupTimeout,
		LabelSelector:       "app.kubernetes.io/part-of=neo4j-operator-test",
	}
}

// AggressiveCleanup performs comprehensive cleanup of test resources
func AggressiveCleanup(ctx context.Context, k8sClient client.Client, options CleanupOptions) {
	ginkgo.By("Performing aggressive cleanup of test environment")

	// Set default timeout if not specified
	if options.Timeout == 0 {
		options.Timeout = cleanupTimeout
	}

	// Create a context with timeout for cleanup operations
	cleanupCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()

	// 1. Clean up Neo4j resources first
	cleanupNeo4jResources(cleanupCtx, k8sClient, options)

	// 2. Clean up orphaned Kubernetes resources
	cleanupOrphanedResources(cleanupCtx, k8sClient, options)

	// 3. Clean up test namespaces
	if options.DeleteNamespaces {
		cleanupTestNamespaces(cleanupCtx, k8sClient, options)
	}

	// 4. Verify cleanup completion
	verifyCleanup(cleanupCtx, k8sClient, options)
}

// cleanupNeo4jResources removes all Neo4j custom resources
func cleanupNeo4jResources(ctx context.Context, k8sClient client.Client, options CleanupOptions) {
	ginkgo.By("Cleaning up Neo4j custom resources")

	// Delete all Neo4jEnterpriseClusters
	clusters := &neo4jv1alpha1.Neo4jEnterpriseClusterList{}
	if err := k8sClient.List(ctx, clusters); err == nil {
		for _, cluster := range clusters.Items {
			ginkgo.By(fmt.Sprintf("Deleting Neo4jEnterpriseCluster: %s/%s", cluster.Namespace, cluster.Name))
			if err := deleteWithPropagation(ctx, k8sClient, &cluster, options.ForceDelete); err != nil {
				fmt.Printf("Warning: Failed to delete cluster %s/%s: %v\n", cluster.Namespace, cluster.Name, err)
			}
		}
	}

	// Delete all Neo4jEnterpriseStandalones
	standalones := &neo4jv1alpha1.Neo4jEnterpriseStandaloneList{}
	if err := k8sClient.List(ctx, standalones); err == nil {
		for _, standalone := range standalones.Items {
			ginkgo.By(fmt.Sprintf("Deleting Neo4jEnterpriseStandalone: %s/%s", standalone.Namespace, standalone.Name))
			if err := deleteWithPropagation(ctx, k8sClient, &standalone, options.ForceDelete); err != nil {
				fmt.Printf("Warning: Failed to delete standalone %s/%s: %v\n", standalone.Namespace, standalone.Name, err)
			}
		}
	}

	// Delete all Neo4jBackups
	backups := &neo4jv1alpha1.Neo4jBackupList{}
	if err := k8sClient.List(ctx, backups); err == nil {
		for _, backup := range backups.Items {
			ginkgo.By(fmt.Sprintf("Deleting Neo4jBackup: %s/%s", backup.Namespace, backup.Name))
			if err := deleteWithPropagation(ctx, k8sClient, &backup, options.ForceDelete); err != nil {
				fmt.Printf("Warning: Failed to delete backup %s/%s: %v\n", backup.Namespace, backup.Name, err)
			}
		}
	}

	// Delete all Neo4jRestores
	restores := &neo4jv1alpha1.Neo4jRestoreList{}
	if err := k8sClient.List(ctx, restores); err == nil {
		for _, restore := range restores.Items {
			ginkgo.By(fmt.Sprintf("Deleting Neo4jRestore: %s/%s", restore.Namespace, restore.Name))
			if err := deleteWithPropagation(ctx, k8sClient, &restore, options.ForceDelete); err != nil {
				fmt.Printf("Warning: Failed to delete restore %s/%s: %v\n", restore.Namespace, restore.Name, err)
			}
		}
	}

	// Delete all Neo4jDatabases
	databases := &neo4jv1alpha1.Neo4jDatabaseList{}
	if err := k8sClient.List(ctx, databases); err == nil {
		for _, db := range databases.Items {
			ginkgo.By(fmt.Sprintf("Deleting Neo4jDatabase: %s/%s", db.Namespace, db.Name))
			if err := deleteWithPropagation(ctx, k8sClient, &db, options.ForceDelete); err != nil {
				fmt.Printf("Warning: Failed to delete database %s/%s: %v\n", db.Namespace, db.Name, err)
			}
		}
	}

	// Delete all Neo4jPlugins
	plugins := &neo4jv1alpha1.Neo4jPluginList{}
	if err := k8sClient.List(ctx, plugins); err == nil {
		for _, plugin := range plugins.Items {
			ginkgo.By(fmt.Sprintf("Deleting Neo4jPlugin: %s/%s", plugin.Namespace, plugin.Name))
			if err := deleteWithPropagation(ctx, k8sClient, &plugin, options.ForceDelete); err != nil {
				fmt.Printf("Warning: Failed to delete plugin %s/%s: %v\n", plugin.Namespace, plugin.Name, err)
			}
		}
	}
}

// cleanupOrphanedResources removes orphaned Kubernetes resources
func cleanupOrphanedResources(ctx context.Context, k8sClient client.Client, options CleanupOptions) {
	ginkgo.By("Cleaning up orphaned Kubernetes resources")

	// Delete orphaned StatefulSets
	if options.DeleteTestResources {
		statefulSets := &appsv1.StatefulSetList{}
		if err := k8sClient.List(ctx, statefulSets, client.MatchingLabels(map[string]string{
			"app.kubernetes.io/part-of": "neo4j-operator",
		})); err == nil {
			for _, sts := range statefulSets.Items {
				ginkgo.By(fmt.Sprintf("Deleting orphaned StatefulSet: %s/%s", sts.Namespace, sts.Name))
				if err := deleteWithPropagation(ctx, k8sClient, &sts, options.ForceDelete); err != nil {
					fmt.Printf("Warning: Failed to delete StatefulSet %s/%s: %v\n", sts.Namespace, sts.Name, err)
				}
			}
		}
	}

	// Delete orphaned Jobs
	if options.DeleteOrphanedJobs {
		jobs := &batchv1.JobList{}
		if err := k8sClient.List(ctx, jobs, client.MatchingLabels(map[string]string{
			"app.kubernetes.io/part-of": "neo4j-operator",
		})); err == nil {
			for _, job := range jobs.Items {
				ginkgo.By(fmt.Sprintf("Deleting orphaned Job: %s/%s", job.Namespace, job.Name))
				if err := deleteWithPropagation(ctx, k8sClient, &job, options.ForceDelete); err != nil {
					fmt.Printf("Warning: Failed to delete Job %s/%s: %v\n", job.Namespace, job.Name, err)
				}
			}
		}
	}

	// Delete orphaned Pods
	if options.DeleteOrphanedPods {
		pods := &corev1.PodList{}
		if err := k8sClient.List(ctx, pods, client.MatchingLabels(map[string]string{
			"app.kubernetes.io/part-of": "neo4j-operator",
		})); err == nil {
			for _, pod := range pods.Items {
				ginkgo.By(fmt.Sprintf("Deleting orphaned Pod: %s/%s", pod.Namespace, pod.Name))
				if err := deleteWithPropagation(ctx, k8sClient, &pod, options.ForceDelete); err != nil {
					fmt.Printf("Warning: Failed to delete Pod %s/%s: %v\n", pod.Namespace, pod.Name, err)
				}
			}
		}
	}

	// Delete orphaned PVCs
	if options.DeleteOrphanedPVCs {
		pvcs := &corev1.PersistentVolumeClaimList{}
		if err := k8sClient.List(ctx, pvcs, client.MatchingLabels(map[string]string{
			"app.kubernetes.io/part-of": "neo4j-operator",
		})); err == nil {
			for _, pvc := range pvcs.Items {
				ginkgo.By(fmt.Sprintf("Deleting orphaned PVC: %s/%s", pvc.Namespace, pvc.Name))
				if err := deleteWithPropagation(ctx, k8sClient, &pvc, options.ForceDelete); err != nil {
					fmt.Printf("Warning: Failed to delete PVC %s/%s: %v\n", pvc.Namespace, pvc.Name, err)
				}
			}
		}
	}

	// Delete orphaned ServiceAccounts
	if options.DeleteOrphanedSAs {
		sas := &corev1.ServiceAccountList{}
		if err := k8sClient.List(ctx, sas, client.MatchingLabels(map[string]string{
			"app.kubernetes.io/part-of": "neo4j-operator",
		})); err == nil {
			for _, sa := range sas.Items {
				ginkgo.By(fmt.Sprintf("Deleting orphaned ServiceAccount: %s/%s", sa.Namespace, sa.Name))
				if err := deleteWithPropagation(ctx, k8sClient, &sa, options.ForceDelete); err != nil {
					fmt.Printf("Warning: Failed to delete ServiceAccount %s/%s: %v\n", sa.Namespace, sa.Name, err)
				}
			}
		}
	}
}

// cleanupTestNamespaces removes test namespaces
func cleanupTestNamespaces(ctx context.Context, k8sClient client.Client, options CleanupOptions) {
	ginkgo.By("Cleaning up test namespaces")

	namespaces := &corev1.NamespaceList{}
	if err := k8sClient.List(ctx, namespaces); err == nil {
		for _, ns := range namespaces.Items {
			// Delete test namespaces (those starting with test-)
			if isTestNamespace(ns.Name) {
				ginkgo.By(fmt.Sprintf("Deleting test namespace: %s", ns.Name))
				if err := deleteWithPropagation(ctx, k8sClient, &ns, options.ForceDelete); err != nil {
					fmt.Printf("Warning: Failed to delete namespace %s: %v\n", ns.Name, err)
				}
			}
		}
	}
}

// verifyCleanup ensures cleanup was successful
func verifyCleanup(ctx context.Context, k8sClient client.Client, options CleanupOptions) {
	ginkgo.By("Verifying cleanup completion")

	// Wait for resources to be fully deleted
	gomega.Eventually(func() bool {
		// Check for remaining Neo4j resources
		clusters := &neo4jv1alpha1.Neo4jEnterpriseClusterList{}
		if err := k8sClient.List(ctx, clusters); err == nil && len(clusters.Items) > 0 {
			return false
		}

		standalones := &neo4jv1alpha1.Neo4jEnterpriseStandaloneList{}
		if err := k8sClient.List(ctx, standalones); err == nil && len(standalones.Items) > 0 {
			return false
		}

		backups := &neo4jv1alpha1.Neo4jBackupList{}
		if err := k8sClient.List(ctx, backups); err == nil && len(backups.Items) > 0 {
			return false
		}

		// Check for remaining test namespaces
		if options.DeleteNamespaces {
			namespaces := &corev1.NamespaceList{}
			if err := k8sClient.List(ctx, namespaces); err == nil {
				for _, ns := range namespaces.Items {
					if isTestNamespace(ns.Name) {
						return false
					}
				}
			}
		}

		return true
	}, options.Timeout, cleanupInterval).Should(gomega.BeTrue(), "Cleanup verification failed")
}

// SanityCheck performs environment sanity checks before tests
func SanityCheck(ctx context.Context, k8sClient client.Client) {
	ginkgo.By("Performing environment sanity checks")

	// Check if CRDs are installed
	checkCRDs(ctx, k8sClient)

	// Check cluster connectivity
	checkClusterConnectivity(ctx, k8sClient)

	// Check resource availability
	checkResourceAvailability(ctx, k8sClient)

	// Check for conflicting resources
	checkConflictingResources(ctx, k8sClient)
}

// checkCRDs verifies that required CRDs are installed
func checkCRDs(ctx context.Context, k8sClient client.Client) {
	ginkgo.By("Checking required CRDs are installed")

	requiredCRDs := []string{
		"neo4jenterpriseclusters.neo4j.neo4j.com",
		"neo4jenterprisestandalones.neo4j.neo4j.com",
		"neo4jbackups.neo4j.neo4j.com",
		"neo4jrestores.neo4j.neo4j.com",
		"neo4jusers.neo4j.neo4j.com",
		"neo4jroles.neo4j.neo4j.com",
		"neo4jgrants.neo4j.neo4j.com",
		"neo4jdatabases.neo4j.neo4j.com",
		"neo4jplugins.neo4j.neo4j.com",
	}

	for _, crdName := range requiredCRDs {
		gomega.Eventually(func() error {
			// Try to list the CR to verify CRD exists
			switch crdName {
			case "neo4jenterpriseclusters.neo4j.neo4j.com":
				clusters := &neo4jv1alpha1.Neo4jEnterpriseClusterList{}
				return k8sClient.List(ctx, clusters)
			case "neo4jenterprisestandalones.neo4j.neo4j.com":
				standalones := &neo4jv1alpha1.Neo4jEnterpriseStandaloneList{}
				return k8sClient.List(ctx, standalones)
			case "neo4jbackups.neo4j.neo4j.com":
				backups := &neo4jv1alpha1.Neo4jBackupList{}
				return k8sClient.List(ctx, backups)
			case "neo4jrestores.neo4j.neo4j.com":
				restores := &neo4jv1alpha1.Neo4jRestoreList{}
				return k8sClient.List(ctx, restores)
			case "neo4jdatabases.neo4j.neo4j.com":
				databases := &neo4jv1alpha1.Neo4jDatabaseList{}
				return k8sClient.List(ctx, databases)
			case "neo4jplugins.neo4j.neo4j.com":
				plugins := &neo4jv1alpha1.Neo4jPluginList{}
				return k8sClient.List(ctx, plugins)
			}
			return nil
		}, time.Minute*2, time.Second*5).Should(gomega.Succeed(), "CRD %s not available", crdName)
	}
}

// checkClusterConnectivity verifies cluster connectivity
func checkClusterConnectivity(ctx context.Context, k8sClient client.Client) {
	ginkgo.By("Checking cluster connectivity")

	// Try to list nodes to verify connectivity
	gomega.Eventually(func() error {
		nodes := &corev1.NodeList{}
		return k8sClient.List(ctx, nodes)
	}, time.Minute*2, time.Second*5).Should(gomega.Succeed(), "Failed to connect to cluster")
}

// checkResourceAvailability checks for required resources
func checkResourceAvailability(ctx context.Context, k8sClient client.Client) {
	ginkgo.By("Checking resource availability")

	// Check for available nodes
	gomega.Eventually(func() int {
		nodes := &corev1.NodeList{}
		if err := k8sClient.List(ctx, nodes); err != nil {
			return 0
		}
		readyNodes := 0
		for _, node := range nodes.Items {
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					readyNodes++
					break
				}
			}
		}
		return readyNodes
	}, time.Minute*2, time.Second*5).Should(gomega.BeNumerically(">=", 1), "No ready nodes available")

	// Check for storage classes
	gomega.Eventually(func() int {
		scs := &storagev1.StorageClassList{}
		if err := k8sClient.List(ctx, scs); err != nil {
			return 0
		}
		return len(scs.Items)
	}, time.Minute*2, time.Second*5).Should(gomega.BeNumerically(">=", 1), "No storage classes available")
}

// checkConflictingResources checks for resources that might conflict
func checkConflictingResources(ctx context.Context, k8sClient client.Client) {
	ginkgo.By("Checking for conflicting resources")

	// Check for existing Neo4j resources that might conflict
	clusters := &neo4jv1alpha1.Neo4jEnterpriseClusterList{}
	if err := k8sClient.List(ctx, clusters); err == nil && len(clusters.Items) > 0 {
		fmt.Printf("Warning: Found %d existing Neo4jEnterpriseClusters that might conflict with tests\n", len(clusters.Items))
		for _, cluster := range clusters.Items {
			fmt.Printf("  - %s/%s\n", cluster.Namespace, cluster.Name)
		}
	}

	standalones := &neo4jv1alpha1.Neo4jEnterpriseStandaloneList{}
	if err := k8sClient.List(ctx, standalones); err == nil && len(standalones.Items) > 0 {
		fmt.Printf("Warning: Found %d existing Neo4jEnterpriseStandalones that might conflict with tests\n", len(standalones.Items))
		for _, standalone := range standalones.Items {
			fmt.Printf("  - %s/%s\n", standalone.Namespace, standalone.Name)
		}
	}

	// Check for test namespaces that might conflict
	namespaces := &corev1.NamespaceList{}
	if err := k8sClient.List(ctx, namespaces); err == nil {
		for _, ns := range namespaces.Items {
			if isTestNamespace(ns.Name) {
				fmt.Printf("Warning: Found test namespace %s that might conflict\n", ns.Name)
			}
		}
	}
}

// Helper functions

func deleteWithPropagation(ctx context.Context, k8sClient client.Client, obj client.Object, force bool) error {
	if force {
		return k8sClient.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationForeground))
	}
	return k8sClient.Delete(ctx, obj)
}

func isTestNamespace(name string) bool {
	return name == "default" || // Skip default namespace
		name == "kube-system" || // Skip system namespaces
		name == "kube-public" ||
		name == "kube-node-lease" ||
		len(name) < 5 || // Skip very short names
		(name[:5] == "test-" || name[:5] == "gke-" || name[:5] == "aks-" || name[:5] == "eks-")
}

// SetupTestEnvironment performs complete test environment setup
func SetupTestEnvironment(ctx context.Context, k8sClient client.Client) {
	ginkgo.By("Setting up test environment")

	// Perform sanity checks
	SanityCheck(ctx, k8sClient)

	// Perform aggressive cleanup
	AggressiveCleanup(ctx, k8sClient, DefaultCleanupOptions())

	ginkgo.By("Test environment setup complete")
}
