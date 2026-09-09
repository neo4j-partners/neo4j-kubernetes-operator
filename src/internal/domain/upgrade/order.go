/*
Copyright Neo4j.

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

package upgrade

import (
	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// HoldPrimaries reports whether the primary pool must keep its current pods for now.
//
// Neo4j requires the system-database secondaries to be upgraded before the system primary: the
// primary upgrades the system database as it starts, and once it has, a secondary still on the old
// version can no longer join. The operator renders read and analytics pools with
// server.cluster.system_database_mode=SECONDARY, so every cluster it builds with one of those pools
// is subject to that rule (ADR-017 R3).
//
// Holding also gives the upgrade a canary: a new image that does not come up is found on a member
// that carries no Raft vote, before any primary has been touched.
//
// It holds only during a version change. A configuration change, where spec.version and
// status.version already agree, rolls every pool as it always has.
func HoldPrimaries(n *neo4jv1beta1.Neo4j, secondaries []PoolState) bool {
	if n.Status.Version == "" || n.Status.Version == n.Spec.Version {
		return false // first install, or no version change in flight
	}
	for _, p := range secondaries {
		// Settled first, and for the same reason Observe needs it: OnTarget is read from the pod
		// template, which the operator has just written, while everything after it is read from the
		// StatefulSet's status, which its controller has not yet caught up with. In that window a
		// pool whose image changed a moment ago still reports every replica updated and ready at the
		// old revision — converged, by every other test here — and the primaries would be released
		// onto the new image before the secondary had restarted a single pod. That is precisely the
		// order Neo4j forbids (R3), so the gate has to survive the gap between spec and status.
		if !p.Settled || !p.OnTarget || p.Rolling || p.Updated < p.Desired || p.Ready < p.Desired {
			return true
		}
	}
	return false // nothing to wait for: no secondary pools, or all of them are done
}
