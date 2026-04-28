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
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	neo4jclient "github.com/priyolahiri/neo4j-kubernetes-operator/internal/neo4j"
)

// maxDiagnosticUsers and maxDiagnosticRoles cap the number of rows surfaced
// in status to keep CRDs small. The full count is always recorded in
// diagnostics.UserCount / diagnostics.RoleCount.
const (
	maxDiagnosticUsers = 50
	maxDiagnosticRoles = 50
)

// collectUsersAndRoles populates the Users/Roles diagnostic slices from
// SHOW USERS / SHOW ROLES. Best-effort: if either query fails (e.g. because
// the connecting user lacks SHOW USER/SHOW ROLE privilege), the failure is
// appended to collectionError but the diagnostics collection as a whole
// continues. Slices are sorted by name to keep status updates idempotent.
func collectUsersAndRoles(
	ctx context.Context,
	nc *neo4jclient.Client,
	users *[]neo4jv1beta1.UserDiagnosticInfo,
	userCount *int,
	roles *[]neo4jv1beta1.RoleDiagnosticInfo,
	roleCount *int,
	collectionError *string,
	logger logr.Logger,
) {
	rawUsers, err := nc.ListUsers(ctx)
	if err != nil {
		logger.Error(err, "Failed to collect SHOW USERS")
		appendCollectionError(collectionError, fmt.Sprintf("SHOW USERS failed: %v", err))
	} else {
		*userCount = len(rawUsers)
		// Sort by username for stable diffs.
		sort.SliceStable(rawUsers, func(i, j int) bool { return rawUsers[i].User < rawUsers[j].User })
		limit := len(rawUsers)
		if limit > maxDiagnosticUsers {
			limit = maxDiagnosticUsers
		}
		out := make([]neo4jv1beta1.UserDiagnosticInfo, 0, limit)
		for _, u := range rawUsers[:limit] {
			out = append(out, neo4jv1beta1.UserDiagnosticInfo{
				User:         u.User,
				Roles:        u.Roles,
				Suspended:    u.Suspended,
				HomeDatabase: u.HomeDatabase,
			})
		}
		*users = out
	}

	rawRoles, err := nc.ListRoles(ctx)
	if err != nil {
		logger.Error(err, "Failed to collect SHOW ROLES")
		appendCollectionError(collectionError, fmt.Sprintf("SHOW ROLES failed: %v", err))
	} else {
		*roleCount = len(rawRoles)
		sort.SliceStable(rawRoles, func(i, j int) bool { return rawRoles[i].Role < rawRoles[j].Role })
		limit := len(rawRoles)
		if limit > maxDiagnosticRoles {
			limit = maxDiagnosticRoles
		}
		out := make([]neo4jv1beta1.RoleDiagnosticInfo, 0, limit)
		for _, r := range rawRoles[:limit] {
			out = append(out, neo4jv1beta1.RoleDiagnosticInfo{
				Role:      r.Role,
				Immutable: r.Immutable,
			})
		}
		*roles = out
	}
}

func appendCollectionError(target *string, msg string) {
	if *target == "" {
		*target = msg
		return
	}
	*target = *target + "; " + msg
}
