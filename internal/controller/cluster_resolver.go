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
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/neo4j"
)

// ResolvedTarget is a thin holder for the result of looking up a clusterRef.
// Exactly one of Cluster / Standalone is non-nil when Found is true.
type ResolvedTarget struct {
	Found      bool
	Cluster    *neo4jv1beta1.Neo4jEnterpriseCluster
	Standalone *neo4jv1beta1.Neo4jEnterpriseStandalone
}

// IsStandalone reports whether the referenced target is a standalone deployment.
func (r ResolvedTarget) IsStandalone() bool { return r.Standalone != nil }

// AdminSecret returns the configured admin secret name for the resolved target.
func (r ResolvedTarget) AdminSecret() string {
	if r.Cluster != nil {
		return r.Cluster.Spec.Auth.AdminSecret
	}
	if r.Standalone != nil {
		return r.Standalone.Spec.Auth.AdminSecret
	}
	return ""
}

// IsReady reports whether the resolved target has reached its Ready phase.
// Cluster readiness is detected via the Ready condition; standalone via the
// status.Ready boolean (matching the existing convention from the database
// controller).
func (r ResolvedTarget) IsReady() bool {
	if r.Cluster != nil {
		for _, c := range r.Cluster.Status.Conditions {
			if c.Type == ConditionTypeReady && c.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}
	if r.Standalone != nil {
		return r.Standalone.Status.Ready
	}
	return false
}

// NewClient builds a Neo4j client appropriate for the resolved target.
func (r ResolvedTarget) NewClient(c client.Client) (*neo4j.Client, error) {
	switch {
	case r.Cluster != nil:
		return neo4j.NewClientForEnterprise(r.Cluster, c, r.Cluster.Spec.Auth.AdminSecret)
	case r.Standalone != nil:
		return neo4j.NewClientForEnterpriseStandalone(r.Standalone, c, r.Standalone.Spec.Auth.AdminSecret)
	default:
		return nil, fmt.Errorf("ResolvedTarget has neither Cluster nor Standalone")
	}
}

// EnqueueDependentsForClusterChange returns a handler.EventHandler that, when
// the named Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone changes,
// enqueues every CR in the same namespace whose `clusterRef` matches the
// changed object's name.
//
// Both cluster and standalone reconcilers update status repeatedly during
// formation (Pending → Forming → Ready, plus diagnostics flips). Without
// this watch, dependent controllers (Neo4jUser, Neo4jRole, Neo4jRoleBinding)
// only see those transitions on their next 30-second requeue, which can
// add minutes of perceived latency in CI. With it, the dependent reconcile
// runs immediately on each cluster status update.
//
// `list` must be a fresh, empty list of the dependent type
// (e.g. `&neo4jv1beta1.Neo4jUserList{}`); `extractClusterRef` extracts the
// `clusterRef` field from a single item.
func EnqueueDependentsForClusterChange(
	c client.Client,
	newList func() client.ObjectList,
	walk func(client.ObjectList, func(name string, namespace string, clusterRef string)),
) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		// The triggering object is a Neo4jEnterpriseCluster or
		// Neo4jEnterpriseStandalone. We only need its name and namespace.
		changedName := obj.GetName()
		changedNS := obj.GetNamespace()
		if changedName == "" || changedNS == "" {
			return nil
		}
		list := newList()
		if err := c.List(ctx, list, client.InNamespace(changedNS)); err != nil {
			return nil
		}
		var reqs []reconcile.Request
		walk(list, func(name, namespace, clusterRef string) {
			if clusterRef == changedName {
				reqs = append(reqs, reconcile.Request{
					NamespacedName: types.NamespacedName{Namespace: namespace, Name: name},
				})
			}
		})
		return reqs
	})
}

// ResolveClusterRef looks up a clusterRef in a given namespace. It first
// tries Neo4jEnterpriseCluster and falls back to Neo4jEnterpriseStandalone.
// Returns ResolvedTarget{Found:false} when neither exists.
func ResolveClusterRef(ctx context.Context, c client.Client, namespace, name string) (ResolvedTarget, error) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	key := types.NamespacedName{Namespace: namespace, Name: name}
	if err := c.Get(ctx, key, cluster); err == nil {
		return ResolvedTarget{Found: true, Cluster: cluster}, nil
	} else if !errors.IsNotFound(err) {
		return ResolvedTarget{}, fmt.Errorf("get Neo4jEnterpriseCluster %s/%s: %w", namespace, name, err)
	}

	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := c.Get(ctx, key, standalone); err == nil {
		return ResolvedTarget{Found: true, Standalone: standalone}, nil
	} else if !errors.IsNotFound(err) {
		return ResolvedTarget{}, fmt.Errorf("get Neo4jEnterpriseStandalone %s/%s: %w", namespace, name, err)
	}
	return ResolvedTarget{}, nil
}
