/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package resources_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/resources"
)

// TestBuildAuditConfig_Nil — must return an empty string when audit is
// not configured. The caller concatenates the output unconditionally, so
// a "" return is the no-op contract.
func TestBuildAuditConfig_Nil(t *testing.T) {
	assert.Empty(t, resources.BuildAuditConfig(nil))
}

// TestBuildAuditConfig_EnabledAlone — enabled=true with no other fields
// must emit the secure-by-default obfuscate_literals=true. That's the
// "one flag for compliance" contract: a user sets `audit.enabled: true`
// and gets PII redaction without having to know which underlying Neo4j
// key controls it.
func TestBuildAuditConfig_EnabledAlone(t *testing.T) {
	cfg := resources.BuildAuditConfig(&neo4jv1beta1.AuditSpec{Enabled: true})

	assert.Contains(t, cfg, "db.logs.query.obfuscate_literals=true",
		"enabled=true must default obfuscate_literals to true (compliance-by-default)")
	assert.NotContains(t, cfg, "dbms.security.log_successful_authentication",
		"unset LogSuccessfulAuthentication must not emit a line (Neo4j default applies)")
	assert.NotContains(t, cfg, "db.logs.query.parameter_logging_enabled",
		"unset ParameterLogging must not emit a line (Neo4j default applies)")
}

// TestBuildAuditConfig_DisabledIsNoOp — when audit.enabled=false (default
// when omitting it) AND no individual fields are set, the operator must
// not emit any audit-related config. This is the "I created a CR with
// spec.audit:{} but didn't mean to opt in" guard.
func TestBuildAuditConfig_DisabledIsNoOp(t *testing.T) {
	cfg := resources.BuildAuditConfig(&neo4jv1beta1.AuditSpec{Enabled: false})
	assert.Empty(t, cfg,
		"audit.enabled=false with no other fields must produce zero config lines")
}

// TestBuildAuditConfig_ExplicitFieldsWinOverEnabled — when a field is
// explicitly set, it MUST be emitted regardless of Enabled. In
// particular: `audit.enabled=false` + `obfuscateQueryLiterals=true`
// must still emit obfuscate_literals=true. The Enabled flag is a
// convenience for "secure defaults"; it must NOT swallow explicit
// per-field overrides.
func TestBuildAuditConfig_ExplicitFieldsWinOverEnabled(t *testing.T) {
	cfg := resources.BuildAuditConfig(&neo4jv1beta1.AuditSpec{
		Enabled:                     false,
		ObfuscateQueryLiterals:      ptr.To(true),
		LogSuccessfulAuthentication: ptr.To(false),
		ParameterLogging:            ptr.To(false),
	})

	assert.Contains(t, cfg, "db.logs.query.obfuscate_literals=true")
	assert.Contains(t, cfg, "dbms.security.log_successful_authentication=false")
	assert.Contains(t, cfg, "db.logs.query.parameter_logging_enabled=false")
}

// TestBuildAuditConfig_ExplicitObfuscateFalseDespiteEnabled — Enabled=true
// defaults obfuscate to true only when the field is NIL. If the user
// explicitly set obfuscateQueryLiterals=false (debugging or dev), that
// value MUST win — the Enabled default must not stomp on an explicit
// per-field value.
func TestBuildAuditConfig_ExplicitObfuscateFalseDespiteEnabled(t *testing.T) {
	cfg := resources.BuildAuditConfig(&neo4jv1beta1.AuditSpec{
		Enabled:                false,
		ObfuscateQueryLiterals: ptr.To(false),
	})

	assert.Contains(t, cfg, "db.logs.query.obfuscate_literals=false",
		"explicit ObfuscateQueryLiterals=false must be honored (not silently flipped to true)")

	// And same scenario with Enabled=true:
	cfg = resources.BuildAuditConfig(&neo4jv1beta1.AuditSpec{
		Enabled:                true,
		ObfuscateQueryLiterals: ptr.To(false),
	})
	assert.Contains(t, cfg, "db.logs.query.obfuscate_literals=false",
		"explicit ObfuscateQueryLiterals=false must win over enabled-based default of true")
	// Critically — the secure default must not ALSO emit; only one line per key.
	assert.Equal(t, 1, strings.Count(cfg, "db.logs.query.obfuscate_literals="),
		"exactly one obfuscate_literals line — no duplicate from both branches")
}

// TestBuildAuditConfig_PrecedenceOverMonitoring documents and pins the
// emission ORDER contract: BuildAuditConfig runs AFTER
// BuildMonitoringConfig in the rendered conf, so on the shared key
// `db.logs.query.obfuscate_literals` the audit value wins. This test
// concatenates them in the same order the cluster builder does and
// asserts the audit value appears later (last-write-wins in Neo4j conf).
func TestBuildAuditConfig_PrecedenceOverMonitoring(t *testing.T) {
	mon := &neo4jv1beta1.MonitoringSpec{
		Enabled:           true,
		ObfuscateLiterals: false, // monitoring's view: literals visible (perf debugging)
	}
	audit := &neo4jv1beta1.AuditSpec{Enabled: true} // audit's view: redact

	monitoringConf := resources.BuildMonitoringConfig(mon)
	auditConf := resources.BuildAuditConfig(audit)
	combined := monitoringConf + auditConf

	monIdx := strings.Index(combined, "db.logs.query.obfuscate_literals=false")
	auditIdx := strings.Index(combined, "db.logs.query.obfuscate_literals=true")

	if monIdx < 0 || auditIdx < 0 {
		t.Fatalf("both monitoring and audit must emit obfuscate_literals; got monitoring=%d audit=%d in:\n%s",
			monIdx, auditIdx, combined)
	}
	assert.Greater(t, auditIdx, monIdx,
		"audit emission must appear AFTER monitoring so it wins under Neo4j's last-write-wins config semantics")
}

// TestBuildAuditConfig_TrailingNewlines — ensure the emitted block ends
// with a trailing blank line so subsequent config blocks (auth, fleet)
// don't run together on the same line. Mirrors BuildMonitoringConfig
// which also ends with `\n`.
func TestBuildAuditConfig_TrailingNewlines(t *testing.T) {
	cfg := resources.BuildAuditConfig(&neo4jv1beta1.AuditSpec{Enabled: true})
	if cfg == "" {
		t.Fatal("expected non-empty output for enabled=true")
	}
	assert.True(t, strings.HasSuffix(cfg, "\n\n"),
		"BuildAuditConfig must end with double newline to visually separate from following blocks; got:\n%q", cfg)
}
