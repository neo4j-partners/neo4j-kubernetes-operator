/*
Copyright 2025.

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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestJobConditionTrue(t *testing.T) {
	j := &batchv1.Job{Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{
		{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
		{Type: batchv1.JobComplete, Status: corev1.ConditionFalse},
	}}}
	assert.True(t, jobConditionTrue(j, batchv1.JobFailed))
	assert.False(t, jobConditionTrue(j, batchv1.JobComplete))
	assert.False(t, jobConditionTrue(&batchv1.Job{}, batchv1.JobFailed))
}

// TestCheckRestoreProgress_TerminalDecisions pins the failed-restore recovery
// fix: a failed POD attempt mid-retry (Status.Failed>0 without a JobFailed
// condition) must NOT be terminal; only the JobFailed condition is; and a
// TTL-collected (missing) Job is terminal rather than requeue-forever.
func TestCheckRestoreProgress_TerminalDecisions(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	ctx := context.Background()

	newRestore := func() *neo4jv1beta1.Neo4jRestore {
		return &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "default"},
			Spec:       neo4jv1beta1.Neo4jRestoreSpec{StopCluster: false, DatabaseName: "db"},
			Status:     neo4jv1beta1.Neo4jRestoreStatus{Phase: "Running"},
		}
	}
	newJob := func(mut func(*batchv1.Job)) *batchv1.Job {
		j := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "r1-restore", Namespace: "default"}}
		if mut != nil {
			mut(j)
		}
		return j
	}
	reconciler := func(objs ...client.Object) *Neo4jRestoreReconciler {
		restore := newRestore()
		all := append([]client.Object{restore}, objs...)
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(all...).WithStatusSubresource(restore).Build()
		return &Neo4jRestoreReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(16), RequeueAfter: 5 * time.Second}
	}

	t.Run("failed pod attempt without JobFailed condition keeps running", func(t *testing.T) {
		rec := reconciler(newJob(func(j *batchv1.Job) { j.Status.Failed = 1 }))
		res, err := rec.checkRestoreProgress(ctx, newRestore(), nil)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, res.RequeueAfter,
			"a failed pod attempt within BackoffLimit must requeue, not flip to terminal Failed")
	})

	t.Run("JobFailed condition is terminal", func(t *testing.T) {
		rec := reconciler(newJob(func(j *batchv1.Job) {
			j.Status.Failed = 4
			j.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
		}))
		res, err := rec.checkRestoreProgress(ctx, newRestore(), nil)
		require.NoError(t, err)
		assert.Zero(t, res.RequeueAfter, "JobFailed condition must be terminal (no requeue)")
	})

	t.Run("missing (TTL-collected) Job is terminal", func(t *testing.T) {
		rec := reconciler() // no Job object
		res, err := rec.checkRestoreProgress(ctx, newRestore(), nil)
		require.NoError(t, err)
		assert.Zero(t, res.RequeueAfter, "a TTL-collected Job must be terminal, not requeue forever")
	})
}
