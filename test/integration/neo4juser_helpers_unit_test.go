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

package integration_test

import (
	"testing"
)

// TestCypherShellLastValueIsZero locks in the parsing contract for
// cypher-shell `--format plain` output. The helper is consumed by an
// Eventually loop, so its truth table — especially how it treats
// transient-blip / empty / whitespace-only output — directly determines
// whether a "DROP USER must remove appuser" assertion converges or hangs.
func TestCypherShellLastValueIsZero(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		// --format plain emits "n" header then the value on the next line.
		{"plain count zero", "n\n0", true},
		{"plain count one", "n\n1", false},
		{"plain count larger", "n\n42", false},

		// Real cypher-shell output sometimes has a trailing newline.
		{"trailing newline preserved", "n\n0\n", true},
		{"trailing whitespace", "n\n0   \n  ", true},

		// Single-line output (e.g. when --format plain emits just the value
		// on some Neo4j versions, or when only the count was returned).
		{"single line zero", "0", true},
		{"single line non-zero", "1", false},
		{"single line zero with trailing newline", "0\n", true},

		// Defensive: cypher-shell can in principle return nothing during a
		// transient connection blip. The Eventually loop should retry, so
		// "not zero" is the correct answer (false → keep polling).
		{"empty input", "", false},
		{"whitespace-only input", "   \n\t\n  ", false},
		{"single newline", "\n", false},

		// Multi-line with extra body — the parser only cares about the LAST
		// non-empty line, mirroring how plain format places the value last.
		{"multi-line with header noise then zero", "Some header line\nn\n0", true},
		{"multi-line with header noise then one", "Some header line\nn\n1", false},

		// "0" must match exactly — not as a substring of a longer number.
		{"value 10 is not zero", "n\n10", false},
		{"value -0 is not literal '0'", "n\n-0", false},

		// Tab and other whitespace around the count.
		{"tab-separated whitespace around zero", "n\n\t0\t", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cypherShellLastValueIsZero([]byte(tc.in))
			if got != tc.want {
				t.Errorf("cypherShellLastValueIsZero(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
