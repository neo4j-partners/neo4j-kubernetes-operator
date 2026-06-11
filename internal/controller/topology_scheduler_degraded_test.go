/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller_test

// Tests for #202: in namespace-scoped installs the operator holds only a
// namespaced Role and cannot list cluster-scoped nodes, so availability-zone
// auto-discovery fails. That must NOT leave a cluster stuck `Failed` on the
// core install path — the scheduler degrades to best-effort zone spread
// instead, except when enforceDistribution makes a hard guarantee it can't
// keep blind.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/controller"
)

// nodeListForbiddenClient returns a client that denies List on nodes (as a
// namespaced Role would) but behaves normally otherwise.
func nodeListForbiddenClient(t *testing.T) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	return interceptor.NewClient(base, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*corev1.NodeList); ok {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "nodes"}, "",
					errors.New("namespaced Role cannot list cluster-scoped nodes"))
			}
			return c.List(ctx, list, opts...)
		},
	})
}

func clusterWithZoneSpread(servers int32, enforce bool) *neo4jv1beta1.Neo4jEnterpriseCluster {
	return &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers:             servers,
				EnforceDistribution: enforce,
				Placement: &neo4jv1beta1.PlacementConfig{
					TopologySpread: &neo4jv1beta1.TopologySpreadConfig{
						Enabled:           true,
						TopologyKey:       "topology.kubernetes.io/zone",
						MaxSkew:           1,
						WhenUnsatisfiable: "DoNotSchedule",
					},
				},
			},
		},
	}
}

func TestCalculateTopologyPlacement_NodeListDeniedDegradesGracefully(t *testing.T) {
	ts := controller.NewTopologyScheduler(nodeListForbiddenClient(t))

	placement, err := ts.CalculateTopologyPlacement(context.Background(), clusterWithZoneSpread(3, false))
	require.NoError(t, err, "node-list denial must NOT fail the reconcile when enforceDistribution is off")
	require.NotNil(t, placement)
	assert.True(t, placement.ZoneDiscoveryDegraded, "degradation flag must be set so the controller can warn")
	assert.True(t, placement.UseTopologySpread, "topology spread still applies via the zone label key")
	assert.Empty(t, placement.AvailabilityZones, "AZ enumeration is skipped under degradation")
}

func TestCalculateTopologyPlacement_NodeListDeniedWithEnforceDistributionErrors(t *testing.T) {
	ts := controller.NewTopologyScheduler(nodeListForbiddenClient(t))

	_, err := ts.CalculateTopologyPlacement(context.Background(), clusterWithZoneSpread(3, true))
	require.Error(t, err, "enforceDistribution can't be honored without zone visibility — must fail, not silently degrade")
	assert.Contains(t, err.Error(), "enforceDistribution")
	assert.Contains(t, err.Error(), "availabilityZones", "error must point at the explicit-AZ workaround")
}

func TestCalculateTopologyPlacement_NodeListDeniedNoConstraintsNoDegrade(t *testing.T) {
	// spec.topology.placement is set but both topology-spread and anti-affinity
	// are disabled, so no zone constraints are produced. A failed node list is
	// then inconsequential — we must NOT flag degradation or warn (#205 review).
	ts := controller.NewTopologyScheduler(nodeListForbiddenClient(t))
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers:   3,
				Placement: &neo4jv1beta1.PlacementConfig{}, // both spread + anti-affinity off
			},
		},
	}

	placement, err := ts.CalculateTopologyPlacement(context.Background(), cluster)
	require.NoError(t, err)
	assert.False(t, placement.UseTopologySpread)
	assert.False(t, placement.UseAntiAffinity)
	assert.False(t, placement.ZoneDiscoveryDegraded, "no zone constraints applied -> no misleading degrade/warn")
}

func TestCalculateTopologyPlacement_ExplicitZonesSkipNodeList(t *testing.T) {
	// With AZs listed explicitly, the scheduler never lists nodes, so even a
	// node-denying client succeeds and there's no degradation.
	ts := controller.NewTopologyScheduler(nodeListForbiddenClient(t))
	cluster := clusterWithZoneSpread(3, true)
	cluster.Spec.Topology.AvailabilityZones = []string{"zone-a", "zone-b", "zone-c"}

	placement, err := ts.CalculateTopologyPlacement(context.Background(), cluster)
	require.NoError(t, err)
	assert.False(t, placement.ZoneDiscoveryDegraded)
	assert.Equal(t, []string{"zone-a", "zone-b", "zone-c"}, placement.AvailabilityZones)
}
