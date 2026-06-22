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

package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildMCPEnv_NilSpec locks in the nil-safety contract on buildMCPEnv.
// Production callers (BuildMCPDeploymentForCluster / ForStandalone) early-
// return when cluster.Spec.MCP is nil, so today's reachable code never
// invokes buildMCPEnv(nil, ...). But the function takes a pointer and
// touches a dozen fields on it (Database, SchemaSampleSize, Telemetry,
// LogLevel, LogFormat, HTTP, Env), so a future caller that forgets the
// early-return — or a refactor that consolidates the two public builders —
// would panic. Calling with nil explicitly here regression-locks the
// substitute-with-zero-value guard at the top of buildMCPEnv.
func TestBuildMCPEnv_NilSpec(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("buildMCPEnv(nil, ...) panicked: %v", r)
		}
	}()

	env := buildMCPEnv(nil, "neo4j+s://cluster.example:7687", "neo4j-admin-secret", "username", "password")

	require.NotEmpty(t, env)

	// Build a name → value map for cheaper assertions.
	got := map[string]string{}
	for _, e := range env {
		got[e.Name] = e.Value
	}

	assert.Equal(t, "neo4j+s://cluster.example:7687", got["NEO4J_URI"])
	// Zero-value spec → ReadOnly defaults to false.
	assert.Equal(t, "false", got["NEO4J_READ_ONLY"])
	// mcpTransport(nil) → "http" (preserved by the zero-value substitution).
	assert.Equal(t, "http", got["NEO4J_TRANSPORT_MODE"])

	// Optional fields on MCPServerSpec are all absent in the env block when
	// spec is nil → zero-value: the `if spec.X != ""` / `!= nil` guards
	// throughout buildMCPEnv must filter them out cleanly.
	for _, optional := range []string{
		"NEO4J_DATABASE",
		"NEO4J_SCHEMA_SAMPLE_SIZE",
		"NEO4J_TELEMETRY",
		"NEO4J_LOG_LEVEL",
		"NEO4J_LOG_FORMAT",
		"NEO4J_MCP_HTTP_TLS_ENABLED",
		"NEO4J_MCP_HTTP_TLS_CERT_FILE",
		"NEO4J_MCP_HTTP_TLS_KEY_FILE",
		"NEO4J_AUTH_HEADER_NAME",
	} {
		_, present := got[optional]
		assert.False(t, present, "%s must not be set when spec is nil", optional)
	}
}
