package neo4j

import "testing"

func TestSupportsAdvancedBackupFlags(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"2025.10.0", false},
		{"2025.11.0", true},
		{"2026.1.0", true},
		{"5.26.0", false},
	}

	for _, tt := range tests {
		v, err := ParseVersion(tt.version)
		if err != nil {
			t.Fatalf("failed to parse version %s: %v", tt.version, err)
		}
		if got := v.SupportsAdvancedBackupFlags(); got != tt.want {
			t.Errorf("SupportsAdvancedBackupFlags(%s) = %v, want %v", tt.version, got, tt.want)
		}
	}
}
