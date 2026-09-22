package status

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/plugins"
)

// logHeadBytes bounds the read. The entrypoint's plugin lines are within the first few hundred
// bytes; 32 KiB leaves room for a verbose boot ahead of them without streaming a whole log.
const logHeadBytes = 32 * 1024

// observePluginsReady reports whether the plugins the spec asks for reached the server.
//
// The operator does not install them: NEO4J_PLUGINS makes the image entrypoint do it before Neo4j
// starts. Two of the three failures it can hit are not fatal — it prints the error, returns, and
// lets the server come up without the plugin — so a member can be perfectly ready while the spec
// went unhonoured. Reading the container's opening output is the only evidence of that (BDR-004).
//
// Best-effort by construction. Container logs are rotated by the kubelet and lost when a pod is
// replaced, so a missing log is reported as installed rather than as a failure: refusing Ready on
// absent evidence would strand every CR whose pod outlived its log.
func (w *Writer) observePluginsReady(ctx context.Context, neo4j *neo4jv1.Neo4j) (ok bool, reason oracle.Reason, message string) {
	if !anyPluginAssigned(neo4j) {
		return true, oracle.ReasonPluginsInstalled, ""
	}
	if w.PodLogs == nil || w.Client == nil {
		// No reader wired (unit tests, or a manager built without one) — the spec's own validation
		// has already refused what it can, and silence is better than a false failure.
		return true, oracle.ReasonPluginsInstalled, ""
	}
	log := ctrllog.FromContext(ctx)

	var failures []plugins.Failure
	for _, pool := range render.ActivePools(neo4j) {
		ctxRender := render.ContextForPool(neo4j, pool)
		if len(ctxRender.PoolPluginIDs()) == 0 {
			continue
		}
		var pods corev1.PodList
		if err := w.Client.List(ctx, &pods, client.InNamespace(neo4j.Namespace),
			client.MatchingLabels(ctxRender.WorkloadLabels())); err != nil {
			log.V(1).Info("plugin check skipped: cannot list pods", "pool", string(pool), "error", err.Error())
			continue
		}
		for i := range pods.Items {
			failures = append(failures, w.pluginFailuresOnPod(ctx, &pods.Items[i])...)
		}
	}
	if len(failures) == 0 {
		return true, oracle.ReasonPluginsInstalled, ""
	}
	// One reason for the whole condition, so the worst wins: a JAR the entrypoint refused stopped
	// the container, which matters more than a plugin that merely failed to download.
	worst := failures[0]
	for _, f := range failures[1:] {
		if f.Kind > worst.Kind {
			worst = f
		}
	}
	return false, reasonFor(worst.Kind), failureMessage(failures)
}

// pluginFailuresOnPod reads one pod's opening output. A container that exited is read with
// previous, which is where a JAR refusal lives: the current instance has not printed it yet.
func (w *Writer) pluginFailuresOnPod(ctx context.Context, pod *corev1.Pod) []plugins.Failure {
	log := ctrllog.FromContext(ctx)
	previous := crashed(pod)
	body, err := w.PodLogs.Head(ctx, pod.Namespace, pod.Name, render.Neo4jContainerName, logHeadBytes, previous)
	if err != nil {
		// A pod still pulling its image, or one whose log the kubelet has dropped, has no output
		// to read. Not a plugin failure.
		log.V(1).Info("plugin check skipped: cannot read container log", "pod", pod.Name, "error", err.Error())
		return nil
	}
	return plugins.DiagnoseLog(body)
}

// crashed reports whether the Neo4j container is waiting after an exit, which is the shape a
// refused JAR takes: the entrypoint exits 1 and the kubelet backs off.
func crashed(pod *corev1.Pod) bool {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != render.Neo4jContainerName {
			continue
		}
		return cs.RestartCount > 0 && cs.State.Running == nil
	}
	return false
}

func reasonFor(kind plugins.FailureKind) oracle.Reason {
	switch kind {
	case plugins.FailureDownload:
		return oracle.ReasonPluginDownloadFailed
	case plugins.FailureVersionIncompatible:
		return oracle.ReasonPluginVersionIncompatible
	case plugins.FailureJarUnreadable:
		return oracle.ReasonPluginJarUnreadable
	default:
		return oracle.ReasonPluginDownloadFailed
	}
}

// failureMessage carries the entrypoint's own lines, deduplicated: every member of a pool hits the
// same failure, and repeating it once per pod would bury the one line that explains it.
func failureMessage(failures []plugins.Failure) string {
	seen := map[string]struct{}{}
	var lines []string
	for _, f := range failures {
		if _, dup := seen[f.Detail]; dup {
			continue
		}
		seen[f.Detail] = struct{}{}
		lines = append(lines, f.Detail)
	}
	sort.Strings(lines)
	return fmt.Sprintf("the image entrypoint did not install every assigned plugin: %s",
		strings.Join(lines, " | "))
}

func anyPluginAssigned(neo4j *neo4jv1.Neo4j) bool {
	for _, pool := range render.ActivePools(neo4j) {
		if len(render.ContextForPool(neo4j, pool).PoolPluginIDs()) > 0 {
			return true
		}
	}
	return false
}
