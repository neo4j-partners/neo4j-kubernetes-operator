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

package validation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestConfigValidator_ValidateDiscoveryRestrictions(t *testing.T) {
	validator := NewConfigValidator()

	tests := []struct {
		name         string
		config       map[string]string
		expectErrors bool
		errorType    string
	}{
		{
			name: "valid configuration without discovery settings",
			config: map[string]string{
				"db.logs.query.enabled":    "INFO",
				"dbms.transaction.timeout": "60s",
				"metrics.enabled":          "true",
			},
			expectErrors: false,
		},
		{
			name: "forbidden resolver_type configuration",
			config: map[string]string{
				"dbms.cluster.discovery.resolver_type": "dns",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "forbidden static endpoints configuration",
			config: map[string]string{
				"dbms.cluster.discovery.v2.endpoints": "server1:6000,server2:6000",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "forbidden legacy endpoints configuration",
			config: map[string]string{
				"dbms.cluster.endpoints": "server1:6000,server2:6000",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "forbidden manual kubernetes label selector",
			config: map[string]string{
				"dbms.kubernetes.label_selector": "app=my-app",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "forbidden manual kubernetes service port",
			config: map[string]string{
				"dbms.kubernetes.discovery.service_port_name": "custom-port",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "multiple forbidden discovery settings",
			config: map[string]string{
				"dbms.cluster.discovery.resolver_type": "list",
				"dbms.cluster.discovery.v2.endpoints":  "server1:6000",
				"dbms.kubernetes.label_selector":       "app=test",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "deprecated discovery version setting - invalid value",
			config: map[string]string{
				"dbms.cluster.discovery.version": "V1_ONLY",
			},
			expectErrors: true,
			errorType:    "Unsupported value", // This generates both Invalid (deprecated) and Unsupported (invalid value)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Config: tt.config,
				},
			}

			errors := validator.Validate(cluster)
			hasErrors := len(errors) > 0
			assert.Equal(t, tt.expectErrors, hasErrors, "Error expectation mismatch: %v", errors)

			if tt.expectErrors && tt.errorType != "" {
				found := false
				errorTypes := []string{}
				for _, err := range errors {
					errorTypes = append(errorTypes, err.Type.String())
					if err.Type.String() == tt.errorType {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error type %s not found. Available types: %v. Errors: %v", tt.errorType, errorTypes, errors)
			}
		})
	}
}

// TestConfigValidator_RejectsManagedSSLKeys covers the rejection of any
// user-supplied dbms.ssl.policy.* / server.bolt.tls_level /
// server.directories.certificates in spec.config. The operator owns the
// SSL surface end-to-end via spec.tls.* and the new
// spec.tls.strictPeerValidation toggle. Without this rejection, a user
// could put e.g. dbms.ssl.policy.cluster.trust_all=true in spec.config
// and — because the operator de-duplicates the rendered conf (last
// occurrence wins, collapsing to one line invisible to strict validation) —
// silently downgrade the strict-by-default cluster TLS posture.
func TestConfigValidator_RejectsManagedSSLKeys(t *testing.T) {
	validator := NewConfigValidator()

	cases := []struct {
		name   string
		key    string
		value  string
		reason string
	}{
		{
			name:   "cluster SSL trust_all override is rejected",
			key:    "dbms.ssl.policy.cluster.trust_all",
			value:  "true",
			reason: "would silently revert strict peer validation to legacy posture",
		},
		{
			name:   "cluster SSL client_auth override is rejected",
			key:    "dbms.ssl.policy.cluster.client_auth",
			value:  "NONE",
			reason: "would disable mutual TLS",
		},
		{
			name:   "cluster SSL verify_hostname override is rejected",
			key:    "dbms.ssl.policy.cluster.verify_hostname",
			value:  "false",
			reason: "would disable hostname verification",
		},
		{
			name:   "bolt SSL policy override is rejected",
			key:    "dbms.ssl.policy.bolt.client_auth",
			value:  "REQUIRE",
			reason: "would force every Bolt driver to present a client cert",
		},
		{
			name:   "https SSL policy override is rejected",
			key:    "dbms.ssl.policy.https.enabled",
			value:  "false",
			reason: "would silently disable HTTPS even with TLS enabled",
		},
		{
			name:   "bolt TLS level override is rejected",
			key:    "server.bolt.tls_level",
			value:  "OPTIONAL",
			reason: "would downgrade Bolt TLS enforcement away from REQUIRED",
		},
		{
			name:   "certificates directory override is rejected",
			key:    "server.directories.certificates",
			value:  "/etc/neo4j/certs",
			reason: "would point Neo4j away from the operator-managed /ssl/ mount",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Config: map[string]string{tc.key: tc.value},
				},
			}
			errors := validator.Validate(cluster)
			require.NotEmpty(t, errors, "expected rejection (%s)", tc.reason)
			found := false
			for _, err := range errors {
				if err.Type == field.ErrorTypeForbidden {
					found = true
					break
				}
			}
			assert.True(t, found, "expected a Forbidden error type — got %v", errors)
		})
	}
}

func TestConfigValidator_RejectsRuntimeManagedKeys(t *testing.T) {
	validator := NewConfigValidator()

	// Keys the operator writes into neo4j.conf at pod startup (per-pod FQDN
	// advertised addresses, topology). A user value collides at runtime →
	// "declared multiple times" on CalVer, which the static-conf de-dup can't
	// catch — so reject at apply time.
	keys := []string{
		"server.default_advertised_address",
		"server.cluster.advertised_address",
		"server.routing.advertised_address",
		"server.cluster.raft.advertised_address",
		"initial.server.mode_constraint",
		"dbms.cluster.minimum_initial_system_primaries_count",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Config: map[string]string{key: "some-value"},
				},
			}
			errors := validator.Validate(cluster)
			found := false
			for _, err := range errors {
				if err.Type == field.ErrorTypeForbidden && err.Field == "spec.config."+key {
					found = true
					break
				}
			}
			assert.True(t, found, "expected %s to be Forbidden — got %v", key, errors)
		})
	}
}

func TestConfigValidator_DeprecatedKeys(t *testing.T) {
	validator := NewConfigValidator()

	tests := []struct {
		name         string
		config       map[string]string
		expectErrors bool
	}{
		{
			name: "deprecated dbms.logs.query.enabled should warn",
			config: map[string]string{
				"dbms.logs.query.enabled": "INFO",
			},
			expectErrors: true,
		},
		{
			name: "correct db.logs.query.enabled should not warn",
			config: map[string]string{
				"db.logs.query.enabled": "INFO",
			},
			expectErrors: false,
		},
		{
			name: "deprecated dbms.default_database should warn",
			config: map[string]string{
				"dbms.default_database": "mydb",
			},
			expectErrors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Config: tt.config,
				},
			}

			errors := validator.Validate(cluster)
			hasErrors := len(errors) > 0
			assert.Equal(t, tt.expectErrors, hasErrors, "Deprecated key expectation mismatch for config %v: %v", tt.config, errors)
		})
	}
}

// TestConfigValidator_RejectsDbFormat pins that db.format is rejected for ANY
// value — including "block" — because the operator emits db.format=block itself
// and runs with strict_validation=true, so a user-set db.format is a duplicate
// key that fails Neo4j startup. The rejection message must describe the key as
// operator-managed, not tell the user to "use block" (which the old check did
// even when the user had already passed block).
func TestConfigValidator_RejectsDbFormat(t *testing.T) {
	validator := NewConfigValidator()

	for _, value := range []string{"block", "high_limit", "standard", "aligned"} {
		t.Run("db.format="+value, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Config: map[string]string{"db.format": value},
				},
			}

			errs := validator.Validate(cluster)

			var dbFormatErr *field.Error
			for _, e := range errs {
				if e.Field == "spec.config.db.format" {
					dbFormatErr = e
					break
				}
			}
			if dbFormatErr == nil {
				t.Fatalf("expected db.format=%q to be rejected, got errors: %v", value, errs)
			}
			// Exactly one error for the key (the old code double-reported
			// standard/high_limit via both the deprecated map and a dedicated
			// check).
			count := 0
			for _, e := range errs {
				if e.Field == "spec.config.db.format" {
					count++
				}
			}
			assert.Equal(t, 1, count, "expected exactly one db.format error, got %d: %v", count, errs)
			assert.Equal(t, field.ErrorTypeForbidden, dbFormatErr.Type)
			assert.Contains(t, dbFormatErr.Detail, "managed by the operator")
			assert.NotContains(t, dbFormatErr.Detail, "use block", "message must not tell the user to set block")
		})
	}
}

func TestConfigValidator_ValidDiscoveryVersion(t *testing.T) {
	validator := NewConfigValidator()

	tests := []struct {
		name         string
		version      string
		expectErrors bool
	}{
		{
			name:         "valid V2_ONLY version for Neo4j 5.x",
			version:      "V2_ONLY",
			expectErrors: false, // V2_ONLY is required for Neo4j 5.26+
		},
		{
			name:         "invalid V1_ONLY version",
			version:      "V1_ONLY",
			expectErrors: true,
		},
		{
			name:         "invalid empty version",
			version:      "",
			expectErrors: true,
		},
		{
			name:         "invalid random version",
			version:      "INVALID",
			expectErrors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Config: map[string]string{
						"dbms.cluster.discovery.version": tt.version,
					},
				},
			}

			errors := validator.Validate(cluster)
			hasErrors := len(errors) > 0
			assert.Equal(t, tt.expectErrors, hasErrors, "Error expectation mismatch for version %s: %v", tt.version, errors)
		})
	}
}

func TestConfigValueHasControlChars(t *testing.T) {
	for _, s := range []string{"OFF\ndbms.security.auth_enabled=false", "x\r\ny", "line\nbreak"} {
		if !ConfigValueHasControlChars(s) {
			t.Errorf("expected %q to be flagged", s)
		}
	}
	for _, s := range []string{"block", "true", "gds.*", "1.5", ""} {
		if ConfigValueHasControlChars(s) {
			t.Errorf("expected %q to be clean", s)
		}
	}
}

func TestConfigValidator_RejectsNewlineInValue(t *testing.T) {
	v := NewConfigValidator()
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Config: map[string]string{
				"db.logs.query.threshold": "0\ndbms.security.auth_enabled=false",
			},
		},
	}
	errs := v.Validate(cluster)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "newline or carriage-return") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected newline rejection, got: %v", errs)
	}
}
