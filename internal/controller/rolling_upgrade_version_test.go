package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func newOrchestrator() *RollingUpgradeOrchestrator {
	return &RollingUpgradeOrchestrator{}
}

func TestParseVersion(t *testing.T) {
	r := newOrchestrator()

	tests := []struct {
		input string
		want  *VersionInfo
	}{
		{"5.26.1", &VersionInfo{5, 26, 1}},
		{"v5.26.1", &VersionInfo{5, 26, 1}},
		{"5.26.1-enterprise", &VersionInfo{5, 26, 1}},
		{"2025.1.0", &VersionInfo{2025, 1, 0}},
		{"5.26", &VersionInfo{5, 26, 0}},
		{"invalid", nil},
		{"5", nil},
		{"", nil},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := r.parseVersion(tt.input)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, tt.want.Major, got.Major)
				assert.Equal(t, tt.want.Minor, got.Minor)
				assert.Equal(t, tt.want.Patch, got.Patch)
			}
		})
	}
}

func TestIsCalVer(t *testing.T) {
	r := newOrchestrator()
	assert.True(t, r.isCalVer(&VersionInfo{2025, 1, 0}))
	assert.True(t, r.isCalVer(&VersionInfo{2026, 0, 0}))
	assert.False(t, r.isCalVer(&VersionInfo{5, 26, 0}))
	assert.False(t, r.isCalVer(&VersionInfo{2024, 12, 0}))
}

func TestIsSemVer(t *testing.T) {
	r := newOrchestrator()
	assert.True(t, r.isSemVer(&VersionInfo{5, 26, 0}))
	assert.True(t, r.isSemVer(&VersionInfo{4, 4, 0}))
	assert.False(t, r.isSemVer(&VersionInfo{3, 5, 0}))
	assert.False(t, r.isSemVer(&VersionInfo{2025, 1, 0}))
}

func TestIsDowngrade(t *testing.T) {
	r := newOrchestrator()

	tests := []struct {
		name     string
		current  *VersionInfo
		target   *VersionInfo
		expected bool
	}{
		{"patch upgrade", &VersionInfo{5, 26, 0}, &VersionInfo{5, 26, 1}, false},
		{"patch downgrade", &VersionInfo{5, 26, 1}, &VersionInfo{5, 26, 0}, true},
		{"minor downgrade", &VersionInfo{5, 26, 0}, &VersionInfo{5, 25, 0}, true},
		{"calver upgrade", &VersionInfo{2025, 1, 0}, &VersionInfo{2025, 2, 0}, false},
		{"calver downgrade", &VersionInfo{2025, 2, 0}, &VersionInfo{2025, 1, 0}, true},
		{"semver to calver", &VersionInfo{5, 26, 0}, &VersionInfo{2025, 1, 0}, false},
		{"calver to semver always downgrade", &VersionInfo{2025, 1, 0}, &VersionInfo{5, 26, 0}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, r.isDowngrade(tt.current, tt.target))
		})
	}
}

func TestValidateCalVerUpgrade(t *testing.T) {
	r := newOrchestrator()

	assert.NoError(t, r.validateCalVerUpgrade(
		&VersionInfo{2025, 1, 0}, &VersionInfo{2025, 1, 1}, "2025.1.0", "2025.1.1"))
	assert.NoError(t, r.validateCalVerUpgrade(
		&VersionInfo{2025, 1, 0}, &VersionInfo{2025, 2, 0}, "2025.1.0", "2025.2.0"))
	assert.NoError(t, r.validateCalVerUpgrade(
		&VersionInfo{2025, 12, 0}, &VersionInfo{2026, 1, 0}, "2025.12.0", "2026.1.0"))
}

func TestValidateSemVerUpgrade(t *testing.T) {
	r := newOrchestrator()

	t.Run("patch within 5.26.x", func(t *testing.T) {
		assert.NoError(t, r.validateSemVerUpgrade(
			&VersionInfo{5, 26, 0}, &VersionInfo{5, 26, 1}, "5.26.0", "5.26.1"))
	})

	t.Run("different minor rejected", func(t *testing.T) {
		assert.Error(t, r.validateSemVerUpgrade(
			&VersionInfo{5, 25, 0}, &VersionInfo{5, 26, 0}, "5.25.0", "5.26.0"))
	})

	t.Run("5.27 not supported", func(t *testing.T) {
		assert.Error(t, r.validateSemVerUpgrade(
			&VersionInfo{5, 27, 0}, &VersionInfo{5, 27, 1}, "5.27.0", "5.27.1"))
	})

	t.Run("4.x not supported", func(t *testing.T) {
		assert.Error(t, r.validateSemVerUpgrade(
			&VersionInfo{4, 4, 0}, &VersionInfo{4, 4, 1}, "4.4.0", "4.4.1"))
	})
}

func TestValidateSemVerToCalVerUpgrade(t *testing.T) {
	r := newOrchestrator()

	t.Run("5.26 to 2025.x allowed", func(t *testing.T) {
		assert.NoError(t, r.validateSemVerToCalVerUpgrade(
			&VersionInfo{5, 26, 0}, &VersionInfo{2025, 1, 0}, "5.26.0", "2025.1.0"))
	})

	t.Run("5.25 to 2025.x rejected", func(t *testing.T) {
		assert.Error(t, r.validateSemVerToCalVerUpgrade(
			&VersionInfo{5, 25, 0}, &VersionInfo{2025, 1, 0}, "5.25.0", "2025.1.0"))
	})

	t.Run("4.x to 2025.x rejected", func(t *testing.T) {
		assert.Error(t, r.validateSemVerToCalVerUpgrade(
			&VersionInfo{4, 4, 0}, &VersionInfo{2025, 1, 0}, "4.4.0", "2025.1.0"))
	})
}

func TestGetUpgradeTimeout(t *testing.T) {
	r := newOrchestrator()

	t.Run("nil strategy returns default", func(t *testing.T) {
		cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		assert.Equal(t, 30*time.Minute, r.getUpgradeTimeout(cluster))
	})

	t.Run("custom timeout", func(t *testing.T) {
		cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				UpgradeStrategy: &neo4jv1beta1.UpgradeStrategySpec{
					UpgradeTimeout: "1h",
				},
			},
		}
		assert.Equal(t, time.Hour, r.getUpgradeTimeout(cluster))
	})

	t.Run("invalid timeout returns default", func(t *testing.T) {
		cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				UpgradeStrategy: &neo4jv1beta1.UpgradeStrategySpec{
					UpgradeTimeout: "not-a-duration",
				},
			},
		}
		assert.Equal(t, 30*time.Minute, r.getUpgradeTimeout(cluster))
	})
}

func TestGetHealthCheckTimeout(t *testing.T) {
	r := newOrchestrator()
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	assert.Equal(t, 5*time.Minute, r.getHealthCheckTimeout(cluster))
}

func TestGetStabilizationTimeout(t *testing.T) {
	r := newOrchestrator()
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	assert.Equal(t, 3*time.Minute, r.getStabilizationTimeout(cluster))
}
