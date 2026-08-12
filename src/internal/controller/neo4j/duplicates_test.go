package neo4j

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/status"
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

	reportDuplicateEntries(logf.Log, recorder, neo4j)

	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, status.ReasonDuplicateEntry) {
			t.Errorf("event %q should carry the oracle reason %s", event, status.ReasonDuplicateEntry)
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

	reportDuplicateEntries(logf.Log, recorder, neo4j)

	select {
	case event := <-recorder.Events:
		for _, want := range []string{status.ReasonDuplicateEntry, "spec.config.neo4j", "dbms.routing.enabled"} {
			if !strings.Contains(event, want) {
				t.Errorf("event %q should mention %q", event, want)
			}
		}
	default:
		t.Fatal("no event recorded for a config key overridden by the operator")
	}
}

func TestReportDuplicateEntriesSilentWithoutCollision(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	reportDuplicateEntries(logf.Log, recorder, neo4jWithJVM("-XX:+ExitOnOutOfMemoryError"))

	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected event for a non-colliding argument: %s", event)
	default:
	}
}

func TestReportDuplicateEntriesWithoutRecorder(t *testing.T) {
	reportDuplicateEntries(logf.Log, nil, neo4jWithJVM("-Djdk.nio.maxCachedBufferSize=2048"))
}
