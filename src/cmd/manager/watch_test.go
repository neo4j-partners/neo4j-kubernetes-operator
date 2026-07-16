package main

import (
	"reflect"
	"testing"
)

func TestWatchNamespaces(t *testing.T) {
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
	got, err = watchNamespaces()
	if err != nil || !reflect.DeepEqual(got, []string{"default", "neo4j-operator-system"}) {
		t.Fatalf("list = %#v err=%v", got, err)
	}
}
