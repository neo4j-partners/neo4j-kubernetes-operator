package status

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

// stubLogs stands in for the clientset: controller-runtime's client cannot read a log subresource,
// so the reader is an interface precisely so this can exist.
type stubLogs struct {
	body string
	err  error
	// previous records what the caller asked for, since reading the dead instance rather than the
	// live one is the whole point on a container that exited.
	previous bool
	calls    int
}

func (s *stubLogs) Head(_ context.Context, _, _, _ string, _ int64, previous bool) (string, error) {
	s.calls++
	s.previous = previous
	return s.body, s.err
}

func pluginScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := neo4jv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func standaloneWithPlugins(ids ...string) *neo4jv1.Neo4j {
	return &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Edition:  neo4jv1.EditionEnterprise,
			Version:  "2026.07.1",
			Topology: neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeStandalone},
			Plugins:  ids,
		},
	}
}

// memberPod is labelled the way the renderer labels it, so the writer's own selector finds it.
func memberPod(neo4j *neo4jv1.Neo4j, name string, restarts int32, running bool) *corev1.Pod {
	ctxRender := render.ContextForPool(neo4j, render.PoolServer)
	state := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	if !running {
		state = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: neo4j.Namespace, Labels: ctxRender.WorkloadLabels()},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         render.Neo4jContainerName,
				RestartCount: restarts,
				State:        state,
			}},
		},
	}
}

const downloadFailureLog = `Fetching versions.json for Plugin 'apoc-extended' from https://neo4j-contrib.github.io/neo4j-apoc-procedures/versions.json
ERROR: could not query https://neo4j-contrib.github.io/neo4j-apoc-procedures/versions.json for plugin compatibility information.
    Neo4j will continue to start, but "apoc-extended" will not be loaded.`

func observe(t *testing.T, neo4j *neo4jv1.Neo4j, logs *stubLogs, objs ...client.Object) (bool, oracle.Reason, string) {
	t.Helper()
	b := fake.NewClientBuilder().WithScheme(pluginScheme(t)).WithObjects(neo4j)
	if len(objs) > 0 {
		b = b.WithObjects(objs...)
	}
	w := &Writer{Client: b.Build(), PodLogs: logs}
	return w.observePluginsReady(t.Context(), neo4j)
}

// A spec asking for no plugin has nothing to check, and must not cost a log read.
func TestPluginsReadyNoPluginsAssigned(t *testing.T) {
	logs := &stubLogs{}
	ok, reason, _ := observe(t, standaloneWithPlugins(), logs)
	if !ok || reason != oracle.ReasonPluginsInstalled {
		t.Fatalf("ok=%v reason=%s", ok, reason)
	}
	if logs.calls != 0 {
		t.Errorf("read the log %d time(s) with no plugin assigned", logs.calls)
	}
}

// The failure that motivates the whole condition: the entrypoint could not fetch the plugin, said
// so, and let Neo4j start. The member is running and ready, so nothing else would report this.
func TestPluginsReadyReportsDownloadFailureOnARunningMember(t *testing.T) {
	neo4j := standaloneWithPlugins("apoc-extended")
	ok, reason, msg := observe(t, neo4j, &stubLogs{body: downloadFailureLog},
		memberPod(neo4j, "dev-server-0", 0, true))
	if ok {
		t.Fatal("a plugin the spec asks for is missing; PluginsReady must be false")
	}
	if reason != oracle.ReasonPluginDownloadFailed {
		t.Errorf("reason = %s, want %s", reason, oracle.ReasonPluginDownloadFailed)
	}
	if msg == "" || !contains(msg, "could not query") {
		t.Errorf("message must carry the image's own line, got %q", msg)
	}
}

// A healthy install has to read as silence, or every CR with a plugin never reaches Ready.
func TestPluginsReadyHealthyInstall(t *testing.T) {
	neo4j := standaloneWithPlugins("apoc")
	body := "Installing Plugin 'apoc' from /var/lib/neo4j/labs/apoc-core.jar to /plugins/apoc.jar\nStarting..."
	ok, reason, _ := observe(t, neo4j, &stubLogs{body: body}, memberPod(neo4j, "dev-server-0", 0, true))
	if !ok || reason != oracle.ReasonPluginsInstalled {
		t.Fatalf("ok=%v reason=%s", ok, reason)
	}
}

// Container logs are rotated by the kubelet and vanish with the pod. Refusing Ready because the
// evidence is gone would strand any CR whose pod outlived its log, so an unreadable log is not a
// failure.
func TestPluginsReadyTreatsUnreadableLogAsInstalled(t *testing.T) {
	neo4j := standaloneWithPlugins("apoc")
	ok, reason, _ := observe(t, neo4j, &stubLogs{err: errors.New("container has not started")},
		memberPod(neo4j, "dev-server-0", 0, true))
	if !ok || reason != oracle.ReasonPluginsInstalled {
		t.Fatalf("ok=%v reason=%s: a log we cannot read must not hold Ready back", ok, reason)
	}
}

// No reader wired means the check is off, not failing — the manager may have failed to build one.
func TestPluginsReadyWithoutAReaderIsDisabled(t *testing.T) {
	neo4j := standaloneWithPlugins("apoc")
	b := fake.NewClientBuilder().WithScheme(pluginScheme(t)).WithObjects(neo4j)
	w := &Writer{Client: b.Build()}
	ok, reason, _ := w.observePluginsReady(t.Context(), neo4j)
	if !ok || reason != oracle.ReasonPluginsInstalled {
		t.Fatalf("ok=%v reason=%s", ok, reason)
	}
}

// A refused JAR exits the entrypoint, so the live instance has printed nothing yet and the error
// is only in the instance that died.
func TestPluginsReadyReadsThePreviousInstanceOfACrashedContainer(t *testing.T) {
	neo4j := standaloneWithPlugins("gds")
	logs := &stubLogs{body: "Plugin at '/plugins/graph-data-science.jar' is not readable"}
	ok, reason, _ := observe(t, neo4j, logs, memberPod(neo4j, "dev-server-0", 3, false))
	if ok || reason != oracle.ReasonPluginJarUnreadable {
		t.Fatalf("ok=%v reason=%s", ok, reason)
	}
	if !logs.previous {
		t.Error("a container waiting after an exit must be read with previous=true, or the failure is invisible")
	}
}

// One condition carries one reason, so a pool hitting both failures has to report the one that
// cost more: a refused JAR stopped the container, a failed download only lost a plugin.
func TestPluginsReadyPrefersTheWorstFailure(t *testing.T) {
	neo4j := standaloneWithPlugins("apoc-extended", "gds")
	body := downloadFailureLog + "\nPlugin at '/plugins/graph-data-science.jar' is not readable"
	ok, reason, _ := observe(t, neo4j, &stubLogs{body: body}, memberPod(neo4j, "dev-server-0", 0, true))
	if ok {
		t.Fatal("expected a failure")
	}
	if reason != oracle.ReasonPluginJarUnreadable {
		t.Errorf("reason = %s, want %s", reason, oracle.ReasonPluginJarUnreadable)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
