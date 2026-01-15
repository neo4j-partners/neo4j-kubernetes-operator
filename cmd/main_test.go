package main

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestParseWatchNamespaceConfigEmpty(t *testing.T) {
	cfg, err := parseWatchNamespaceConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.isAll() {
		t.Fatalf("expected watch-all config for empty input")
	}
}

func TestParseWatchNamespaceConfigExplicit(t *testing.T) {
	cfg, err := parseWatchNamespaceConfig("team-b, team-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.isAll() {
		t.Fatalf("expected explicit list, got watch-all")
	}
	if len(cfg.explicit) != 2 || cfg.explicit[0] != "team-a" || cfg.explicit[1] != "team-b" {
		t.Fatalf("unexpected explicit namespaces: %v", cfg.explicit)
	}
}

func TestParseWatchNamespaceConfigPatterns(t *testing.T) {
	cfg, err := parseWatchNamespaceConfig("team-*,regex:^prod-,label:{env=prod,tier=backend},glob:dev-*")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.globs) != 2 {
		t.Fatalf("expected 2 glob patterns, got %d", len(cfg.globs))
	}
	if len(cfg.regexRaw) != 1 || cfg.regexRaw[0] != "^prod-" {
		t.Fatalf("unexpected regex patterns: %v", cfg.regexRaw)
	}
	if len(cfg.labelRaw) != 1 || cfg.labelRaw[0] != "env=prod,tier=backend" {
		t.Fatalf("unexpected label selectors: %v", cfg.labelRaw)
	}
}

func TestSplitNamespaceEntriesWithLabelBraces(t *testing.T) {
	entries := splitNamespaceEntries("label:{env=prod,tier=backend},team-a")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0] != "label:{env=prod,tier=backend}" || entries[1] != "team-a" {
		t.Fatalf("unexpected entries: %v", entries)
	}
}

func TestResolveWatchNamespaces(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Labels: map[string]string{"env": "prod"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-b", Labels: map[string]string{"env": "dev"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prod-1", Labels: map[string]string{"env": "prod"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "data", Labels: map[string]string{"env": "prod", "tier": "backend"}}},
	)

	cfg, err := parseWatchNamespaceConfig("team-*,regex:^prod-,label:{env=prod,tier=backend},explicit-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	selection, err := resolveWatchNamespaces(context.Background(), clientset, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selection.all {
		t.Fatalf("expected explicit selection, got watch-all")
	}

	expected := []string{"data", "explicit-ns", "prod-1", "team-a", "team-b"}
	if !reflect.DeepEqual(selection.namespaces, expected) {
		t.Fatalf("unexpected namespaces: %v", selection.namespaces)
	}
}

func TestWatchNamespaceSelectionEqual(t *testing.T) {
	left := watchNamespaceSelection{namespaces: []string{"a", "b"}}
	right := watchNamespaceSelection{namespaces: []string{"a", "b"}}
	if !watchNamespaceSelectionEqual(left, right) {
		t.Fatalf("expected selections to be equal")
	}
	if watchNamespaceSelectionEqual(left, watchNamespaceSelection{namespaces: []string{"b", "c"}}) {
		t.Fatalf("expected selections to differ")
	}
}
