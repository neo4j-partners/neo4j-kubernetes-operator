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
	"testing"
)

func TestCanonicalisePrivilegeStatement(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "lowercase verb is upper-cased",
			in:   "grant access on database analytics to analytics_reader",
			want: "GRANT ACCESS ON DATABASE analytics TO analytics_reader",
		},
		{
			name: "extra whitespace is collapsed",
			in:   "GRANT   READ   {*}    ON  GRAPH analytics  NODES * TO analytics_reader",
			want: "GRANT READ {*} ON GRAPH analytics NODES * TO analytics_reader",
		},
		{
			name: "trailing semicolon is stripped",
			in:   "GRANT ACCESS ON DATABASE analytics TO r;",
			want: "GRANT ACCESS ON DATABASE analytics TO r",
		},
		{
			name: "quoted strings preserve case and whitespace",
			in:   "GRANT ACCESS ON DATABASE 'My DB' TO r",
			want: "GRANT ACCESS ON DATABASE 'My DB' TO r",
		},
		{
			name: "empty input yields empty output",
			in:   "   ;   ",
			want: "",
		},
		{
			name: "deny is recognised as a verb",
			in:   "deny write on graph * to r",
			want: "DENY WRITE ON GRAPH * TO r",
		},
		{
			name: "two semantically-equal inputs produce equal output",
			in:   "GRANT  match {*}  ON GRAPH  myDb  NODES *  TO  r",
			want: "GRANT MATCH {*} ON GRAPH myDb NODES * TO r",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CanonicalisePrivilegeStatement(tc.in)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCanonicaliseEqualForms(t *testing.T) {
	a := CanonicalisePrivilegeStatement("grant access on database analytics to r")
	b := CanonicalisePrivilegeStatement("GRANT  ACCESS  ON  DATABASE  analytics  TO  r")
	if a != b {
		t.Fatalf("expected canonical forms to match, got %q vs %q", a, b)
	}
}

func TestDerivePrivilegeRevoke(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "GRANT becomes REVOKE GRANT",
			in:   "GRANT ACCESS ON DATABASE analytics TO analytics_reader",
			want: "REVOKE GRANT ACCESS ON DATABASE analytics FROM analytics_reader",
		},
		{
			name: "DENY becomes REVOKE DENY",
			in:   "DENY WRITE ON GRAPH * TO bob",
			want: "REVOKE DENY WRITE ON GRAPH * FROM bob",
		},
		{
			name: "complex body is preserved",
			in:   "GRANT MATCH {*} ON GRAPH analytics NODES * TO r",
			want: "REVOKE GRANT MATCH {*} ON GRAPH analytics NODES * FROM r",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DerivePrivilegeRevoke(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDerivePrivilegeRevoke_Errors(t *testing.T) {
	for _, in := range []string{
		"",
		"REVOKE ACCESS ON DATABASE x FROM r",
		"SELECT * FROM users",
		"GRANT ACCESS ON DATABASE x",
	} {
		if _, err := DerivePrivilegeRevoke(in); err == nil {
			t.Errorf("expected error for %q, got nil", in)
		}
	}
}

func TestPrivilegeStatementMatchesRole(t *testing.T) {
	cases := []struct {
		stmt string
		role string
		want bool
	}{
		{"GRANT ACCESS ON DATABASE x TO analytics_reader", "analytics_reader", true},
		{"grant access on database x to analytics_reader", "analytics_reader", true},
		{"GRANT ACCESS ON DATABASE x TO somebody_else", "analytics_reader", false},
		{"GRANT ACCESS ON DATABASE x", "analytics_reader", false},
		{"", "analytics_reader", false},
		{"GRANT ACCESS ON DATABASE x TO `analytics_reader`", "analytics_reader", true},
	}
	for _, tc := range cases {
		got := PrivilegeStatementMatchesRole(tc.stmt, tc.role)
		if got != tc.want {
			t.Errorf("PrivilegeStatementMatchesRole(%q, %q) = %v, want %v", tc.stmt, tc.role, got, tc.want)
		}
	}
}

func TestPrivilegeStatementVerb(t *testing.T) {
	cases := map[string]string{
		"GRANT ACCESS ON DATABASE x TO r":    "GRANT",
		"deny write on graph * to r":         "DENY",
		"REVOKE ACCESS ON DATABASE x FROM r": "",
		"":                                   "",
	}
	for in, want := range cases {
		if got := PrivilegeStatementVerb(in); got != want {
			t.Errorf("PrivilegeStatementVerb(%q) = %q, want %q", in, got, want)
		}
	}
}
