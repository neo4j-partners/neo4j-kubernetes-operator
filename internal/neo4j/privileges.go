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
	"regexp"
	"strings"
)

// CanonicalisePrivilegeStatement normalises a Cypher GRANT/DENY/REVOKE
// statement so that two semantically-equivalent statements compare equal.
//
// The canonicalisation is intentionally conservative — it does not parse
// Cypher. It performs textual normalisation that is safe for the dialect
// of statements emitted by `SHOW ... PRIVILEGES AS COMMANDS` and the
// statements users typically write in Neo4jRole.spec.privileges:
//
//   - collapses runs of ASCII whitespace to a single space
//   - trims leading and trailing whitespace and any single trailing semicolon
//   - upper-cases reserved keywords (GRANT/DENY/REVOKE/ON/TO/FROM/...)
//     while preserving identifiers, quoted strings and braces
//
// The result is suitable for use as a map key when diffing desired vs.
// actual privileges. It is NOT safe to feed the canonical form back to
// Neo4j — always execute the original statement text.
func CanonicalisePrivilegeStatement(stmt string) string {
	s := strings.TrimSpace(stmt)
	s = strings.TrimSuffix(s, ";")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	// Collapse whitespace runs (but only outside of single/double-quoted strings)
	s = collapseWhitespacePreservingQuotes(s)

	// Upper-case reserved keywords while preserving everything else.
	s = upperCaseReservedKeywords(s)

	return s
}

// privilegeKeywords is the set of tokens that should be upper-cased when
// found as standalone words in a privilege statement. The list is closed
// rather than open so we never accidentally mutate identifiers that happen
// to spell a keyword (e.g. a role literally named "graph").
var privilegeKeywords = map[string]struct{}{
	"GRANT":         {},
	"DENY":          {},
	"REVOKE":        {},
	"IMMUTABLE":     {},
	"ON":            {},
	"TO":            {},
	"FROM":          {},
	"DATABASE":      {},
	"DATABASES":     {},
	"GRAPH":         {},
	"GRAPHS":        {},
	"DBMS":          {},
	"HOME":          {},
	"DEFAULT":       {},
	"NODES":         {},
	"NODE":          {},
	"RELATIONSHIPS": {},
	"RELATIONSHIP":  {},
	"ELEMENTS":      {},
	"ELEMENT":       {},
	"FOR":           {},
	"ALL":           {},
	"ACCESS":        {},
	"READ":          {},
	"MATCH":         {},
	"TRAVERSE":      {},
	"WRITE":         {},
	"CREATE":        {},
	"DELETE":        {},
	"DROP":          {},
	"ALTER":         {},
	"SET":           {},
	"REMOVE":        {},
	"LOAD":          {},
	"INDEX":         {},
	"CONSTRAINT":    {},
	"PRIVILEGE":     {},
	"PRIVILEGES":    {},
	"ROLE":          {},
	"ROLES":         {},
	"USER":          {},
	"USERS":         {},
	"NAME":          {},
	"LABEL":         {},
	"PROPERTY":      {},
	"EXECUTE":       {},
	"PROCEDURE":     {},
	"PROCEDURES":    {},
	"FUNCTION":      {},
	"FUNCTIONS":     {},
	"BOOSTED":       {},
	"MANAGEMENT":    {},
	"TRANSACTION":   {},
	"SHOW":          {},
	"START":         {},
	"STOP":          {},
	"TERMINATE":     {},
	"ASSIGN":        {},
	"IMPERSONATE":   {},
	"AUTH":          {},
	"SERVER":        {},
	"COMPOSITE":     {},
	"ALIAS":         {},
	"ALIASES":       {},
	"OF":            {},
	"AS":            {},
	"ANY":           {},
	"AWAIT":         {},
	"WAIT":          {},
	// Property-based access control (PBAC) WHERE-clause keywords. These appear
	// in `GRANT/DENY MATCH/READ/TRAVERSE … FOR pattern WHERE … TO role` and
	// must be upper-cased so spec strings round-trip equal against the output
	// of `SHOW ROLE PRIVILEGES AS COMMANDS`.
	"WHERE": {},
	"IS":    {},
	"NULL":  {},
	"NOT":   {},
	"IN":    {},
	"AND":   {},
	"OR":    {},
}

var (
	tokenSplit = regexp.MustCompile(`\s+`)
)

// collapseWhitespacePreservingQuotes collapses whitespace runs to a single
// space, but does not modify whitespace inside single- or double-quoted
// substrings (which would otherwise be folded incorrectly for things like
// `'multi  word' literals`). Backslash escapes inside quotes are honoured.
func collapseWhitespacePreservingQuotes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSingle, inDouble, inBacktick, escape := false, false, false, false
	prevWasSpace := false
	for _, r := range s {
		if escape {
			b.WriteRune(r)
			escape = false
			prevWasSpace = false
			continue
		}
		switch r {
		case '\\':
			if inSingle || inDouble {
				escape = true
			}
			b.WriteRune(r)
			prevWasSpace = false
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
			b.WriteRune(r)
			prevWasSpace = false
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
			b.WriteRune(r)
			prevWasSpace = false
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
			b.WriteRune(r)
			prevWasSpace = false
		default:
			if !inSingle && !inDouble && !inBacktick && (r == ' ' || r == '\t' || r == '\n' || r == '\r') {
				if !prevWasSpace {
					b.WriteRune(' ')
					prevWasSpace = true
				}
				continue
			}
			b.WriteRune(r)
			prevWasSpace = false
		}
	}
	return b.String()
}

// upperCaseReservedKeywords walks tokens (delimited by ASCII whitespace and
// punctuation) and upper-cases any that match privilegeKeywords. Quoted
// strings and backtick-delimited identifiers are passed through unchanged.
func upperCaseReservedKeywords(s string) string {
	// Fast path: split on whitespace, walk each segment, upper-case if a
	// "bare" keyword. Punctuation like '{', '}', '(', ')', ',' is treated
	// as a terminator and preserved.
	var out strings.Builder
	out.Grow(len(s))

	inSingle, inDouble, inBacktick, escape := false, false, false, false
	var token strings.Builder
	flush := func() {
		t := token.String()
		token.Reset()
		if t == "" {
			return
		}
		if _, ok := privilegeKeywords[strings.ToUpper(t)]; ok {
			// Only mutate if it does not contain a quote character (already
			// handled below — bare tokens cannot contain quotes by
			// construction).
			out.WriteString(strings.ToUpper(t))
			return
		}
		out.WriteString(t)
	}

	for _, r := range s {
		if escape {
			token.WriteRune(r)
			escape = false
			continue
		}
		if inSingle {
			token.WriteRune(r)
			if r == '\\' {
				escape = true
				continue
			}
			if r == '\'' {
				flush()
				inSingle = false
			}
			continue
		}
		if inDouble {
			token.WriteRune(r)
			if r == '\\' {
				escape = true
				continue
			}
			if r == '"' {
				flush()
				inDouble = false
			}
			continue
		}
		if inBacktick {
			token.WriteRune(r)
			if r == '`' {
				flush()
				inBacktick = false
			}
			continue
		}
		switch r {
		case '\'':
			flush()
			inSingle = true
			token.WriteRune(r)
		case '"':
			flush()
			inDouble = true
			token.WriteRune(r)
		case '`':
			flush()
			inBacktick = true
			token.WriteRune(r)
		case ' ', '\t', '\n', '\r':
			flush()
			out.WriteRune(' ')
		case '{', '}', '(', ')', ',', '*', '/', ':', ';':
			flush()
			out.WriteRune(r)
		default:
			token.WriteRune(r)
		}
	}
	flush()
	return out.String()
}

// DerivePrivilegeRevoke turns a GRANT or DENY statement into the matching
// REVOKE statement that, when executed, removes that exact assignment. It
// is a textual transform: the input must already be canonical (run
// CanonicalisePrivilegeStatement first if unsure).
//
// The transform is:
//
//	GRANT  [IMMUTABLE] <body> TO <role>   →   REVOKE GRANT [IMMUTABLE] <body> FROM <role>
//	DENY   [IMMUTABLE] <body> TO <role>   →   REVOKE DENY  [IMMUTABLE] <body> FROM <role>
//
// Returns an error if the statement does not start with GRANT/DENY or
// does not contain a `TO <role>` clause.
func DerivePrivilegeRevoke(stmt string) (string, error) {
	canon := CanonicalisePrivilegeStatement(stmt)
	if canon == "" {
		return "", fmt.Errorf("empty privilege statement")
	}

	tokens := tokenSplit.Split(canon, -1)
	if len(tokens) < 4 {
		return "", fmt.Errorf("privilege statement too short: %q", stmt)
	}

	var verb string
	switch strings.ToUpper(tokens[0]) {
	case "GRANT", "DENY":
		verb = strings.ToUpper(tokens[0])
	default:
		return "", fmt.Errorf("statement must start with GRANT or DENY, got %q", tokens[0])
	}

	// Find the last `TO <role>` boundary. Privileges may contain a TO inside
	// quoted bodies, but our canonical form upper-cases bare TO only — quoted
	// 'TO' will not be re-cased. Walk tokens right-to-left for the first
	// bare TO.
	toIdx := -1
	for i := len(tokens) - 2; i > 0; i-- {
		if tokens[i] == "TO" {
			toIdx = i
			break
		}
	}
	if toIdx < 0 {
		return "", fmt.Errorf("statement missing TO <role>: %q", stmt)
	}

	body := strings.Join(tokens[1:toIdx], " ")
	roles := strings.Join(tokens[toIdx+1:], " ")
	return fmt.Sprintf("REVOKE %s %s FROM %s", verb, body, roles), nil
}

// PrivilegeStatementMatchesRole returns true when the (canonicalised)
// privilege statement ends with `TO <role>`. Used by validators to ensure
// each entry in Neo4jRole.spec.privileges names the role being defined.
func PrivilegeStatementMatchesRole(stmt, role string) bool {
	canon := CanonicalisePrivilegeStatement(stmt)
	if canon == "" {
		return false
	}
	tokens := tokenSplit.Split(canon, -1)
	if len(tokens) < 3 {
		return false
	}
	last := tokens[len(tokens)-1]
	prev := tokens[len(tokens)-2]
	if prev != "TO" {
		return false
	}
	// Strip backticks from `roleName` form
	last = strings.Trim(last, "`")
	return strings.EqualFold(last, role)
}

// PrivilegeStatementVerb returns "GRANT", "DENY" or "" for the first token
// of the (canonicalised) statement.
func PrivilegeStatementVerb(stmt string) string {
	canon := CanonicalisePrivilegeStatement(stmt)
	if canon == "" {
		return ""
	}
	first := strings.SplitN(canon, " ", 2)[0]
	switch strings.ToUpper(first) {
	case "GRANT":
		return "GRANT"
	case "DENY":
		return "DENY"
	default:
		return ""
	}
}
