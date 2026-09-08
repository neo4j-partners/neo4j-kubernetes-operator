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

// Package upgrade refuses, orders and reports a spec.version change (ADR-017).
//
// Preflight is the refusal half and runs at pipeline entry, before any domain step: a refusal that
// ran after domain/workload would arrive with the pod template already replaced.
package upgrade

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// Sentinel errors, mapped to catalogued reasons by internal/status.PipelineErrorReason.
var (
	// ErrVersionDowngrade is permanent: Neo4j does not support downgrade in any form, and reverting
	// spec.version does not undo a store the new binary has already opened. The documented recovery
	// is restoring a backup taken before the upgrade (ADR-017 R4).
	ErrVersionDowngrade = errors.New("version downgrade refused")
	// ErrVersionUpgrade covers the refusals a user can act on: an unsupported path, a digest pin
	// that would make the change a silent no-op, offline mode, or plugins that cannot be refreshed.
	ErrVersionUpgrade = errors.New("version change refused")
)

// Preflight reports whether the CR's spec.version may be rolled out, comparing it against
// status.version — what the workload is actually running.
//
// status.version is the anchor rather than a separately pinned field: once it reports the running
// version instead of echoing the spec, it already is the record of where we are. An empty value
// means the resource has never been ready, so this is a first install and not an upgrade.
func Preflight(n *neo4jv1beta1.Neo4j) error {
	running := n.Status.Version
	if running == "" || running == n.Spec.Version {
		return nil // first install, or nothing changed
	}

	from, fromOK := parseVersion(running)
	to, toOK := parseVersion(n.Spec.Version)
	if !fromOK || !toOK {
		// ponytail: unparseable versions are allowed through rather than blocking a cluster on a tag
		// shape we failed to anticipate. The roll is still gated by everything below.
		return nil
	}

	if to.compare(from) < 0 {
		return fmt.Errorf("%w: %s is older than the running %s. Neo4j does not support downgrade; "+
			"recover by restoring a backup taken before the upgrade",
			ErrVersionDowngrade, n.Spec.Version, running)
	}

	// R5: anything older than the 5.26 LTS must reach it before crossing into the CalVer series.
	// LTS releases are checkpoints and cannot be skipped.
	if from.calendar() != to.calendar() && !from.atLeast(5, 26) {
		return fmt.Errorf("%w: %s cannot go directly to %s. Neo4j treats LTS releases as checkpoints — "+
			"upgrade to 5.26 LTS first", ErrVersionUpgrade, running, n.Spec.Version)
	}

	// A digest pin wins over spec.version in the rendered image, so the bump would change nothing
	// while status advanced — the resource would report a version it is not running.
	if n.Spec.Image != nil && n.Spec.Image.Digest != "" {
		return fmt.Errorf("%w: spec.image.digest pins the image, so spec.version is not used and "+
			"nothing would roll. Change the digest instead", ErrVersionUpgrade)
	}

	if n.Spec.Maintenance != nil && n.Spec.Maintenance.OfflineMode {
		return fmt.Errorf("%w: spec.maintenance.offlineMode is true, so Neo4j is not running and the "+
			"new version could not start. Clear offline mode first", ErrVersionUpgrade)
	}

	// R11: an incompatible plugin JAR stops Neo4j starting at all. In Existing mode the operator
	// never emits NEO4J_PLUGINS, so nothing re-fetches and the new image boots against the old JARs.
	if v := pluginsVolume(n); v != nil && v.Mode == neo4jv1beta1.VolumeModeExisting {
		return fmt.Errorf("%w: storage.volumes.plugins is Existing, so the operator never refreshes "+
			"the plugin JARs and %s would start against the ones already on that claim. Update the "+
			"claim's plugins, or use Share or Dynamic mode", ErrVersionUpgrade, n.Spec.Version)
	}

	return nil
}

func pluginsVolume(n *neo4jv1beta1.Neo4j) *neo4jv1beta1.AuxiliaryVolumeSpec {
	if n.Spec.Storage == nil || n.Spec.Storage.Volumes == nil {
		return nil
	}
	return n.Spec.Storage.Volumes.Plugins
}

// version is a Neo4j version as a numeric component list. It spans both eras: SemVer 5.26.0 and
// CalVer 2026.05.0, which may carry an optional fourth LTS component. Non-numeric trailing parts
// (the -enterprise suffix a user may write, a pre-release marker) are dropped rather than compared.
type version struct{ parts []int }

func parseVersion(s string) (version, bool) {
	s = strings.TrimSuffix(strings.TrimSpace(s), "-enterprise")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return version{}, false
	}
	var v version
	for _, f := range strings.Split(s, ".") {
		n, err := strconv.Atoi(f)
		if err != nil {
			break // stop at the first non-numeric component, e.g. the LTS marker
		}
		v.parts = append(v.parts, n)
	}
	if len(v.parts) == 0 {
		return version{}, false
	}
	return v, true
}

// compare orders two versions, treating a missing component as zero so 5.26 and 5.26.0 are equal.
func (v version) compare(o version) int {
	n := max(len(v.parts), len(o.parts))
	for i := 0; i < n; i++ {
		if d := v.at(i) - o.at(i); d != 0 {
			if d < 0 {
				return -1
			}
			return 1
		}
	}
	return 0
}

func (v version) at(i int) int {
	if i < len(v.parts) {
		return v.parts[i]
	}
	return 0
}

// calendar reports whether this is a CalVer release. Neo4j moved from 5.x to 2025.xx, so a leading
// component that looks like a year separates the two eras.
func (v version) calendar() bool { return v.at(0) >= 2000 }

func (v version) atLeast(major, minor int) bool {
	return v.compare(version{parts: []int{major, minor}}) >= 0
}
