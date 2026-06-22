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

package integration_test

import (
	"testing"
)

// TestSplitImageTag locks in the parsing contract for the helper that
// separates an OCI image reference into (repo, tag). The non-trivial bit
// is that registry hosts can themselves contain a colon (port), so a naive
// LastIndex(":") would mis-parse `registry:5000/ns/img` as repo="registry"
// + tag="5000/ns/img". The implementation guards against that by requiring
// the chosen colon to come AFTER the last slash.
func TestSplitImageTag(t *testing.T) {
	cases := []struct {
		name     string
		image    string
		wantRepo string
		wantTag  string
	}{
		{
			name:     "no tag → defaults to latest",
			image:    "mcp/neo4j",
			wantRepo: "mcp/neo4j",
			wantTag:  "latest",
		},
		{
			name:     "simple tag",
			image:    "mcp/neo4j:1.2.3",
			wantRepo: "mcp/neo4j",
			wantTag:  "1.2.3",
		},
		{
			name:     "registry host with tag",
			image:    "registry.example.com/ns/neo4j:5.26",
			wantRepo: "registry.example.com/ns/neo4j",
			wantTag:  "5.26",
		},
		{
			name:     "registry host with port AND tag",
			image:    "registry.example.com:5000/ns/neo4j:5.26",
			wantRepo: "registry.example.com:5000/ns/neo4j",
			wantTag:  "5.26",
		},
		{
			name:     "registry host with port, no tag → port colon is NOT mistaken for a tag",
			image:    "registry.example.com:5000/ns/neo4j",
			wantRepo: "registry.example.com:5000/ns/neo4j",
			wantTag:  "latest",
		},
		{
			name:     "deeply nested namespace",
			image:    "registry.example.com/ns/sub/neo4j:edge",
			wantRepo: "registry.example.com/ns/sub/neo4j",
			wantTag:  "edge",
		},
		{
			// Digests look like "@sha256:<hex>". The current parser splits on
			// the LAST colon, so for digest-style references it returns
			// repo="<image>@sha256" + tag="<hex>". This isn't a "real" tag
			// but the parser is consistent — locking it in so a future
			// refactor doesn't silently change the contract.
			name:     "digest-style reference is split at the last colon",
			image:    "mcp/neo4j@sha256:abcdef",
			wantRepo: "mcp/neo4j@sha256",
			wantTag:  "abcdef",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, tag := splitImageTag(tc.image)
			if repo != tc.wantRepo {
				t.Errorf("splitImageTag(%q) repo = %q, want %q", tc.image, repo, tc.wantRepo)
			}
			if tag != tc.wantTag {
				t.Errorf("splitImageTag(%q) tag = %q, want %q", tc.image, tag, tc.wantTag)
			}
		})
	}
}
