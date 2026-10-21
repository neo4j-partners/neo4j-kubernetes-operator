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

// Package studio applies the Enterprise Studio object set and resolves served-deployment URIs
// (ADR-018). It resolves an in-cluster neo4jRef to the workload's client Service URI using the same
// naming the Neo4j workload renders (render.Context), never a re-derived guess.
package studio

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderstudio "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/studio"
)

// ResolveErrorKind classifies a deployment-resolution failure so the reconciler can map it to the
// right catalogued reason (oracle).
type ResolveErrorKind int

const (
	// ResolveTargetNotFound — an in-cluster neo4jRef does not resolve to a Neo4j yet (retryable).
	ResolveTargetNotFound ResolveErrorKind = iota
	// ResolveEditionUnsupported — the referenced Neo4j is community (terminal).
	ResolveEditionUnsupported
)

// ResolveError is a typed resolution failure for one served deployment.
type ResolveError struct {
	Deployment string
	Kind       ResolveErrorKind
	Detail     string
}

func (e *ResolveError) Error() string { return e.Detail }

// ResolveDeployments turns every spec.deployments[] entry into a resolved client URI: an external
// uri is used verbatim; an in-cluster neo4jRef is resolved to the workload's client Service URI
// (ADR-018). It returns a typed *ResolveError on the first in-cluster target that is missing or
// not Enterprise.
func ResolveDeployments(ctx context.Context, c client.Client, s *neo4jv1.Neo4jStudio) ([]renderstudio.ResolvedDeployment, *ResolveError) {
	out := make([]renderstudio.ResolvedDeployment, 0, len(s.Spec.Deployments))
	for _, d := range s.Spec.Deployments {
		if d.URI != "" {
			rd := renderstudio.ResolvedDeployment{Name: d.Name, URI: d.URI, Mode: string(d.Mode)}
			// External Direct: NES needs publicUri set when directClientQuery is on. Use the explicit
			// override if given, else the uri itself (the external endpoint is already browser-reachable).
			if d.Mode == neo4jv1.StudioModeDirect {
				rd.PublicURI = d.PublicURI
				if rd.PublicURI == "" {
					rd.PublicURI = d.URI
				}
			}
			out = append(out, rd)
			continue
		}
		var neo4j neo4jv1.Neo4j
		key := types.NamespacedName{Name: d.Neo4jRef.Name, Namespace: s.Namespace}
		if err := c.Get(ctx, key, &neo4j); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, &ResolveError{Deployment: d.Name, Kind: ResolveTargetNotFound,
					Detail: fmt.Sprintf("deployment %q: Neo4j %q not found in namespace %q", d.Name, d.Neo4jRef.Name, s.Namespace)}
			}
			return nil, &ResolveError{Deployment: d.Name, Kind: ResolveTargetNotFound, Detail: err.Error()}
		}
		if neo4j.Spec.Edition != neo4jv1.EditionEnterprise {
			return nil, &ResolveError{Deployment: d.Name, Kind: ResolveEditionUnsupported,
				Detail: fmt.Sprintf("deployment %q: Neo4j %q is %s; Enterprise Studio serves Enterprise deployments", d.Name, neo4j.Name, neo4j.Spec.Edition)}
		}
		// uri is always the server-reachable HTTP API (NES uses it to proxy/validate); Direct adds the
		// browser-reachable Bolt endpoint as publicUri. Reuses the workload's own Service naming/ports
		// (render.Context) rather than re-deriving them (ADR-018).
		rc := render.StandaloneContext(&neo4j)
		host := fmt.Sprintf("%s.%s.svc", rc.ClientServiceName(), neo4j.Namespace)
		rd := renderstudio.ResolvedDeployment{Name: d.Name, URI: fmt.Sprintf("http://%s:%d", host, rc.HTTPPort()), Mode: string(d.Mode)}
		if d.Mode == neo4jv1.StudioModeDirect {
			// Browser-reachable Bolt: explicit override wins, else the in-cluster Service DNS.
			rd.PublicURI = d.PublicURI
			if rd.PublicURI == "" {
				rd.PublicURI = fmt.Sprintf("bolt://%s:%d", host, rc.BoltPort())
			}
		}
		out = append(out, rd)
	}
	return out, nil
}

// Apply renders and server-side applies the Studio object set, each owner-referenced to the
// Neo4jStudio so deletion GCs them (ADR-018 — no finalizer). Follows the house apply pattern: a bare
// object keyed by name/namespace plus a mutate that stamps the desired spec, so shared.Apply's
// create-or-update detects drift and updates existing objects (a no-op mutate would only ever create).
func Apply(ctx context.Context, c client.Client, scheme *runtime.Scheme, s *neo4jv1.Neo4jStudio, resolved []renderstudio.ResolvedDeployment) error {
	saDesired := renderstudio.ServiceAccount(s)
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saDesired.Name, Namespace: saDesired.Namespace}}
	if err := shared.Apply(ctx, c, scheme, s, sa, func() error {
		sa.Labels = saDesired.Labels
		return nil
	}); err != nil {
		return err
	}

	depDesired := renderstudio.Deployment(s, resolved)
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: depDesired.Name, Namespace: depDesired.Namespace}}
	if err := shared.Apply(ctx, c, scheme, s, dep, func() error {
		dep.Labels = depDesired.Labels
		dep.Spec = depDesired.Spec
		return nil
	}); err != nil {
		return err
	}

	svcDesired := renderstudio.Service(s, resolved)
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: svcDesired.Name, Namespace: svcDesired.Namespace}}
	if err := shared.Apply(ctx, c, scheme, s, svc, func() error {
		svc.Labels = svcDesired.Labels
		svc.Annotations = svcDesired.Annotations
		preserveServiceServerFields(&svc.Spec, &svcDesired.Spec)
		svc.Spec = svcDesired.Spec
		return nil
	}); err != nil {
		return err
	}

	if ingDesired := renderstudio.Ingress(s, resolved); ingDesired != nil {
		ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: ingDesired.Name, Namespace: ingDesired.Namespace}}
		if err := shared.Apply(ctx, c, scheme, s, ing, func() error {
			ing.Labels = ingDesired.Labels
			ing.Annotations = ingDesired.Annotations
			ing.Spec = ingDesired.Spec
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// preserveServiceServerFields carries API-server-assigned Service fields (clusterIP, IP families,
// session affinity defaults) from the live Spec onto the desired Spec, so an update does not try to
// clear immutable fields (mirrors domain/connectivity).
func preserveServiceServerFields(live, desired *corev1.ServiceSpec) {
	if desired.ClusterIP == "" {
		desired.ClusterIP = live.ClusterIP
	}
	if len(desired.ClusterIPs) == 0 {
		desired.ClusterIPs = live.ClusterIPs
	}
	if len(desired.IPFamilies) == 0 {
		desired.IPFamilies = live.IPFamilies
	}
	if desired.IPFamilyPolicy == nil {
		desired.IPFamilyPolicy = live.IPFamilyPolicy
	}
	if desired.SessionAffinity == "" {
		desired.SessionAffinity = live.SessionAffinity
		desired.SessionAffinityConfig = live.SessionAffinityConfig
	}
	if desired.InternalTrafficPolicy == nil {
		desired.InternalTrafficPolicy = live.InternalTrafficPolicy
	}
}
