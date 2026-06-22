package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	neo4jclient "github.com/priyolahiri/neo4j-kubernetes-operator/internal/neo4j"
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
				AcceptLicenseAgreement: "eval",
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
				AcceptLicenseAgreement: "eval",
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

// TestVersionsMatch_CalVerKernelAlias pins the 2025.01 rebrand quirk: Neo4j
// 2025.01.x self-reports kernel 5.27.x via dbms.components(), so post-upgrade
// verification of a 2025.01.0-enterprise target must accept servers reporting
// 5.27.0 (found live on Kind — without the alias a successful 5.26→2025.01
// upgrade was declared Failed).
func TestVersionsMatch_CalVerKernelAlias(t *testing.T) {
	r := newOrchestrator()

	tests := []struct {
		actual, expected string
		want             bool
	}{
		{"5.27.0", "2025.01.0-enterprise", true},    // the rebrand alias
		{"2025.01.0", "2025.01.0-enterprise", true}, // direct CalVer report
		{"5.26.1", "5.26.1-enterprise", true},
		{"5.27.0", "2025.02.0-enterprise", false}, // alias only covers 2025.01
		{"5.26.1", "2025.01.0-enterprise", false}, // old version still mismatches
	}
	for _, tt := range tests {
		t.Run(tt.actual+" vs "+tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.want, r.versionsMatch(tt.actual, tt.expected))
		})
	}
}

// TestVersionMismatchesFromServers pins the SHOW SERVERS-based verification:
// one row per server (true per-server versions, unlike the old
// dbms.components() routing-driver sampling), Enabled-only filtering so stale
// Cordoned/Free entries can't wedge verification, and the CalVer report
// (2025.01.0) matching directly without the kernel alias.
func TestVersionMismatchesFromServers(t *testing.T) {
	r := newOrchestrator()
	sv := func(name, state, version string) neo4jclient.ServerInfo {
		return neo4jclient.ServerInfo{Name: name, State: state, Version: version}
	}

	t.Run("all enabled on target passes", func(t *testing.T) {
		mismatches, verified := r.versionMismatchesFromServers([]neo4jclient.ServerInfo{
			sv("s0", "Enabled", "2025.01.0"),
			sv("s1", "Enabled", "2025.01.0"),
			sv("s2", "Enabled", "2025.01.0"),
		}, "2025.01.0-enterprise")
		assert.Empty(t, mismatches)
		assert.Equal(t, 3, verified)
	})

	t.Run("kernel-alias report still accepted", func(t *testing.T) {
		mismatches, verified := r.versionMismatchesFromServers([]neo4jclient.ServerInfo{
			sv("s0", "Enabled", "5.27.0"),
		}, "2025.01.0-enterprise")
		assert.Empty(t, mismatches)
		assert.Equal(t, 1, verified)
	})

	t.Run("stale non-enabled entries are ignored", func(t *testing.T) {
		mismatches, verified := r.versionMismatchesFromServers([]neo4jclient.ServerInfo{
			sv("s0", "Enabled", "2025.01.0"),
			sv("old", "Cordoned", "5.26.0"),
			sv("free", "Free", ""),
		}, "2025.01.0-enterprise")
		assert.Empty(t, mismatches)
		assert.Equal(t, 1, verified)
	})

	t.Run("lagging server reported", func(t *testing.T) {
		mismatches, verified := r.versionMismatchesFromServers([]neo4jclient.ServerInfo{
			sv("s0", "Enabled", "2025.01.0"),
			sv("s1", "Enabled", "5.26.0"),
		}, "2025.01.0-enterprise")
		require.Len(t, mismatches, 1)
		assert.Contains(t, mismatches[0], "s1")
		assert.Contains(t, mismatches[0], "5.26.0")
		assert.Equal(t, 2, verified)
	})

	t.Run("enabled server without version is a mismatch", func(t *testing.T) {
		mismatches, _ := r.versionMismatchesFromServers([]neo4jclient.ServerInfo{
			sv("s0", "Enabled", ""),
		}, "2025.01.0-enterprise")
		require.Len(t, mismatches, 1)
		assert.Contains(t, mismatches[0], "no version reported")
	})

	t.Run("no enabled servers yields zero verified", func(t *testing.T) {
		_, verified := r.versionMismatchesFromServers([]neo4jclient.ServerInfo{
			sv("free", "Free", ""),
		}, "2025.01.0-enterprise")
		assert.Zero(t, verified)
	})
}
