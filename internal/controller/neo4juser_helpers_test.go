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
	"reflect"
	"testing"
)

func TestNormaliseRoles(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty input → empty", nil, []string{}},
		{"trim whitespace", []string{"  reader  ", "editor"}, []string{"editor", "reader"}},
		{"deduplicate", []string{"reader", "reader", "editor"}, []string{"editor", "reader"}},
		{"sorted output", []string{"reader", "admin", "editor"}, []string{"admin", "editor", "reader"}},
		{"strip empty entries", []string{"", "reader", "  "}, []string{"reader"}},
		{
			// THIS IS THE CRITICAL CASE — Neo4j auto-assigns PUBLIC to every
			// user, so SHOW USERS YIELD roles always returns it. Spec
			// comparison must filter it out or the user sits in Pending forever.
			name: "filter PUBLIC (auto-assigned by Neo4j)",
			in:   []string{"PUBLIC", "reader"},
			want: []string{"reader"},
		},
		{
			name: "filter PUBLIC case-insensitive",
			in:   []string{"public", "Reader", "PUBLIC"},
			want: []string{"Reader"},
		},
		{
			name: "PUBLIC alone produces empty",
			in:   []string{"PUBLIC"},
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normaliseRoles(tc.in)
			// Treat nil and empty-slice as equal for the "empty" cases.
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normaliseRoles(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestStringSlicesEqualSorted(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both empty", nil, nil, true},
		{"equal", []string{"a", "b"}, []string{"a", "b"}, true},
		{"different lengths", []string{"a"}, []string{"a", "b"}, false},
		{"different elements", []string{"a", "c"}, []string{"a", "b"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stringSlicesEqualSorted(tc.a, tc.b); got != tc.want {
				t.Errorf("stringSlicesEqualSorted(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
