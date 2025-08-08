package controller_test

import (
	"context"
	"testing"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/controller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTopologyScheduler_CalculateTopologyPlacement(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name    string
		cluster *neo4jv1alpha1.Neo4jEnterpriseCluster
		nodes   []corev1.Node
		want    *controller.TopologyPlacement
		wantErr bool
	}{
		{
			name: "basic server cluster without placement config",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone-a",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-2",
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone-b",
						},
					},
				},
			},
			want: &controller.TopologyPlacement{
				UseTopologySpread:   false,
				UseAntiAffinity:     false,
				AvailabilityZones:   []string{}, // Will be auto-discovered but empty in test
				EnforceDistribution: false,
			},
			wantErr: false,
		},
		{
			name: "server cluster with topology spread enabled",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers:             3, // Reduce to match 3 available zones
						EnforceDistribution: true,
						Placement: &neo4jv1alpha1.PlacementConfig{
							TopologySpread: &neo4jv1alpha1.TopologySpreadConfig{
								Enabled:           true,
								TopologyKey:       "topology.kubernetes.io/zone",
								MaxSkew:           1,
								WhenUnsatisfiable: "DoNotSchedule",
							},
							AntiAffinity: &neo4jv1alpha1.PodAntiAffinityConfig{
								Enabled: true,
								Type:    "required",
							},
						},
					},
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone-a",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-2",
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone-b",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-3",
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone-c",
						},
					},
				},
			},
			want: &controller.TopologyPlacement{
				UseTopologySpread:   true,
				UseAntiAffinity:     true,
				AvailabilityZones:   []string{"zone-a", "zone-b", "zone-c"},
				EnforceDistribution: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with nodes
			objs := make([]runtime.Object, len(tt.nodes))
			for i, node := range tt.nodes {
				objs[i] = &node
			}
			client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			ts := controller.NewTopologyScheduler(client)
			got, err := ts.CalculateTopologyPlacement(context.Background(), tt.cluster)

			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateTopologyPlacement() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if got.UseTopologySpread != tt.want.UseTopologySpread {
					t.Errorf("UseTopologySpread = %v, want %v", got.UseTopologySpread, tt.want.UseTopologySpread)
				}
				if got.UseAntiAffinity != tt.want.UseAntiAffinity {
					t.Errorf("UseAntiAffinity = %v, want %v", got.UseAntiAffinity, tt.want.UseAntiAffinity)
				}
				if got.EnforceDistribution != tt.want.EnforceDistribution {
					t.Errorf("EnforceDistribution = %v, want %v", got.EnforceDistribution, tt.want.EnforceDistribution)
				}
				if len(got.AvailabilityZones) != len(tt.want.AvailabilityZones) {
					t.Errorf("AvailabilityZones length = %v, want %v", len(got.AvailabilityZones), len(tt.want.AvailabilityZones))
				}
			}
		})
	}
}

func TestTopologyScheduler_ApplyTopologyConstraints(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
				Placement: &neo4jv1alpha1.PlacementConfig{
					TopologySpread: &neo4jv1alpha1.TopologySpreadConfig{
						Enabled:           true,
						TopologyKey:       "topology.kubernetes.io/zone",
						MaxSkew:           1,
						WhenUnsatisfiable: "DoNotSchedule",
					},
				},
			},
		},
	}

	placement := &controller.TopologyPlacement{
		UseTopologySpread:   true,
		UseAntiAffinity:     false,
		AvailabilityZones:   []string{"zone-a", "zone-b", "zone-c"},
		EnforceDistribution: true,
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{},
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	ts := controller.NewTopologyScheduler(client)

	err := ts.ApplyTopologyConstraints(context.Background(), sts, cluster, placement)
	if err != nil {
		t.Errorf("ApplyTopologyConstraints() error = %v", err)
		return
	}

	// Check that topology spread constraints were applied
	if len(sts.Spec.Template.Spec.TopologySpreadConstraints) == 0 {
		t.Error("Expected topology spread constraints to be applied")
	}

	// Check the constraint details
	constraint := sts.Spec.Template.Spec.TopologySpreadConstraints[0]
	if constraint.TopologyKey != "topology.kubernetes.io/zone" {
		t.Errorf("TopologyKey = %v, want %v", constraint.TopologyKey, "topology.kubernetes.io/zone")
	}
	if constraint.MaxSkew != 1 {
		t.Errorf("MaxSkew = %v, want %v", constraint.MaxSkew, 1)
	}
}
