package main

import (
	"reflect"
	"testing"
)

func TestWatchNamespaces(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "")
	t.Setenv("WATCH_NAMESPACE", "")
	if _, err := watchNamespaces(); err == nil {
		t.Fatal("empty WATCH_NAMESPACE should error")
	}

	t.Setenv("WATCH_NAMESPACE", "*")
	if _, err := watchNamespaces(); err == nil {
		t.Fatal("* should error")
	}

	t.Setenv("WATCH_NAMESPACE", "default")
	got, err := watchNamespaces()
	if err != nil || !reflect.DeepEqual(got, []string{"default"}) {
		t.Fatalf("single = %#v err=%v", got, err)
	}

	t.Setenv("WATCH_NAMESPACE", " default, neo4j-operator-system ,default ")
	t.Setenv("POD_NAMESPACE", "")
	got, err = watchNamespaces()
	if err != nil || !reflect.DeepEqual(got, []string{"default", "neo4j-operator-system"}) {
		t.Fatalf("list = %#v err=%v", got, err)
	}

	t.Setenv("WATCH_NAMESPACE", "default,neo4j-operator-system")
	t.Setenv("POD_NAMESPACE", "neo4j-operator-system")
	if _, err := watchNamespaces(); err == nil {
		t.Fatal("watching the operator namespace should error (NEO-016)")
	}

	t.Setenv("WATCH_NAMESPACE", "default")
	t.Setenv("POD_NAMESPACE", "neo4j-operator-system")
	got, err = watchNamespaces()
	if err != nil || !reflect.DeepEqual(got, []string{"default"}) {
		t.Fatalf("workload-only = %#v err=%v", got, err)
	}
}

func TestResolveWatchScope(t *testing.T) {
	// Explicit list → namespaced scope.
	t.Setenv("WATCH_ALL_NAMESPACES", "")
	t.Setenv("WATCH_NAMESPACE", "default")
	t.Setenv("POD_NAMESPACE", "neo4j-operator-system")
	s, err := resolveWatchScope()
	if err != nil || s.AllNamespaces || !reflect.DeepEqual(s.Namespaces, []string{"default"}) || s.OperatorNS != "neo4j-operator-system" {
		t.Fatalf("namespaced = %#v err=%v", s, err)
	}

	// Cluster-wide opt-in with empty WATCH_NAMESPACE.
	t.Setenv("WATCH_ALL_NAMESPACES", "true")
	t.Setenv("WATCH_NAMESPACE", "")
	s, err = resolveWatchScope()
	if err != nil || !s.AllNamespaces || len(s.Namespaces) != 0 || s.OperatorNS != "neo4j-operator-system" {
		t.Fatalf("cluster-wide = %#v err=%v", s, err)
	}

	// Mutually exclusive: cluster-wide + a namespace list is an error.
	t.Setenv("WATCH_ALL_NAMESPACES", "true")
	t.Setenv("WATCH_NAMESPACE", "default")
	if _, err := resolveWatchScope(); err == nil {
		t.Fatal("WATCH_ALL_NAMESPACES with WATCH_NAMESPACE should error")
	}

	// Fail-closed: neither set is still an error (delegates to watchNamespaces).
	t.Setenv("WATCH_ALL_NAMESPACES", "")
	t.Setenv("WATCH_NAMESPACE", "")
	if _, err := resolveWatchScope(); err == nil {
		t.Fatal("no scope should error")
	}
}
