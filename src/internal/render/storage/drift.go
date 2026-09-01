package storage

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
)

// ErrTemplateDrift is returned when a live StatefulSet's claim templates no longer match what the
// spec renders. Kubernetes refuses a new set of volumeClaimTemplates, so there is nothing the
// operator can patch: it reports and stops (BDR-005).
var ErrTemplateDrift = errors.New("statefulset volumeClaimTemplates no longer match the spec")

// VolumeClaimDrift describes how a live StatefulSet's claim templates diverge from the rendered
// ones in a way Kubernetes will not let the operator repair, or returns "" when they agree.
//
// Size is deliberately not compared: it is the one thing allowed to differ, because a grow is
// applied to the claims and never to the immutable template, which keeps the size the StatefulSet
// was created with for good. Defaulted fields are not compared either — the live template comes
// back from the API server with a storage class and a volume mode filled in, so comparing those
// would report drift on every pass of a perfectly aligned StatefulSet.
//
// What is compared is what actually wedges a StatefulSet: the set of claim names. The renderer
// emits a claim and its pod volumeMount as a pair, so a spec that adds or drops a volume would
// otherwise produce a pod template mounting a volume no template backs. Admission now freezes that
// set (the CEL rules on VolumesSpec), which leaves this guard for CRs created before those rules
// existed.
func VolumeClaimDrift(desired, live []corev1.PersistentVolumeClaim) string {
	liveByName := make(map[string]corev1.PersistentVolumeClaim, len(live))
	for _, vct := range live {
		liveByName[vct.Name] = vct
	}
	var problems []string
	for _, want := range desired {
		got, ok := liveByName[want.Name]
		if !ok {
			problems = append(problems, fmt.Sprintf("the spec asks for volume %q, which the StatefulSet has no template for", want.Name))
			continue
		}
		delete(liveByName, want.Name)
		if want.Spec.StorageClassName != nil && *want.Spec.StorageClassName != "" {
			liveClass := ""
			if got.Spec.StorageClassName != nil {
				liveClass = *got.Spec.StorageClassName
			}
			if liveClass != *want.Spec.StorageClassName {
				problems = append(problems, fmt.Sprintf("volume %q asks for StorageClass %q and the StatefulSet has %q",
					want.Name, *want.Spec.StorageClassName, liveClass))
			}
		}
		if len(want.Spec.AccessModes) > 0 && !equality.Semantic.DeepEqual(want.Spec.AccessModes, got.Spec.AccessModes) {
			problems = append(problems, fmt.Sprintf("volume %q asks for access modes %v and the StatefulSet has %v",
				want.Name, want.Spec.AccessModes, got.Spec.AccessModes))
		}
	}
	for name := range liveByName {
		problems = append(problems, fmt.Sprintf("the StatefulSet has a template for volume %q, which the spec no longer asks for", name))
	}
	sort.Strings(problems)
	return strings.Join(problems, "; ")
}
