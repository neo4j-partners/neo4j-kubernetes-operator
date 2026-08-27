package neo4j

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/events"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
)

func neo4jWithJVM(args ...string) *neo4jv1beta1.Neo4j {
	useDefaults := true
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Config: &neo4jv1beta1.ConfigSpec{
				JVM: &neo4jv1beta1.JVMSpec{UseDefaults: &useDefaults, AdditionalArguments: args},
			},
		},
	}
}

func TestReportDuplicateEntriesEmitsWarningEvent(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	neo4j := neo4jWithJVM("-Djdk.nio.maxCachedBufferSize=2048")

	reportDuplicateEntries(logf.Log, recorder, &events.Advisory{}, neo4j)

	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, oracle.ReasonDuplicateEntry.String()) {
			t.Errorf("event %q should carry the catalogued reason %s", event, oracle.ReasonDuplicateEntry)
		}
		if !strings.Contains(event, "Warning") {
			t.Errorf("event %q should be a Warning", event)
		}
		// The message names the field so a user knows where to look, plus both values.
		for _, want := range []string{"spec.config.jvm.additionalArguments", "2048", "1024"} {
			if !strings.Contains(event, want) {
				t.Errorf("event %q should mention %q", event, want)
			}
		}
	default:
		t.Fatal("no event recorded for a colliding JVM argument")
	}
}

// A user setting silently replaced by the operator reports through the same path — the reason
// is field-agnostic, only the message changes.
func TestReportDuplicateEntriesCoversPlainConfigKeys(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:      neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{Members: 3},
			},
			Config: &neo4jv1beta1.ConfigSpec{Neo4j: map[string]string{"dbms.routing.enabled": "false"}},
		},
	}

	reportDuplicateEntries(logf.Log, recorder, &events.Advisory{}, neo4j)

	select {
	case event := <-recorder.Events:
		for _, want := range []string{oracle.ReasonDuplicateEntry.String(), "spec.config.neo4j", "dbms.routing.enabled"} {
			if !strings.Contains(event, want) {
				t.Errorf("event %q should mention %q", event, want)
			}
		}
	default:
		t.Fatal("no event recorded for a config key overridden by the operator")
	}
}

// A collision is a property of the spec, so the reconcile loop must report it once and not on
// every pass: Events are budgeted per object and the budget is shared with what the operator
// decides at runtime.
func TestReportDuplicateEntriesReportsOncePerGeneration(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	advisories := &events.Advisory{}
	neo4j := neo4jWithJVM("-Djdk.nio.maxCachedBufferSize=2048")

	for range 3 {
		reportDuplicateEntries(logf.Log, recorder, advisories, neo4j)
	}

	if got := len(recorder.Events); got != 1 {
		t.Fatalf("expected 1 event over 3 passes, got %d", got)
	}
}

func TestReportDuplicateEntriesSilentWithoutCollision(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	reportDuplicateEntries(logf.Log, recorder, &events.Advisory{}, neo4jWithJVM("-XX:+ExitOnOutOfMemoryError"))

	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected event for a non-colliding argument: %s", event)
	default:
	}
}

func TestReportDuplicateEntriesWithoutRecorder(t *testing.T) {
	reportDuplicateEntries(logf.Log, nil, &events.Advisory{}, neo4jWithJVM("-Djdk.nio.maxCachedBufferSize=2048"))
}
