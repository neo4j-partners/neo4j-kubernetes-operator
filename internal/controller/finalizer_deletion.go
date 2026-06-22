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

package controller

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/neo4j"
)

// finalizerDeletionGracePeriod bounds how long the operator keeps retrying a
// failed Neo4j drop/revoke during finalizer cleanup before it releases the
// finalizer anyway. A CR wedged undeletable forever is worse than an orphaned
// Neo4j object an admin can drop by hand, so transient and unknown failures get
// a bounded retry window — not an infinite one.
const finalizerDeletionGracePeriod = 5 * time.Minute

// finalizerCleanupDisposition is the decision for a Neo4j drop/revoke that ran
// (or failed) during finalizer cleanup.
type finalizerCleanupDisposition int

const (
	// releaseFinalizer means the finalizer may be removed now: the cleanup
	// succeeded, the target was already gone, the host's DNS no longer resolves
	// (cluster deleted), or the bounded retry window has elapsed.
	releaseFinalizer finalizerCleanupDisposition = iota
	// retryCleanup means the failure looks transient and the grace period has
	// not yet elapsed; the caller should requeue and keep the finalizer.
	retryCleanup
)

// classifyFinalizerCleanup decides whether a Neo4j drop/revoke performed during
// deletion lets the finalizer be released. It is the single shared policy used
// by every CR whose deletion runs Cypher against Neo4j (user, role,
// rolebinding, authrule, database):
//
//   - err == nil ...................... release (cleanup succeeded)
//   - target already gone ............. release (idempotent — desired end state)
//   - host DNS no longer resolves ..... release (Service/cluster deleted; the
//     object went with it and the name will not come back)
//   - transient / unknown ............. retry until finalizerDeletionGracePeriod
//     has elapsed since deletion was requested, then release rather than wedge
//
// obj is the object being deleted; its DeletionTimestamp bounds the retry
// window. A nil DeletionTimestamp (object not actually being deleted) is
// treated conservatively as "just started", so transient errors retry.
func classifyFinalizerCleanup(obj metav1.Object, err error) finalizerCleanupDisposition {
	if err == nil {
		return releaseFinalizer
	}
	if neo4j.IsNotFoundError(err) || neo4j.IsHostUnresolvableError(err) {
		return releaseFinalizer
	}
	// Transient connect failures, conflicting-transaction retries, and any
	// unclassified error all get the same bounded-retry treatment: keep trying
	// while the grace window is open, then release so the CR can finish
	// deleting instead of wedging forever.
	if deletionGracePeriodExceeded(obj) {
		return releaseFinalizer
	}
	return retryCleanup
}

// isAlreadyGoneCleanup reports whether err means the specific target of a
// drop/revoke is already gone — i.e. that one operation is idempotently
// satisfied. Multi-step cleanup (e.g. revoking several role grants in a loop)
// uses this to skip a satisfied item and keep going with the rest, instead of
// routing it through classifyFinalizerCleanup, which would release the
// finalizer and abandon the remaining items on the first not-found.
func isAlreadyGoneCleanup(err error) bool {
	return neo4j.IsNotFoundError(err)
}

// deletionGracePeriodExceeded reports whether finalizerDeletionGracePeriod has
// elapsed since the object's deletion was requested.
func deletionGracePeriodExceeded(obj metav1.Object) bool {
	ts := obj.GetDeletionTimestamp()
	if ts == nil {
		return false
	}
	return time.Since(ts.Time) > finalizerDeletionGracePeriod
}
