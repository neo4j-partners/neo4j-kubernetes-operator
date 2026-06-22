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

package validation

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// ---------------------------------------------------------------------------
// TestValidateVersionUpgrade
// ---------------------------------------------------------------------------

func TestValidateVersionUpgrade(t *testing.T) {
	v := NewUpgradeValidator()

	cases := []struct {
		name     string
		current  string
		target   string
		wantErrs int
	}{
		// Valid upgrades
		{"SemVer patch upgrade", "5.26.0-enterprise", "5.26.3-enterprise", 0},
		{"SemVer minor upgrade rejected (5.27 does not exist)", "5.26.0-enterprise", "5.27.0-enterprise", 1},
		{"CalVer minor upgrade", "2025.01.0-enterprise", "2025.02.0-enterprise", 0},
		{"CalVer year upgrade", "2025.01.0-enterprise", "2026.01.0-enterprise", 0},
		{"SemVer to CalVer migration", "5.26.0-enterprise", "2025.01.0-enterprise", 0},

		// Invalid upgrades
		{"CalVer to SemVer downgrade rejected", "2025.01.0-enterprise", "5.26.0-enterprise", 1},
		{"CalVer downgrade", "2025.02.0-enterprise", "2025.01.0-enterprise", 1},
		{"CalVer to SemVer downgrade always rejected", "2025.01.0-enterprise", "5.26.0-enterprise", 1},
		{"invalid version format", "5.26.0-enterprise", "not-a-version", 1},
		{"Neo4j 4.x not supported", "4.4.0-enterprise", "4.4.9-enterprise", 1},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			errs := v.ValidateVersionUpgrade(tc.current, tc.target)
			if len(errs) != tc.wantErrs {
				t.Errorf("ValidateVersionUpgrade(%q, %q): expected %d errors, got %d: %v",
					tc.current, tc.target, tc.wantErrs, len(errs), errs)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestValidateUpgradeStrategy
// ---------------------------------------------------------------------------

func TestValidateUpgradeStrategy(t *testing.T) {
	v := NewUpgradeValidator()

	negOne := int32(-1)
	validMaxUnavail := int32(1)

	cases := []struct {
		name     string
		strategy *neo4jv1beta1.UpgradeStrategySpec
		wantErrs int
	}{
		{
			name:     "nil strategy - no errors",
			strategy: nil, wantErrs: 0,
		},
		{
			name: "valid RollingUpgrade strategy",
			strategy: &neo4jv1beta1.UpgradeStrategySpec{
				Strategy:                    "RollingUpgrade",
				UpgradeTimeout:              "30m",
				HealthCheckTimeout:          "5m",
				StabilizationTimeout:        "3m",
				MaxUnavailableDuringUpgrade: &validMaxUnavail,
			},
			wantErrs: 0,
		},
		{
			name:     "valid Recreate strategy",
			strategy: &neo4jv1beta1.UpgradeStrategySpec{Strategy: "Recreate"}, wantErrs: 0,
		},
		{
			name:     "unknown strategy Blue-Green",
			strategy: &neo4jv1beta1.UpgradeStrategySpec{Strategy: "Blue-Green"}, wantErrs: 1,
		},
		{
			name:     "invalid upgradeTimeout",
			strategy: &neo4jv1beta1.UpgradeStrategySpec{UpgradeTimeout: "not-a-duration"}, wantErrs: 1,
		},
		{
			name:     "invalid healthCheckTimeout",
			strategy: &neo4jv1beta1.UpgradeStrategySpec{HealthCheckTimeout: "not-a-duration"}, wantErrs: 1,
		},
		{
			name:     "invalid stabilizationTimeout",
			strategy: &neo4jv1beta1.UpgradeStrategySpec{StabilizationTimeout: "not-a-duration"}, wantErrs: 1,
		},
		{
			name: "maxUnavailableDuringUpgrade = -1",
			strategy: &neo4jv1beta1.UpgradeStrategySpec{
				MaxUnavailableDuringUpgrade: &negOne,
			},
			wantErrs: 1,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					UpgradeStrategy:        tc.strategy,
				},
			}
			errs := v.ValidateUpgradeStrategy(cluster)
			if len(errs) != tc.wantErrs {
				t.Errorf("ValidateUpgradeStrategy(%q): expected %d errors, got %d: %v",
					tc.name, tc.wantErrs, len(errs), errs)
			}
		})
	}
}
