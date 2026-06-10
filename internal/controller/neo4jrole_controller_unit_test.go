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

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCanonicaliseDesired_MapsCanonicalToOriginal pins audit finding #3: the
// add loop must execute the user's ORIGINAL statement text, not the canonical
// form. The canonical form upper-cases bare tokens — including unquoted
// identifiers — so a role named `users` written unquoted canonicalises to
// "... TO USERS" and would target the wrong (case-sensitive) role if executed.
// canonicaliseDesired must therefore return the original text keyed by canon.
func TestCanonicaliseDesired_MapsCanonicalToOriginal(t *testing.T) {
	r := &Neo4jRoleReconciler{}

	t.Run("keyword-like identifier preserves original case", func(t *testing.T) {
		orig := "GRANT ACCESS ON DATABASE neo4j TO users"
		canon, byCanonical, err := r.canonicaliseDesired([]string{orig})
		assert.NoError(t, err)
		assert.Len(t, canon, 1)
		// Canonical upper-cases the bare role name (it is a reserved keyword).
		assert.Equal(t, "GRANT ACCESS ON DATABASE neo4j TO USERS", canon[0])
		// But the map gives back the original text we must actually execute.
		assert.Equal(t, orig, byCanonical[canon[0]],
			"add loop must run the original statement, not the upper-cased canonical")
	})

	t.Run("dedup keeps first original and sorts canon", func(t *testing.T) {
		canon, byCanonical, err := r.canonicaliseDesired([]string{
			"GRANT ACCESS ON DATABASE neo4j TO r",
			"grant   access  on database neo4j to r", // same canonical, different text
			"GRANT MATCH {*} ON GRAPH neo4j NODES * TO r",
		})
		assert.NoError(t, err)
		assert.Len(t, canon, 2, "two distinct canonical statements")
		// First original wins for the duplicated canonical form.
		assert.Equal(t, "GRANT ACCESS ON DATABASE neo4j TO r",
			byCanonical["GRANT ACCESS ON DATABASE neo4j TO r"])
	})

	t.Run("empty and whitespace-only statements are skipped", func(t *testing.T) {
		canon, byCanonical, err := r.canonicaliseDesired([]string{"", "   ", " ; "})
		assert.NoError(t, err)
		assert.Empty(t, canon)
		assert.Empty(t, byCanonical)
	})
}
