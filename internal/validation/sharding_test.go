/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package validation

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestIsClusterShardingReady(t *testing.T) {
	cases := []struct {
		name         string
		cluster      *neo4jv1beta1.Neo4jEnterpriseCluster
		wantErr      bool
		wantContains string
	}{
		{
			name:         "nil cluster",
			cluster:      nil,
			wantErr:      true,
			wantContains: "nil",
		},
		{
			name: "propertySharding spec absent",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{Tag: "2025.12.0-enterprise"},
				},
			},
			wantErr:      true,
			wantContains: "property sharding enabled",
		},
		{
			name: "propertySharding disabled",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image:            neo4jv1beta1.ImageSpec{Tag: "2025.12.0-enterprise"},
					PropertySharding: &neo4jv1beta1.PropertyShardingSpec{Enabled: false},
				},
			},
			wantErr:      true,
			wantContains: "property sharding enabled",
		},
		{
			name: "sharding enabled but version too old (2025.11)",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image:            neo4jv1beta1.ImageSpec{Tag: "2025.11.0-enterprise"},
					PropertySharding: &neo4jv1beta1.PropertyShardingSpec{Enabled: true},
				},
			},
			wantErr:      true,
			wantContains: "below the 2025.12 minimum",
		},
		{
			name: "sharding enabled but version is semver (5.26)",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image:            neo4jv1beta1.ImageSpec{Tag: "5.26.0-enterprise"},
					PropertySharding: &neo4jv1beta1.PropertyShardingSpec{Enabled: true},
				},
			},
			wantErr:      true,
			wantContains: "below the 2025.12 minimum",
		},
		{
			name: "valid: sharding enabled, 2025.12",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image:            neo4jv1beta1.ImageSpec{Tag: "2025.12.0-enterprise"},
					PropertySharding: &neo4jv1beta1.PropertyShardingSpec{Enabled: true},
				},
			},
			wantErr: false,
		},
		{
			name: "valid: sharding enabled, 2026.05",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image:            neo4jv1beta1.ImageSpec{Tag: "2026.05.0-enterprise"},
					PropertySharding: &neo4jv1beta1.PropertyShardingSpec{Enabled: true},
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := IsClusterShardingReady(tc.cluster)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr && tc.wantContains != "" && !strings.Contains(err.Error(), tc.wantContains) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantContains)
			}
		})
	}
}
