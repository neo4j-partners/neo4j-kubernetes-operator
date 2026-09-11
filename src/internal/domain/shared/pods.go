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

package shared

import (
	"context"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// JobTerminal reports whether a Job reached a terminal condition and, on failure, a short detail
// from the Job condition (callers usually prefer JobPodTerminationMessage for the real cause).
func JobTerminal(job *batchv1.Job) (complete, failed bool, message string) {
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			return true, false, ""
		case batchv1.JobFailed:
			detail := c.Message
			if detail == "" {
				detail = c.Reason
			}
			return false, true, detail
		}
	}
	return false, false, ""
}

// JobPodTerminationMessage returns the most recent terminated container message across a Job's
// pods (selected by the standard job-name label). With
// terminationMessagePolicy=FallbackToLogsOnError on the container, this is what the container
// wrote to /dev/termination-log on success (e.g. recorded artifact names) or the tail of its
// output on failure (the real error). Empty when no pod recorded one. Best-effort: list errors
// are swallowed so callers can degrade gracefully.
func JobPodTerminationMessage(ctx context.Context, c client.Client, namespace, jobName string) string {
	var pods corev1.PodList
	if err := c.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabels{"job-name": jobName}); err != nil {
		return ""
	}
	var msg string
	var latest time.Time
	for i := range pods.Items {
		for _, cs := range pods.Items[i].Status.ContainerStatuses {
			t := cs.State.Terminated
			if t == nil || strings.TrimSpace(t.Message) == "" {
				continue
			}
			if msg == "" || t.FinishedAt.After(latest) {
				latest = t.FinishedAt.Time
				msg = strings.TrimSpace(t.Message)
			}
		}
	}
	return msg
}

// NamedArtifact is a filename plus optional byte size recorded by a backup/aggregate Job.
type NamedArtifact struct {
	Name      string
	SizeBytes int64
}

// ParseNamedArtifacts parses the "<db>=<file>[|<bytes>]" lines a backup/aggregate Job writes to
// its termination message into a map keyed by database. The "|<bytes>" suffix is optional (older
// Jobs omit it, and stat can fail) so a bare "<db>=<file>" still parses with SizeBytes 0. Empty
// names and malformed lines are skipped.
func ParseNamedArtifacts(message string) map[string]NamedArtifact {
	out := map[string]NamedArtifact{}
	for _, line := range strings.Split(message, "\n") {
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		na := NamedArtifact{Name: val}
		if j := strings.LastIndexByte(val, '|'); j >= 0 {
			na.Name = strings.TrimSpace(val[:j])
			if n, err := strconv.ParseInt(strings.TrimSpace(val[j+1:]), 10, 64); err == nil {
				na.SizeBytes = n
			}
		}
		if key == "" || na.Name == "" {
			continue
		}
		out[key] = na
	}
	return out
}
