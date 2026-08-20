// Package events records spec-derived advisories once per generation. client-go budgets Events per
// object — 25, then one every five minutes, on a key that ignores the reason — so an advisory
// re-emitted on every reconcile pass spends that budget and the next Event reporting an actual
// decision is dropped inside the operator, with nothing written anywhere.
package events

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// maxTracked bounds the memo the way client-go bounds its own event cache. Past that it is dropped
// wholesale, which at worst re-emits each advisory still in force once.
const maxTracked = 4096

// Advisory is the memo of what was already reported for an object's current generation. The zero
// value is ready to use, and safe for the concurrent reconciles of different objects.
type Advisory struct {
	mu   sync.Mutex
	seen map[advisoryKey]int64
}

// advisoryKey is per object and per exact text: one reason may carry several distinct advisories
// (one Event per duplicate config entry, per mounted Secret), and each deserves to be recorded.
type advisoryKey struct {
	uid  types.UID
	text string
}

// Emit records message unless the same advisory was already recorded for this generation of obj.
// Only for statements derived from the spec: an Event reporting a decision or a state must never
// go through here, since a generation is not a fact about the cluster.
func (a *Advisory) Emit(rec record.EventRecorder, obj client.Object, eventtype, reason, message string) {
	if rec == nil || obj == nil {
		return
	}
	if !a.claim(obj.GetUID(), reason+"\x00"+message, obj.GetGeneration()) {
		return
	}
	rec.Event(obj, eventtype, reason, message)
}

// Emitf is Emit with a formatted message.
func (a *Advisory) Emitf(rec record.EventRecorder, obj client.Object, eventtype, reason, format string, args ...any) {
	a.Emit(rec, obj, eventtype, reason, fmt.Sprintf(format, args...))
}

func (a *Advisory) claim(uid types.UID, text string, generation int64) bool {
	k := advisoryKey{uid: uid, text: text}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.seen == nil || len(a.seen) >= maxTracked {
		a.seen = make(map[advisoryKey]int64)
	}
	if last, ok := a.seen[k]; ok && last == generation {
		return false
	}
	a.seen[k] = generation
	return true
}
