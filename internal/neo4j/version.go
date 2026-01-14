/*
Copyright 2025.

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

package neo4j

import (
	"fmt"
	"strconv"
	"strings"
)

// Version represents a parsed Neo4j version
type Version struct {
	Major    int
	Minor    int
	Patch    int
	IsCalver bool
	Raw      string
}

// ParseVersion parses a Neo4j version string into a structured Version
func ParseVersion(versionString string) (*Version, error) {
	if versionString == "" {
		return nil, fmt.Errorf("empty version string")
	}

	// Store raw version
	v := &Version{Raw: versionString}

	// Remove common prefixes and suffixes
	cleaned := versionString
	cleaned = strings.TrimPrefix(cleaned, "v")
	cleaned = strings.TrimPrefix(cleaned, "neo4j-")
	cleaned = strings.TrimPrefix(cleaned, "neo4j:")

	// Remove any suffix after a dash (like -enterprise, -community)
	if idx := strings.Index(cleaned, "-"); idx != -1 {
		cleaned = cleaned[:idx]
	}

	// Split by dots
	parts := strings.Split(cleaned, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid version format: %s", versionString)
	}

	// Parse major version
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid major version: %s", parts[0])
	}
	v.Major = major

	// Parse minor version
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid minor version: %s", parts[1])
	}
	v.Minor = minor

	// Parse patch version if present
	if len(parts) >= 3 {
		patch, err := strconv.Atoi(parts[2])
		if err != nil {
			// Some versions might have non-numeric patch like "5.26.0-beta1"
			// In this case, we'll use 0 as the patch
			v.Patch = 0
		} else {
			v.Patch = patch
		}
	}

	// Determine if this is a CalVer version (2025.x.x format)
	v.IsCalver = major >= 2025

	return v, nil
}

// IsSupported checks if the version meets minimum requirements
func (v *Version) IsSupported() bool {
	// CalVer versions (2025.x.x) are all supported
	if v.IsCalver {
		return true
	}

	// SemVer versions must be 5.26+
	if v.Major == 5 {
		return v.Minor >= 26
	}

	return false
}

// Compare compares two versions
// Returns -1 if v < other, 0 if v == other, 1 if v > other
func (v *Version) Compare(other *Version) int {
	// Compare major
	if v.Major < other.Major {
		return -1
	}
	if v.Major > other.Major {
		return 1
	}

	// Compare minor
	if v.Minor < other.Minor {
		return -1
	}
	if v.Minor > other.Minor {
		return 1
	}

	// Compare patch
	if v.Patch < other.Patch {
		return -1
	}
	if v.Patch > other.Patch {
		return 1
	}

	return 0
}

// String returns the version as a string
func (v *Version) String() string {
	if v.Patch > 0 {
		return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	}
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// GetImageVersion extracts the version from a Neo4j image tag
func GetImageVersion(image string) (*Version, error) {
	// Extract tag from image
	// Format: neo4j:5.26.0-enterprise or neo4j:2025.01.0-enterprise
	parts := strings.Split(image, ":")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid image format: %s", image)
	}

	tag := parts[len(parts)-1]
	return ParseVersion(tag)
}

// GetKubernetesDiscoveryParameter returns the correct discovery parameter based on version
func GetKubernetesDiscoveryParameter(version *Version) string {
	if version.IsCalver {
		// CalVer versions (2025.x) use the new parameter without v2
		return "dbms.kubernetes.discovery.service_port_name"
	}
	// SemVer versions (5.x) use the v2 parameter
	return "dbms.kubernetes.discovery.v2.service_port_name"
}

// GetBackupCommand generates the correct backup command based on version
func GetBackupCommand(version *Version, databaseName string, backupPath string, includeAllDatabases bool) string {
	// Base command is the same for both versions
	cmd := "neo4j-admin database backup"

	if includeAllDatabases {
		// Backup all databases with metadata
		cmd += " --include-metadata=all"
	} else {
		// Backup specific database
		cmd += " " + databaseName
	}

	// Add destination path with --to-path flag (Neo4j 5.26+ syntax)
	cmd += " --to-path=" + backupPath

	return cmd
}

// GetRestoreCommand generates the correct restore command based on version
func GetRestoreCommand(version *Version, databaseName string, backupPath string) string {
	// Base command is the same for both versions
	cmd := "neo4j-admin database restore"

	// Add source path
	cmd += " --from-path=" + backupPath

	// Add database name
	cmd += " " + databaseName

	return cmd
}

// SupportsMetadataOption checks if version supports --include-metadata option
func (v *Version) SupportsMetadataOption() bool {
	// All supported versions (5.26+ and 2025.x) support metadata option
	return v.IsSupported()
}

// SupportsCypherLanguageVersion checks if version supports DEFAULT LANGUAGE CYPHER
func (v *Version) SupportsCypherLanguageVersion() bool {
	// Only CalVer versions (2025.x) support Cypher language version
	return v.IsCalver
}

// SupportsAdvancedBackupFlags checks if version supports flags like --parallel-download and --skip-recovery
func (v *Version) SupportsAdvancedBackupFlags() bool {
	if !v.IsCalver {
		return false
	}
	// 2025.11+ (and any later calver) supports these flags
	if v.Major > 2025 {
		return true
	}
	return v.Major == 2025 && v.Minor >= 11
}

// SupportsSourceDatabaseFilter checks if version supports --source-database in restore
func (v *Version) SupportsSourceDatabaseFilter() bool {
	// Available from 2025.02+
	if v.IsCalver {
		return v.Minor >= 2
	}
	return false
}
