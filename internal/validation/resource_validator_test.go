package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

func TestIsNodeSchedulable(t *testing.T) {
	v := &ResourceValidator{}

	t.Run("ready schedulable node", func(t *testing.T) {
		node := &corev1.Node{
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		}
		assert.True(t, v.isNodeSchedulable(node))
	})

	t.Run("not ready node", func(t *testing.T) {
		node := &corev1.Node{
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
				},
			},
		}
		assert.False(t, v.isNodeSchedulable(node))
	})

	t.Run("cordoned node", func(t *testing.T) {
		node := &corev1.Node{
			Spec: corev1.NodeSpec{Unschedulable: true},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		}
		assert.False(t, v.isNodeSchedulable(node))
	})

	t.Run("NoSchedule taint", func(t *testing.T) {
		node := &corev1.Node{
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{
					{Key: "dedicated", Effect: corev1.TaintEffectNoSchedule},
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		}
		assert.False(t, v.isNodeSchedulable(node))
	})

	t.Run("NoExecute taint still schedulable", func(t *testing.T) {
		node := &corev1.Node{
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{
					{Key: "node.kubernetes.io/unreachable", Effect: corev1.TaintEffectNoExecute},
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		}
		assert.True(t, v.isNodeSchedulable(node))
	})
}

func TestCalculateRequiredResources(t *testing.T) {
	v := &ResourceValidator{}

	t.Run("uses defaults when no resources specified", func(t *testing.T) {
		cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		info := v.calculateRequiredResources(cluster, 3)
		// Defaults: 2Gi memory, 500m CPU per pod × 3 pods
		assert.Equal(t, int64(3*2*1024*1024*1024), info.Memory.Value())
		assert.Equal(t, int64(3*500), info.CPU.MilliValue())
	})

	t.Run("uses custom resources", func(t *testing.T) {
		cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				AcceptLicenseAgreement: "eval",
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						corev1.ResourceCPU:    resource.MustParse("1"),
					},
				},
			},
		}
		info := v.calculateRequiredResources(cluster, 2)
		assert.Equal(t, int64(2*4*1024*1024*1024), info.Memory.Value())
		assert.Equal(t, int64(2*1000), info.CPU.MilliValue())
	})

	t.Run("single pod", func(t *testing.T) {
		cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		info := v.calculateRequiredResources(cluster, 1)
		assert.Equal(t, int64(2*1024*1024*1024), info.Memory.Value())
		assert.Equal(t, int64(500), info.CPU.MilliValue())
	})
}

func TestCalculateMaxPodsPerNode(t *testing.T) {
	v := &ResourceValidator{}

	t.Run("no nodes returns default 110", func(t *testing.T) {
		assert.Equal(t, 110, v.calculateMaxPodsPerNode(nil))
	})

	t.Run("all nodes default capacity", func(t *testing.T) {
		nodes := []corev1.Node{
			{Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
				corev1.ResourcePods: resource.MustParse("110"),
			}}},
		}
		assert.Equal(t, 110, v.calculateMaxPodsPerNode(nodes))
	})

	t.Run("returns minimum across nodes", func(t *testing.T) {
		nodes := []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "node1"},
				Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
					corev1.ResourcePods: resource.MustParse("110"),
				}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "node2"},
				Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
					corev1.ResourcePods: resource.MustParse("50"),
				}},
			},
		}
		assert.Equal(t, 50, v.calculateMaxPodsPerNode(nodes))
	})

	t.Run("node without pod capacity uses default", func(t *testing.T) {
		nodes := []corev1.Node{
			{Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{}}},
		}
		assert.Equal(t, 110, v.calculateMaxPodsPerNode(nodes))
	})
}
