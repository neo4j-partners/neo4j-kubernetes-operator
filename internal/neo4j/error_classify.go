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
	"errors"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// IsTransientError reports whether err is a transient Neo4j error worth
// retrying (driver-classified retryable, or a conflicting-transaction-state
// marker). Exported wrapper around the package-internal classifier so callers
// outside this package (e.g. controller finalizer cleanup) can reuse it.
func IsTransientError(err error) bool {
	return isTransientNeo4jError(err)
}

// IsHostUnresolvableError reports whether err indicates the Neo4j host's DNS
// name no longer resolves — the signal that the Service (and almost always the
// whole cluster/standalone) has been deleted. Distinct from a transient
// connect failure (IsConnectivityError): a vanished DNS name will not come
// back on its own, so finalizer cleanup against it is a permanent no-op and the
// finalizer can be released immediately.
func IsHostUnresolvableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"no such host",
		"name resolution",
		"server misbehaving",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// IsConnectivityError reports whether err indicates the operator could not
// reach the Neo4j server even though its host still resolves: connection
// refused (pod down/restarting), connect timeout, reset, or a routing-table
// fetch failure. These are typically transient (a rolling restart, a
// not-yet-ready pod), so callers should retry rather than give up immediately.
//
// The driver's own neo4j.IsConnectivityError uses a bare type assertion, so it
// returns false once the error has been wrapped with %w (as every drop/revoke
// helper in this package does). We unwrap with errors.As so wrapped driver
// errors are still recognised, then fall back to matching the common
// network-failure substrings the driver surfaces as plain strings.
//
// DNS-resolution failures are deliberately excluded here — see
// IsHostUnresolvableError, which callers should check first.
func IsConnectivityError(err error) bool {
	if err == nil {
		return false
	}
	var ce *neo4j.ConnectivityError
	if errors.As(err, &ce) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"connection refused",
		"i/o timeout",
		"connection reset",
		"broken pipe",
		"unable to retrieve routing table",
		"could not perform discovery",
		"server is shutting down",
		"dial tcp",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// IsNotFoundError reports whether err indicates the target object (database,
// user, or role) does not exist — i.e. a drop/revoke is already satisfied and
// can be treated as idempotent success.
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if isDatabaseNotFoundError(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "does not exist")
}
