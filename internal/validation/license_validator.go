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
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// validateLicenseAcceptance requires the user to explicitly accept the Neo4j
// Enterprise license. Neo4j Enterprise is commercial software owned by Neo4j,
// Inc.; this operator is independent and must NOT accept that license on the
// user's behalf — so an unset value fails closed (the operator refuses to
// deploy) rather than silently accepting commercial terms for the user.
//
// Accepted values:
//   - "yes"  — accept the Neo4j Enterprise commercial license agreement
//   - "eval" — accept the 30-day evaluation license
//
// Shared by the cluster and standalone validators so both deployment kinds
// enforce the same explicit acceptance.
func validateLicenseAcceptance(path *field.Path, value string) field.ErrorList {
	switch value {
	case "yes", "eval":
		return nil
	case "":
		return field.ErrorList{field.Required(path,
			"you must accept the Neo4j Enterprise license: set spec.acceptLicenseAgreement to "+
				"\"yes\" (commercial license) or \"eval\" (30-day evaluation). This operator is "+
				"independent and does not provide or accept a Neo4j license on your behalf — you "+
				"must hold your own valid Neo4j Enterprise license. See https://neo4j.com/licensing/")}
	default:
		return field.ErrorList{field.NotSupported(path, value, []string{"yes", "eval"})}
	}
}
