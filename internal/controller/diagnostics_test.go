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
	"fmt"
	"testing"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// helper: build a minimal cluster for condition testing
func diagTestCluster() *neo4jv1alpha1.Neo4jEnterpriseCluster {
	return &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster",
			Namespace:  "default",
			Generation: 2,
		},
	}
}

// helper: find a condition by type
func findCondByType(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, condType string) *metav1.Condition {
	for i := range cluster.Status.Conditions {
		if cluster.Status.Conditions[i].Type == condType {
			return &cluster.Status.Conditions[i]
		}
	}
	return nil
}

// ---------- ServersHealthy tests ----------

func TestUpdateServersCondition_AllHealthy(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := diagTestCluster()
	servers := []neo4jclient.ServerInfo{
		{Name: "server-0", Address: "server-0.svc:7687", State: "Enabled", Health: "Available", Hosting: []string{"neo4j"}},
		{Name: "server-1", Address: "server-1.svc:7687", State: "Enabled", Health: "Available", Hosting: []string{"neo4j"}},
	}

	qm.updateServersCondition(cluster, servers, nil)

	cond := findCondByType(cluster, ConditionTypeServersHealthy)
	if cond == nil {
		t.Fatal("expected ServersHealthy condition to be set")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected True, got %s", cond.Status)
	}
	if cond.Reason != ConditionReasonAllServersHealthy {
		t.Errorf("expected reason %s, got %s", ConditionReasonAllServersHealthy, cond.Reason)
	}
	if cond.ObservedGeneration != 2 {
		t.Errorf("expected observedGeneration=2, got %d", cond.ObservedGeneration)
	}
}

func TestUpdateServersCondition_CordonnedServer(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := diagTestCluster()
	servers := []neo4jclient.ServerInfo{
		{Name: "server-0", State: "Enabled", Health: "Available"},
		{Name: "server-1", State: "Cordoned", Health: "Available"},
	}

	qm.updateServersCondition(cluster, servers, nil)

	cond := findCondByType(cluster, ConditionTypeServersHealthy)
	if cond == nil {
		t.Fatal("expected ServersHealthy condition")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected False, got %s", cond.Status)
	}
	if cond.Reason != ConditionReasonServerDegraded {
		t.Errorf("expected %s, got %s", ConditionReasonServerDegraded, cond.Reason)
	}
	if cond.Message == "" {
		t.Error("expected non-empty message for degraded server")
	}
}

func TestUpdateServersCondition_UnavailableHealth(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := diagTestCluster()
	servers := []neo4jclient.ServerInfo{
		{Name: "server-0", State: "Enabled", Health: "Unavailable"},
	}

	qm.updateServersCondition(cluster, servers, nil)

	cond := findCondByType(cluster, ConditionTypeServersHealthy)
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Errorf("expected False condition for Unavailable server")
	}
}

func TestUpdateServersCondition_CollectionError(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := diagTestCluster()

	qm.updateServersCondition(cluster, nil, fmt.Errorf("bolt connection refused"))

	cond := findCondByType(cluster, ConditionTypeServersHealthy)
	if cond == nil {
		t.Fatal("expected ServersHealthy condition")
	}
	if cond.Status != metav1.ConditionUnknown {
		t.Errorf("expected Unknown, got %s", cond.Status)
	}
	if cond.Reason != ConditionReasonDiagnosticsUnavailable {
		t.Errorf("expected %s, got %s", ConditionReasonDiagnosticsUnavailable, cond.Reason)
	}
}

func TestUpdateServersCondition_EmptyList(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := diagTestCluster()

	qm.updateServersCondition(cluster, []neo4jclient.ServerInfo{}, nil)

	cond := findCondByType(cluster, ConditionTypeServersHealthy)
	if cond == nil || cond.Status != metav1.ConditionUnknown {
		t.Errorf("expected Unknown condition for empty server list")
	}
}

// ---------- DatabasesHealthy tests ----------

func TestUpdateDatabasesCondition_AllOnline(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := diagTestCluster()
	databases := []neo4jclient.DatabaseInfo{
		{Name: "system", Status: "online", RequestedStatus: "online"},
		{Name: "neo4j", Status: "online", RequestedStatus: "online"},
		{Name: "mydb", Status: "online", RequestedStatus: "online"},
	}

	qm.updateDatabasesCondition(cluster, databases, nil)

	cond := findCondByType(cluster, ConditionTypeDatabasesHealthy)
	if cond == nil {
		t.Fatal("expected DatabasesHealthy condition")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected True, got %s", cond.Status)
	}
	if cond.Reason != ConditionReasonAllDatabasesOnline {
		t.Errorf("expected %s, got %s", ConditionReasonAllDatabasesOnline, cond.Reason)
	}
}

func TestUpdateDatabasesCondition_DatabaseOffline(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := diagTestCluster()
	databases := []neo4jclient.DatabaseInfo{
		{Name: "system", Status: "online", RequestedStatus: "online"},
		{Name: "neo4j", Status: "online", RequestedStatus: "online"},
		{Name: "mydb", Status: "offline", RequestedStatus: "online"},
	}

	qm.updateDatabasesCondition(cluster, databases, nil)

	cond := findCondByType(cluster, ConditionTypeDatabasesHealthy)
	if cond == nil {
		t.Fatal("expected DatabasesHealthy condition")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected False, got %s", cond.Status)
	}
	if cond.Reason != ConditionReasonDatabaseOffline {
		t.Errorf("expected %s, got %s", ConditionReasonDatabaseOffline, cond.Reason)
	}
	if len(cond.Message) == 0 {
		t.Error("expected non-empty message")
	}
}

func TestUpdateDatabasesCondition_SystemDatabaseOfflineIgnored(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := diagTestCluster()
	databases := []neo4jclient.DatabaseInfo{
		{Name: "system", Status: "offline", RequestedStatus: "online"},
		{Name: "neo4j", Status: "online", RequestedStatus: "online"},
	}

	qm.updateDatabasesCondition(cluster, databases, nil)

	cond := findCondByType(cluster, ConditionTypeDatabasesHealthy)
	if cond == nil {
		t.Fatal("expected DatabasesHealthy condition")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected True (system db skipped), got %s (message: %s)", cond.Status, cond.Message)
	}
}

func TestUpdateDatabasesCondition_RequestedOfflineIgnored(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := diagTestCluster()
	databases := []neo4jclient.DatabaseInfo{
		{Name: "neo4j", Status: "online", RequestedStatus: "online"},
		{Name: "archived", Status: "offline", RequestedStatus: "offline"},
	}

	qm.updateDatabasesCondition(cluster, databases, nil)

	cond := findCondByType(cluster, ConditionTypeDatabasesHealthy)
	if cond == nil {
		t.Fatal("expected DatabasesHealthy condition")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected True (intentionally offline db should not be flagged), got %s", cond.Status)
	}
}

func TestUpdateDatabasesCondition_CollectionError(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := diagTestCluster()

	qm.updateDatabasesCondition(cluster, nil, fmt.Errorf("session expired"))

	cond := findCondByType(cluster, ConditionTypeDatabasesHealthy)
	if cond == nil {
		t.Fatal("expected DatabasesHealthy condition")
	}
	if cond.Status != metav1.ConditionUnknown {
		t.Errorf("expected Unknown, got %s", cond.Status)
	}
	if cond.Reason != ConditionReasonDiagnosticsUnavailable {
		t.Errorf("expected %s, got %s", ConditionReasonDiagnosticsUnavailable, cond.Reason)
	}
}

// ---------- SetNamedCondition idempotency test ----------

func TestSetNamedCondition_Idempotent(t *testing.T) {
	cluster := diagTestCluster()
	SetNamedCondition(&cluster.Status.Conditions, ConditionTypeServersHealthy, 1,
		metav1.ConditionTrue, ConditionReasonAllServersHealthy, "all good")

	before := findCondByType(cluster, ConditionTypeServersHealthy)
	if before == nil {
		t.Fatal("condition not set")
	}
	ts1 := before.LastTransitionTime

	// Call again with same status/reason — LastTransitionTime should NOT change
	SetNamedCondition(&cluster.Status.Conditions, ConditionTypeServersHealthy, 1,
		metav1.ConditionTrue, ConditionReasonAllServersHealthy, "all good updated message")

	after := findCondByType(cluster, ConditionTypeServersHealthy)
	if after.LastTransitionTime != ts1 {
		t.Error("LastTransitionTime should not change when status and reason are unchanged")
	}

	// Change status — LastTransitionTime SHOULD change
	SetNamedCondition(&cluster.Status.Conditions, ConditionTypeServersHealthy, 1,
		metav1.ConditionFalse, ConditionReasonServerDegraded, "one server down")
	after2 := findCondByType(cluster, ConditionTypeServersHealthy)
	if after2.LastTransitionTime == ts1 {
		t.Error("LastTransitionTime should change when status changes")
	}
}
