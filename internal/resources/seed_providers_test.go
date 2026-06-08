/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package resources_test

import (
	"strings"
	"testing"

	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
)

// TestSeedFromURIProvidersConfigValue pins the version-gated default for
// dbms.databases.seed_from_uri_providers. Two invariants are load-bearing:
//   - S3SeedProvider is NEVER in the list (deprecated; CloudSeedProvider
//     covers s3:// via SDK default credentials, so seedCredentials becomes
//     unnecessary).
//   - ServerSeedProvider is ONLY included on Neo4j 2026.04+; pre-2026.04 the
//     class isn't registered in META-INF/services and Neo4j will warn /
//     refuse to bootstrap if we name it.
func TestSeedFromURIProvidersConfigValue(t *testing.T) {
	cases := []struct {
		imageTag             string
		wantBase             []string
		wantServerSeed       bool
		neverIncludeS3Legacy bool
	}{
		// SemVer 5.x — pre-CalVer, no ServerSeedProvider.
		{imageTag: "5.26-enterprise", wantServerSeed: false},
		{imageTag: "5.26.0-enterprise", wantServerSeed: false},

		// CalVer 2025.x — pre-2026.04, no ServerSeedProvider.
		{imageTag: "2025.01.0-enterprise", wantServerSeed: false},
		{imageTag: "2025.12-enterprise", wantServerSeed: false},
		{imageTag: "2025.12.1-enterprise", wantServerSeed: false},

		// Early 2026.x — still pre-2026.04.
		{imageTag: "2026.01.0-enterprise", wantServerSeed: false},
		{imageTag: "2026.02.0-enterprise", wantServerSeed: false},
		{imageTag: "2026.03.5-enterprise", wantServerSeed: false},

		// 2026.04 → first release with ServerSeedProvider.
		{imageTag: "2026.04.0-enterprise", wantServerSeed: true},
		{imageTag: "2026.04-enterprise", wantServerSeed: true},
		{imageTag: "2026.05.0-enterprise", wantServerSeed: true},
		{imageTag: "2026.12.0-enterprise", wantServerSeed: true},

		// Future CalVer years.
		{imageTag: "2027.01.0-enterprise", wantServerSeed: true},

		// Empty tag → safe minimum (no ServerSeedProvider).
		{imageTag: "", wantServerSeed: false},
	}

	for _, tc := range cases {
		t.Run(tc.imageTag, func(t *testing.T) {
			got := resources.SeedFromURIProvidersConfigValue(tc.imageTag)
			providers := strings.Split(got, ",")

			// Base providers ALWAYS present.
			for _, want := range []string{"CloudSeedProvider", "FileSeedProvider", "URLConnectionSeedProvider"} {
				if !contains(providers, want) {
					t.Errorf("providers = %q, missing required %q", got, want)
				}
			}

			// S3SeedProvider must NEVER appear (deprecated; would re-introduce
			// the seedCredentials requirement).
			if contains(providers, "S3SeedProvider") {
				t.Errorf("providers = %q includes deprecated S3SeedProvider — must always be excluded", got)
			}

			// ServerSeedProvider only on 2026.04+.
			hasServerSeed := contains(providers, "ServerSeedProvider")
			if hasServerSeed != tc.wantServerSeed {
				t.Errorf("providers = %q: ServerSeedProvider present = %v, want %v (tag %s)", got, hasServerSeed, tc.wantServerSeed, tc.imageTag)
			}
		})
	}
}

func TestIsNeo4jVersion202604OrHigher(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		// SemVer is never CalVer 2026+.
		{"5.26.0-enterprise", false},
		{"5.99.99-enterprise", false},
		// CalVer pre-2026.04.
		{"2025.01.0-enterprise", false},
		{"2025.12.0-enterprise", false},
		{"2026.01.0-enterprise", false},
		{"2026.02.0-enterprise", false},
		{"2026.03.0-enterprise", false},
		// 2026.04 itself + everything after.
		{"2026.04.0-enterprise", true},
		{"2026.04-enterprise", true},
		{"2026.05.0-enterprise", true},
		{"2026.12.0-enterprise", true},
		{"2027.01.0-enterprise", true},
		// Edge cases.
		{"", false},
		{"invalid", false},
	}

	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			if got := resources.IsNeo4jVersion202604OrHigher(tc.tag); got != tc.want {
				t.Errorf("IsNeo4jVersion202604OrHigher(%q) = %v, want %v", tc.tag, got, tc.want)
			}
		})
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.TrimSpace(h) == needle {
			return true
		}
	}
	return false
}
