/*
Copyright 2025 Priyo Lahiri.

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

package v1beta1

import "testing"

// TestRestoreSpec_EffectiveAndNormalize verifies the v1.13 restore scope
// helpers: InstanceRef/Database are authoritative over the deprecated
// ClusterRef/DatabaseName, NormalizeSpec maps them onto the internal fields the
// controller reads, and legacy-field detection drives the deprecation warning.
func TestRestoreSpec_EffectiveAndNormalize(t *testing.T) {
	t.Run("new fields win and normalize onto legacy fields", func(t *testing.T) {
		s := &Neo4jRestoreSpec{InstanceRef: "my-neo4j", Database: "customers"}
		if got := s.EffectiveClusterRef(); got != "my-neo4j" {
			t.Errorf("EffectiveClusterRef() = %q, want my-neo4j", got)
		}
		if got := s.EffectiveDatabaseName(); got != "customers" {
			t.Errorf("EffectiveDatabaseName() = %q, want customers", got)
		}
		if s.UsesLegacyRestoreFields() {
			t.Errorf("UsesLegacyRestoreFields() = true, want false (new fields only)")
		}
		s.NormalizeSpec()
		if s.ClusterRef != "my-neo4j" || s.DatabaseName != "customers" {
			t.Errorf("after NormalizeSpec: clusterRef=%q databaseName=%q, want my-neo4j/customers", s.ClusterRef, s.DatabaseName)
		}
	})

	t.Run("legacy-only fields are detected and preserved", func(t *testing.T) {
		s := &Neo4jRestoreSpec{ClusterRef: "legacy", DatabaseName: "db"}
		if !s.UsesLegacyRestoreFields() {
			t.Errorf("UsesLegacyRestoreFields() = false, want true (legacy fields only)")
		}
		s.NormalizeSpec() // no-op
		if s.EffectiveClusterRef() != "legacy" || s.EffectiveDatabaseName() != "db" {
			t.Errorf("legacy values not preserved: clusterRef=%q databaseName=%q", s.EffectiveClusterRef(), s.EffectiveDatabaseName())
		}
	})

	t.Run("instanceRef set with legacy databaseName still flags legacy use", func(t *testing.T) {
		s := &Neo4jRestoreSpec{InstanceRef: "my-neo4j", DatabaseName: "db"}
		if !s.UsesLegacyRestoreFields() {
			t.Errorf("UsesLegacyRestoreFields() = false, want true (databaseName is legacy)")
		}
		if got := s.EffectiveDatabaseName(); got != "db" {
			t.Errorf("EffectiveDatabaseName() = %q, want db", got)
		}
	})
}
