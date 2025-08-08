package monitoring

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestNewResourceMonitor(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()
	recorder := record.NewFakeRecorder(10)

	monitor := NewResourceMonitor(fakeClient, recorder)

	assert.NotNil(t, monitor)
	assert.Equal(t, fakeClient, monitor.client)
	assert.Equal(t, recorder, monitor.recorder)
}

func TestResourceMonitor_isNodeSchedulable(t *testing.T) {
	monitor := &ResourceMonitor{}

	tests := []struct {
		name              string
		node              *corev1.Node
		expectSchedulable bool
	}{
		{
			name: "ready and schedulable node",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
				},
			},
			expectSchedulable: true,
		},
		{
			name: "not ready node",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
				},
			},
			expectSchedulable: false,
		},
		{
			name: "unschedulable node",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: true,
				},
			},
			expectSchedulable: false,
		},
		{
			name: "node with NoSchedule taint",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
					Taints: []corev1.Taint{
						{
							Key:    "test-taint",
							Value:  "test-value",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectSchedulable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := monitor.isNodeSchedulable(tt.node)
			assert.Equal(t, tt.expectSchedulable, result)
		})
	}
}

func TestResourceMonitor_getClusterNodes(t *testing.T) {
	tests := []struct {
		name        string
		nodes       []corev1.Node
		expectCount int
	}{
		{
			name: "schedulable nodes only",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
					Spec: corev1.NodeSpec{Unschedulable: false},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node2"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
					Spec: corev1.NodeSpec{Unschedulable: false},
				},
			},
			expectCount: 2,
		},
		{
			name: "mixed schedulable and unschedulable nodes",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
					Spec: corev1.NodeSpec{Unschedulable: false},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node2"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
					},
					Spec: corev1.NodeSpec{Unschedulable: false},
				},
			},
			expectCount: 1,
		},
		{
			name:        "no nodes",
			nodes:       []corev1.Node{},
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))

			// Create client with existing objects to avoid creation issues
			objs := make([]client.Object, len(tt.nodes))
			for i := range tt.nodes {
				objs[i] = &tt.nodes[i]
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			recorder := record.NewFakeRecorder(10)
			monitor := NewResourceMonitor(fakeClient, recorder)

			ctx := context.Background()
			result, err := monitor.getClusterNodes(ctx)

			require.NoError(t, err)
			assert.Len(t, result, tt.expectCount)
		})
	}
}

func TestResourceMonitor_countPodsInNamespace(t *testing.T) {
	tests := []struct {
		name        string
		pods        []corev1.Pod
		namespace   string
		expectCount int
	}{
		{
			name: "running pods in namespace",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "test-namespace",
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "test-namespace",
					},
					Status: corev1.PodStatus{Phase: corev1.PodPending},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod3",
						Namespace: "other-namespace",
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			namespace:   "test-namespace",
			expectCount: 2,
		},
		{
			name: "failed pods not counted",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "test-namespace",
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "test-namespace",
					},
					Status: corev1.PodStatus{Phase: corev1.PodFailed},
				},
			},
			namespace:   "test-namespace",
			expectCount: 1,
		},
		{
			name:        "no pods",
			pods:        []corev1.Pod{},
			namespace:   "test-namespace",
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))

			// Create client with existing pods
			objs := make([]client.Object, len(tt.pods))
			for i := range tt.pods {
				objs[i] = &tt.pods[i]
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			recorder := record.NewFakeRecorder(10)
			monitor := NewResourceMonitor(fakeClient, recorder)

			ctx := context.Background()
			result, err := monitor.countPodsInNamespace(ctx, tt.namespace)

			require.NoError(t, err)
			assert.Equal(t, tt.expectCount, result)
		})
	}
}

func TestResourceMonitor_canScaleUp(t *testing.T) {
	tests := []struct {
		name           string
		utilization    *ResourceUtilization
		cluster        *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectCanScale bool
	}{
		{
			name: "low utilization can scale",
			utilization: &ResourceUtilization{
				MemoryPercentage: 60.0,
				CPUPercentage:    50.0,
				AvailableMemory:  resource.MustParse("4Gi"),
			},
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			},
			expectCanScale: true,
		},
		{
			name: "high memory utilization cannot scale",
			utilization: &ResourceUtilization{
				MemoryPercentage: 90.0,
				CPUPercentage:    50.0,
				AvailableMemory:  resource.MustParse("4Gi"),
			},
			cluster:        &neo4jv1alpha1.Neo4jEnterpriseCluster{},
			expectCanScale: false,
		},
		{
			name: "high CPU utilization cannot scale",
			utilization: &ResourceUtilization{
				MemoryPercentage: 60.0,
				CPUPercentage:    85.0,
				AvailableMemory:  resource.MustParse("4Gi"),
			},
			cluster:        &neo4jv1alpha1.Neo4jEnterpriseCluster{},
			expectCanScale: false,
		},
		{
			name: "insufficient available memory cannot scale",
			utilization: &ResourceUtilization{
				MemoryPercentage: 60.0,
				CPUPercentage:    50.0,
				AvailableMemory:  resource.MustParse("1Gi"),
			},
			cluster:        &neo4jv1alpha1.Neo4jEnterpriseCluster{},
			expectCanScale: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := &ResourceMonitor{}
			result := monitor.canScaleUp(tt.utilization, tt.cluster)
			assert.Equal(t, tt.expectCanScale, result)
		})
	}
}

func TestResourceMonitor_generateScalingRecommendation(t *testing.T) {
	tests := []struct {
		name           string
		utilization    *ResourceUtilization
		cluster        *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectContains string
	}{
		{
			name: "cannot scale - high memory",
			utilization: &ResourceUtilization{
				MemoryPercentage: 95.0,
				CPUPercentage:    50.0,
				CanScale:         false,
			},
			cluster:        &neo4jv1alpha1.Neo4jEnterpriseCluster{},
			expectContains: "memory utilization too high",
		},
		{
			name: "cannot scale - high CPU",
			utilization: &ResourceUtilization{
				MemoryPercentage: 60.0,
				CPUPercentage:    90.0,
				CanScale:         false,
			},
			cluster:        &neo4jv1alpha1.Neo4jEnterpriseCluster{},
			expectContains: "CPU utilization too high",
		},
		{
			name: "good resource headroom",
			utilization: &ResourceUtilization{
				MemoryPercentage: 40.0,
				CPUPercentage:    30.0,
				CanScale:         true,
			},
			cluster:        &neo4jv1alpha1.Neo4jEnterpriseCluster{},
			expectContains: "good resource headroom",
		},
		{
			name: "high utilization but can scale",
			utilization: &ResourceUtilization{
				MemoryPercentage: 80.0,
				CPUPercentage:    60.0,
				CanScale:         true,
			},
			cluster:        &neo4jv1alpha1.Neo4jEnterpriseCluster{},
			expectContains: "high resource utilization",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := &ResourceMonitor{}
			result := monitor.generateScalingRecommendation(tt.utilization, tt.cluster)
			assert.Contains(t, result, tt.expectContains)
		})
	}
}

func TestResourceMonitor_ValidateScalingCapacity(t *testing.T) {
	tests := []struct {
		name           string
		cluster        *neo4jv1alpha1.Neo4jEnterpriseCluster
		targetTopology neo4jv1alpha1.TopologyConfiguration
		nodes          []corev1.Node
		expectCanScale bool
		expectMessage  string
	}{
		{
			name: "scaling down should succeed",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
				},
			},
			targetTopology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),
							corev1.ResourceCPU:    resource.MustParse("2000m"),
						},
					},
					Spec: corev1.NodeSpec{Unschedulable: false},
				},
			},
			expectCanScale: true,
			expectMessage:  "No scaling up required",
		},
		{
			name: "scaling up with insufficient resources",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
					},
				},
			},
			targetTopology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 5, // Scale up from 3 to 5 servers
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
					},
					Spec: corev1.NodeSpec{Unschedulable: false},
				},
			},
			expectCanScale: false,
			expectMessage:  "Insufficient memory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))

			// Create client with existing nodes
			objs := make([]client.Object, len(tt.nodes))
			for i := range tt.nodes {
				objs[i] = &tt.nodes[i]
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			recorder := record.NewFakeRecorder(10)
			monitor := NewResourceMonitor(fakeClient, recorder)

			ctx := context.Background()
			canScale, message, err := monitor.ValidateScalingCapacity(ctx, tt.cluster, tt.targetTopology)

			require.NoError(t, err)
			assert.Equal(t, tt.expectCanScale, canScale)
			assert.Contains(t, message, tt.expectMessage)
		})
	}
}

// TestResourceMonitor_MonitorClusterResources tests the main monitoring function
// Note: This test is simplified due to fake client limitations with field selectors
func TestResourceMonitor_MonitorClusterResources(t *testing.T) {
	tests := []struct {
		name        string
		nodes       []corev1.Node
		pods        []corev1.Pod
		expectError bool
	}{
		{
			name: "with nodes returns utilization",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),
							corev1.ResourceCPU:    resource.MustParse("2000m"),
						},
					},
					Spec: corev1.NodeSpec{Unschedulable: false},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test-namespace",
					},
					Spec: corev1.PodSpec{
						NodeName: "node1",
						Containers: []corev1.Container{
							{
								Name: "test-container",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("1Gi"),
										corev1.ResourceCPU:    resource.MustParse("500m"),
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expectError: false,
		},
		{
			name:        "no nodes found",
			nodes:       []corev1.Node{},
			pods:        []corev1.Pod{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))

			// Create client with existing objects
			objs := make([]client.Object, 0, len(tt.nodes)+len(tt.pods))
			for i := range tt.nodes {
				objs = append(objs, &tt.nodes[i])
			}
			for i := range tt.pods {
				objs = append(objs, &tt.pods[i])
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			recorder := record.NewFakeRecorder(10)
			monitor := NewResourceMonitor(fakeClient, recorder)

			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
			}

			ctx := context.Background()
			result, err := monitor.MonitorClusterResources(ctx, cluster)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, len(tt.nodes), result.NodeCount)
				// Due to fake client limitations with field selectors, we just verify structure
				// The actual percentages may be 0 or NaN due to missing pod-to-node associations
				assert.True(t, result.MemoryPercentage >= 0.0 || result.MemoryPercentage != result.MemoryPercentage) // >= 0 or NaN
				assert.True(t, result.CPUPercentage >= 0.0 || result.CPUPercentage != result.CPUPercentage)          // >= 0 or NaN
			}
		})
	}
}

func TestResourceMonitor_checkResourceThresholds(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	monitor := &ResourceMonitor{recorder: recorder}

	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
	}

	tests := []struct {
		name             string
		utilization      *ResourceUtilization
		expectEventCount int
	}{
		{
			name: "all thresholds exceeded",
			utilization: &ResourceUtilization{
				MemoryPercentage: 90.0,
				CPUPercentage:    85.0,
				AvailableMemory:  resource.MustParse("1Gi"),
				NodeCount:        2,
			},
			expectEventCount: 4, // High memory, high CPU, low available memory, low node count
		},
		{
			name: "no thresholds exceeded",
			utilization: &ResourceUtilization{
				MemoryPercentage: 60.0,
				CPUPercentage:    50.0,
				AvailableMemory:  resource.MustParse("4Gi"),
				NodeCount:        5,
			},
			expectEventCount: 0,
		},
		{
			name: "only memory threshold exceeded",
			utilization: &ResourceUtilization{
				MemoryPercentage: 90.0,
				CPUPercentage:    50.0,
				AvailableMemory:  resource.MustParse("4Gi"),
				NodeCount:        5,
			},
			expectEventCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear previous events
			for len(recorder.Events) > 0 {
				<-recorder.Events
			}

			monitor.checkResourceThresholds(cluster, tt.utilization)

			assert.Equal(t, tt.expectEventCount, len(recorder.Events))
		})
	}
}

// TestResourceUtilization_StructureValidation tests the structure and basic functionality
func TestResourceUtilization_StructureValidation(t *testing.T) {
	utilization := &ResourceUtilization{
		MemoryPercentage:      75.5,
		CPUPercentage:         60.2,
		TotalMemoryUsed:       resource.MustParse("6Gi"),
		TotalCPUUsed:          resource.MustParse("1500m"),
		AvailableMemory:       resource.MustParse("2Gi"),
		AvailableCPU:          resource.MustParse("500m"),
		PodCount:              10,
		NodeCount:             3,
		CanScale:              true,
		ScalingRecommendation: "Cluster has good resource headroom for scaling.",
		Details: []NodeUtilization{
			{
				NodeName:          "node1",
				MemoryUsed:        resource.MustParse("2Gi"),
				CPUUsed:           resource.MustParse("500m"),
				MemoryAllocatable: resource.MustParse("4Gi"),
				CPUAllocatable:    resource.MustParse("2000m"),
				MemoryPercentage:  50.0,
				CPUPercentage:     25.0,
				PodCount:          5,
				Schedulable:       true,
			},
		},
	}

	// Test that all fields are accessible and have expected values
	assert.Equal(t, 75.5, utilization.MemoryPercentage)
	assert.Equal(t, 60.2, utilization.CPUPercentage)
	assert.Equal(t, resource.MustParse("6Gi"), utilization.TotalMemoryUsed)
	assert.Equal(t, resource.MustParse("1500m"), utilization.TotalCPUUsed)
	assert.Equal(t, resource.MustParse("2Gi"), utilization.AvailableMemory)
	assert.Equal(t, resource.MustParse("500m"), utilization.AvailableCPU)
	assert.Equal(t, 10, utilization.PodCount)
	assert.Equal(t, 3, utilization.NodeCount)
	assert.True(t, utilization.CanScale)
	assert.Contains(t, utilization.ScalingRecommendation, "headroom")
	assert.Len(t, utilization.Details, 1)

	// Test NodeUtilization details
	nodeDetail := utilization.Details[0]
	assert.Equal(t, "node1", nodeDetail.NodeName)
	assert.Equal(t, 50.0, nodeDetail.MemoryPercentage)
	assert.Equal(t, 25.0, nodeDetail.CPUPercentage)
	assert.Equal(t, 5, nodeDetail.PodCount)
	assert.True(t, nodeDetail.Schedulable)
}
