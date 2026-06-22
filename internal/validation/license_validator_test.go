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

package validation

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestValidateLicenseAcceptance(t *testing.T) {
	path := field.NewPath("spec", "acceptLicenseAgreement")
	tests := []struct {
		name      string
		value     string
		wantErr   bool
		wantType  field.ErrorType
		wantField string
	}{
		{name: "yes accepts commercial license", value: "yes", wantErr: false},
		{name: "eval accepts evaluation license", value: "eval", wantErr: false},
		{name: "empty fails closed (required)", value: "", wantErr: true, wantType: field.ErrorTypeRequired},
		{name: "no is not a valid acceptance", value: "no", wantErr: true, wantType: field.ErrorTypeNotSupported},
		{name: "wrong case is rejected", value: "YES", wantErr: true, wantType: field.ErrorTypeNotSupported},
		{name: "true is not accepted", value: "true", wantErr: true, wantType: field.ErrorTypeNotSupported},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateLicenseAcceptance(path, tc.value)
			if tc.wantErr {
				if len(errs) != 1 {
					t.Fatalf("value %q: expected 1 error, got %d (%v)", tc.value, len(errs), errs)
				}
				if errs[0].Type != tc.wantType {
					t.Errorf("value %q: error type = %v, want %v", tc.value, errs[0].Type, tc.wantType)
				}
				if errs[0].Field != path.String() {
					t.Errorf("value %q: error field = %q, want %q", tc.value, errs[0].Field, path.String())
				}
			} else if len(errs) != 0 {
				t.Errorf("value %q: expected no errors, got %v", tc.value, errs)
			}
		})
	}
}
