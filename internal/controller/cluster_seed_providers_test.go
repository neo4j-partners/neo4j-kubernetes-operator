/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestEnsureClusterHasURLSeedProvider(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("clientgoscheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("neo4j scheme: %v", err)
	}

	cases := []struct {
		name            string
		config          map[string]string
		annotations     map[string]string
		wantAutoEnabled bool
		wantErrSubstr   string
		wantFinalValue  string
	}{
		{
			name:            "already includes URLConnectionSeedProvider → no-op",
			config:          map[string]string{SeedFromURIProvidersConfigKey: "CloudSeedProvider,URLConnectionSeedProvider"},
			wantAutoEnabled: false,
			wantFinalValue:  "CloudSeedProvider,URLConnectionSeedProvider",
		},
		{
			name:            "URLConnectionSeedProvider alone → no-op (still listed)",
			config:          map[string]string{SeedFromURIProvidersConfigKey: "URLConnectionSeedProvider"},
			wantAutoEnabled: false,
			wantFinalValue:  "URLConnectionSeedProvider",
		},
		{
			name:          "missing + no annotation → actionable error",
			config:        map[string]string{SeedFromURIProvidersConfigKey: "CloudSeedProvider"},
			annotations:   nil,
			wantErrSubstr: "does not enable URLConnectionSeedProvider",
		},
		{
			name:          "nil config + no annotation → actionable error",
			config:        nil,
			annotations:   nil,
			wantErrSubstr: "does not enable URLConnectionSeedProvider",
		},
		{
			name:            "missing + annotation → auto-enable, default preserved",
			config:          map[string]string{SeedFromURIProvidersConfigKey: "CloudSeedProvider"},
			annotations:     map[string]string{AutoEnableURLSeedProviderAnnotation: "true"},
			wantAutoEnabled: true,
			wantFinalValue:  "CloudSeedProvider,URLConnectionSeedProvider",
		},
		{
			name:            "nil config + annotation → auto-enable adds default + URL",
			config:          nil,
			annotations:     map[string]string{AutoEnableURLSeedProviderAnnotation: "true"},
			wantAutoEnabled: true,
			wantFinalValue:  "CloudSeedProvider,URLConnectionSeedProvider",
		},
		{
			name:            "preserves multiple existing providers when extending",
			config:          map[string]string{SeedFromURIProvidersConfigKey: "CloudSeedProvider, S3SeedProvider , FileSeedProvider"},
			annotations:     map[string]string{AutoEnableURLSeedProviderAnnotation: "true"},
			wantAutoEnabled: true,
			wantFinalValue:  "CloudSeedProvider,S3SeedProvider,FileSeedProvider,URLConnectionSeedProvider",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "ec",
					Namespace:   "default",
					Annotations: tc.annotations,
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{Config: tc.config},
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()

			autoEnabled, err := EnsureClusterHasURLSeedProvider(context.Background(), c, cluster)

			if tc.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if autoEnabled != tc.wantAutoEnabled {
				t.Errorf("autoEnabled = %v, want %v", autoEnabled, tc.wantAutoEnabled)
			}
			if got := cluster.Spec.Config[SeedFromURIProvidersConfigKey]; got != tc.wantFinalValue {
				t.Errorf("final config = %q, want %q", got, tc.wantFinalValue)
			}
		})
	}
}

func TestEnsureClusterHasURLSeedProvider_ActionableErrorIncludesSnippet(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "ec", Namespace: "default"},
		Spec:       neo4jv1beta1.Neo4jEnterpriseClusterSpec{Config: map[string]string{SeedFromURIProvidersConfigKey: "CloudSeedProvider"}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()

	_, err := EnsureClusterHasURLSeedProvider(context.Background(), c, cluster)
	if err == nil {
		t.Fatal("expected actionable error")
	}
	for _, want := range []string{
		SeedFromURIProvidersConfigKey,
		"CloudSeedProvider,URLConnectionSeedProvider",
		AutoEnableURLSeedProviderAnnotation,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q\nfull: %s", want, err.Error())
		}
	}
}
