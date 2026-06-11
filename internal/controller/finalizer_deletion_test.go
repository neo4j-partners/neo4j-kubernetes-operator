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
	"errors"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	driver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// objBeingDeleted returns a minimal metav1.Object whose deletion was requested
// `age` ago (age <= 0 means "no deletion timestamp set").
func objBeingDeleted(age time.Duration) metav1.Object {
	om := &metav1.ObjectMeta{}
	if age > 0 {
		ts := metav1.NewTime(time.Now().Add(-age))
		om.DeletionTimestamp = &ts
	}
	return om
}

func TestClassifyFinalizerCleanup(t *testing.T) {
	justDeleted := time.Second                              // well inside grace window
	pastGrace := finalizerDeletionGracePeriod + time.Minute // grace window elapsed

	cases := []struct {
		name string
		obj  metav1.Object
		err  error
		want finalizerCleanupDisposition
	}{
		{"nil error releases", objBeingDeleted(justDeleted), nil, releaseFinalizer},
		{"not found releases", objBeingDeleted(justDeleted), errors.New("user not found"), releaseFinalizer},
		{"database not found releases", objBeingDeleted(justDeleted), errors.New("database does not exist"), releaseFinalizer},
		{"host gone releases immediately", objBeingDeleted(justDeleted), errors.New("dial tcp: lookup x: no such host"), releaseFinalizer},
		{"transient connect retries within grace", objBeingDeleted(justDeleted), errors.New("connection refused"), retryCleanup},
		{"transient connect releases after grace", objBeingDeleted(pastGrace), errors.New("connection refused"), releaseFinalizer},
		{"driver connectivity wrapped retries within grace", objBeingDeleted(justDeleted), fmt.Errorf("drop: %w", &driver.ConnectivityError{Inner: errors.New("boom")}), retryCleanup},
		{"unknown error retries within grace", objBeingDeleted(justDeleted), errors.New("some weird neo4j error"), retryCleanup},
		{"unknown error releases after grace", objBeingDeleted(pastGrace), errors.New("some weird neo4j error"), releaseFinalizer},
		// Defensive: no deletion timestamp is treated as "just started" so a
		// transient error retries rather than releasing prematurely.
		{"no deletion timestamp retries on transient", objBeingDeleted(0), errors.New("connection refused"), retryCleanup},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyFinalizerCleanup(tc.obj, tc.err); got != tc.want {
				t.Fatalf("classifyFinalizerCleanup(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
