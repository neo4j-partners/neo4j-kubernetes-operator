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

package neo4j_test

import (
	"errors"
	"fmt"
	"testing"

	driver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
)

func TestIsHostUnresolvableError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"no such host", errors.New("dial tcp: lookup my-cluster-server.ns.svc.cluster.local: no such host"), true},
		{"name resolution", errors.New("temporary failure in name resolution"), true},
		{"server misbehaving", errors.New("lookup foo: server misbehaving"), true},
		{"wrapped no such host", fmt.Errorf("failed to drop user bob: %w", errors.New("no such host")), true},
		{"connection refused is not host-gone", errors.New("dial tcp 10.0.0.1:7687: connect: connection refused"), false},
		{"not found is not host-gone", errors.New("user not found"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := neo4j.IsHostUnresolvableError(tc.err); got != tc.want {
				t.Fatalf("IsHostUnresolvableError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsConnectivityError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"connection refused", errors.New("dial tcp 10.0.0.1:7687: connect: connection refused"), true},
		{"i/o timeout", errors.New("read tcp: i/o timeout"), true},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"routing table", errors.New("unable to retrieve routing table from any of the servers"), true},
		// The driver's own helper uses a bare type assertion; ours must still
		// see through a %w wrap via errors.As.
		{"driver ConnectivityError direct", &driver.ConnectivityError{Inner: errors.New("boom")}, true},
		{"driver ConnectivityError wrapped", fmt.Errorf("drop failed: %w", &driver.ConnectivityError{Inner: errors.New("boom")}), true},
		{"host gone is not transient connectivity", errors.New("no such host"), false},
		{"not found is not connectivity", errors.New("database not found"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := neo4j.IsConnectivityError(tc.err); got != tc.want {
				t.Fatalf("IsConnectivityError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsNotFoundError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"database not found", errors.New("Neo.ClientError.Database.DatabaseNotFound: database not found"), true},
		{"does not exist", errors.New("role `auditor` does not exist"), true},
		{"user not found", errors.New("user not found"), true},
		{"wrapped", fmt.Errorf("failed to drop database mydb: %w", errors.New("database does not exist")), true},
		{"connection refused is not not-found", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := neo4j.IsNotFoundError(tc.err); got != tc.want {
				t.Fatalf("IsNotFoundError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
