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

import "testing"

func TestSanitizeConfValue(t *testing.T) {
	cases := map[string]string{
		"block":                                 "block",
		"":                                      "",
		"OFF\ndbms.security.auth_enabled=false": "OFFdbms.security.auth_enabled=false",
		"a\r\nb":                                "ab",
	}
	for in, want := range cases {
		if got := sanitizeConfValue(in); got != want {
			t.Errorf("sanitizeConfValue(%q) = %q, want %q", in, got, want)
		}
	}
}
