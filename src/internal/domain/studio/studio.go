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
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderstudio "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/studio"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

// assetStoreArchitectRole is the built-in role Enterprise Studio's asset-store account needs (NES
// prerequisites). A fixed literal — it is interpolated into GRANT ROLE (a role cannot be a parameter),
// so it must never come from user input.
const assetStoreArchitectRole = "architect"

// ResolveAssetStore returns the Bolt URI NES uses to reach the asset store: an external uri verbatim,
// or an in-cluster neo4jRef resolved to the Neo4j's Bolt client Service URI (neo4j://<svc>:<boltPort>
// — customer feedback). A missing neo4jRef is a retryable TargetNotFound; a community target is
// terminal, matching ResolveDeployments. The Deployment field carries "assetStore" for error context.
func ResolveAssetStore(ctx context.Context, c client.Client, s *neo4jv1.Neo4jStudio) (string, *ResolveError) {
	as := s.Spec.AssetStore
	if as.URI != "" || as.Neo4jRef == nil {
		return as.URI, nil
	}
	var neo4j neo4jv1.Neo4j
	key := types.NamespacedName{Name: as.Neo4jRef.Name, Namespace: s.Namespace}
	if err := c.Get(ctx, key, &neo4j); err != nil {
		if apierrors.IsNotFound(err) {
			return "", &ResolveError{Deployment: "assetStore", Kind: ResolveTargetNotFound,
				Detail: fmt.Sprintf("assetStore: Neo4j %q not found in namespace %q", as.Neo4jRef.Name, s.Namespace)}
		}
		return "", &ResolveError{Deployment: "assetStore", Kind: ResolveTargetNotFound, Detail: err.Error()}
	}
	if neo4j.Spec.Edition != neo4jv1.EditionEnterprise {
		return "", &ResolveError{Deployment: "assetStore", Kind: ResolveEditionUnsupported,
			Detail: fmt.Sprintf("assetStore: Neo4j %q is %s; the asset store must be Enterprise", neo4j.Name, neo4j.Spec.Edition)}
	}
	rc := render.StandaloneContext(&neo4j)
	// Match the target's Bolt listener. Plaintext by default; when bolt TLS is enabled the listener is
	// REQUIRED-TLS, so a neo4j:// dial is refused (NES reports "retrieve routing table … EOF"). If the
	// target's CA is resolvable the operator mounts it into the Studio pod (CollectTrustCAs +
	// SSL_CERT_DIR), so NES can fully verify — use neo4j+s. Otherwise fall back to +ssc (encrypted,
	// verification skipped), mirroring render/backup/metadata.
	scheme := "neo4j"
	if rendertrust.BoltTLSEnabled(&neo4j) {
		scheme = "neo4j+ssc"
		if len(rendertrust.TrustAnchorKeys(&neo4j, "bolt")) > 0 {
			scheme = "neo4j+s"
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%d", scheme, rc.ClientServiceName(), neo4j.Namespace, rc.BoltPort()), nil
}

// CollectTrustCAs gathers the CA trust anchors NES must trust to verify the TLS of the in-cluster
// neo4jRef targets this Studio dials: each deployment's https connector (only when the target exposes
// https — the same test httpURI uses) and the asset store's bolt listener (only under bolt TLS).
// External uri targets are skipped — the operator has no handle on their CA (BYO via config). The
// anchors are projected into the pod and exposed via SSL_CERT_DIR by render/studio. Deduped by
// Secret+key across targets; a vanished target is skipped (ResolveDeployments already gates existence).
func CollectTrustCAs(ctx context.Context, c client.Client, s *neo4jv1.Neo4jStudio) ([]rendertrust.SecretKeyNeed, error) {
	seen := map[string]struct{}{}
	var out []rendertrust.SecretKeyNeed
	add := func(needs []rendertrust.SecretKeyNeed) {
		for _, n := range needs {
			id := n.SecretName + "\x00" + n.Key
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, n)
		}
	}
	cache := map[string]*neo4jv1.Neo4j{}
	get := func(name string) (*neo4jv1.Neo4j, error) {
		if n, ok := cache[name]; ok {
			return n, nil
		}
		var n neo4jv1.Neo4j
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: s.Namespace}, &n); err != nil {
			if apierrors.IsNotFound(err) {
				cache[name] = nil
				return nil, nil
			}
			return nil, err
		}
		cache[name] = &n
		return &n, nil
	}

	for _, d := range s.Spec.Deployments {
		if d.URI != "" || d.Neo4jRef == nil {
			continue
		}
		n, err := get(d.Neo4jRef.Name)
		if err != nil {
			return nil, err
		}
		if n != nil && httpsExposed(n) {
			add(rendertrust.TrustAnchorKeys(n, "https"))
		}
	}
	if as := s.Spec.AssetStore; as.URI == "" && as.Neo4jRef != nil {
		n, err := get(as.Neo4jRef.Name)
		if err != nil {
			return nil, err
		}
		if n != nil && rendertrust.BoltTLSEnabled(n) {
			add(rendertrust.TrustAnchorKeys(n, "bolt"))
		}
	}
	return out, nil
}

// httpsExposed reports whether the backing Neo4j publishes the https connector on its client Service
// (the condition httpURI uses to choose https) — so Studio only mounts a deployment's CA when it will
// actually dial it over TLS.
func httpsExposed(neo4j *neo4jv1.Neo4j) bool {
	for _, conn := range render.StandaloneContext(neo4j).ClientExpose() {
		if conn == render.ConnectorHTTPS {
			return true
		}
	}
	return false
}

// ProvisionAssetStore creates the asset database, the architect account (with the given password), and
// the architect role grant on the backing Neo4j via admin Bolt — idempotently (BDR-015 §4 in-cluster
// provisioning, customer feedback). The caller supplies an admin session (formation.Dial) and the
// architect credentials read from assetStore.auth; the password travels as a Bolt parameter.
func ProvisionAssetStore(ctx context.Context, admin intneo4j.Admin, database, username, password string) error {
	if database == "" || username == "" || password == "" {
		return fmt.Errorf("assetStore provisioning needs a database name and architect username/password")
	}
	if err := admin.CreateDatabaseIfNotExists(ctx, database); err != nil {
		return fmt.Errorf("create asset database %q: %w", database, err)
	}
	if err := admin.CreateUserIfNotExists(ctx, username, password); err != nil {
		return fmt.Errorf("create asset-store user %q: %w", username, err)
	}
	if err := admin.GrantRole(ctx, assetStoreArchitectRole, username); err != nil {
		return fmt.Errorf("grant %s to %q: %w", assetStoreArchitectRole, username, err)
	}
	return nil
}

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
		// An empty mode defaults to ReverseProxy (the in-cluster-safe choice — BDR-015 usability
		// revision). CRD defaulting fills this in-cluster; normalise here so in-code callers (unit
		// tests bypassing defaulting) and NeedsSessionAffinity see a concrete mode.
		mode := d.Mode
		if mode == "" {
			mode = neo4jv1.StudioModeReverseProxy
		}
		if d.URI != "" {
			rd := renderstudio.ResolvedDeployment{Name: d.Name, URI: d.URI, Mode: string(mode)}
			// External Direct: NES needs publicUri set when directClientQuery is on. Use the explicit
			// override if given, else the uri itself (the external endpoint is already browser-reachable).
			if mode == neo4jv1.StudioModeDirect {
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
		if perr := validatePool(&neo4j, d.Name, d.Neo4jRef.Pool); perr != nil {
			return nil, perr
		}
		// uri is always the server-reachable HTTP(S) API (NES uses it to proxy/validate); Direct adds
		// the browser-reachable Bolt endpoint as publicUri. An optional neo4jRef.pool targets a cluster
		// pool's Service (GDS analytics — customer feedback). Reuses the workload's own Service
		// naming/ports (render.Context) rather than re-deriving them (ADR-018).
		rc := render.StandaloneContext(&neo4j)
		host, useFacade := backingHost(&neo4j, d.Neo4jRef.Pool)
		rd := renderstudio.ResolvedDeployment{Name: d.Name, URI: httpURI(rc, host, useFacade), Mode: string(mode)}
		if mode == neo4jv1.StudioModeDirect {
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

// backingHost returns the in-cluster Service DNS Studio uses to reach the backing Neo4j, plus whether
// the port should come from the client Service facade (vs. the raw listener port). Default is the
// north-south client Service. An optional neo4jRef.pool (analytics/read) targets that pool's headless
// Service in a cluster — the north-south Service only fronts primaries, so GDS analytics/read members
// are only reachable via their own pool Service (customer feedback). Ignored for a standalone.
//
// The client Service uses the short <svc>.<ns>.svc form (the operator's TLS cert carries both the
// short and FQDN SANs for it — trust.inClusterNames). A pool headless Service is only a cert SAN in
// its FQDN form (<svc>.<ns>.svc.<clusterDomain>), so Studio must dial the pool by FQDN or HTTPS
// certificate verification fails (HeadlessServiceDomain uses the same ClusterDomain as the cert SAN).
func backingHost(neo4j *neo4jv1.Neo4j, pool string) (host string, useFacade bool) {
	rc := render.StandaloneContext(neo4j)
	host, useFacade = fmt.Sprintf("%s.%s.svc", rc.ClientServiceName(), neo4j.Namespace), true
	if render.IsClusterMode(neo4j) {
		switch pool {
		case string(render.PoolAnalytics), string(render.PoolRead):
			pc := render.NewContext(neo4j, render.PoolID(pool))
			host, useFacade = pc.HeadlessServiceDomain(), false
		}
	}
	return host, useFacade
}

// httpURI builds the HTTP(S) endpoint for a resolved host. It follows whatever the Neo4j publishes:
// HTTPS when the https connector is exposed (the operator no longer hard-codes plain http://…:7474 —
// customer feedback), otherwise HTTP. The north-south client Service uses facade (remappable) ports;
// a pool headless Service exposes the raw listener ports. For TLS, the backing Neo4j must expose the
// https connector and NES must trust its certificate (wired via spec.config / configFromSecrets).
func httpURI(rc render.Context, host string, useFacade bool) string {
	scheme, conn := "http", render.ConnectorHTTP
	for _, c := range rc.ClientExpose() {
		if c == render.ConnectorHTTPS {
			scheme, conn = "https", render.ConnectorHTTPS
			break
		}
	}
	port := rc.ListenerPort(conn)
	if useFacade {
		port = rc.ServiceFacadePort(conn)
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

// validatePool fails fast when neo4jRef.pool targets a cluster pool that is not configured, so a
// mistargeted deployment surfaces a clear error instead of resolving to a dead Service DNS. The pool
// is only honoured for a cluster (ignored for a standalone — see backingHost); primary always exists.
func validatePool(neo4j *neo4jv1.Neo4j, deployment, pool string) *ResolveError {
	if pool == "" || !render.IsClusterMode(neo4j) ||
		(pool != string(render.PoolAnalytics) && pool != string(render.PoolRead)) {
		return nil
	}
	if render.NewContext(neo4j, render.PoolID(pool)).PoolReplicas() == 0 {
		return &ResolveError{Deployment: deployment, Kind: ResolveTargetNotFound,
			Detail: fmt.Sprintf("deployment %q: Neo4j %q has no %s pool (neo4jRef.pool)", deployment, neo4j.Name, pool)}
	}
	return nil
}

// Apply renders and server-side applies the Studio object set, each owner-referenced to the
// Neo4jStudio so deletion GCs them (ADR-018 — no finalizer). Follows the house apply pattern: a bare
// object keyed by name/namespace plus a mutate that stamps the desired spec, so shared.Apply's
// create-or-update detects drift and updates existing objects (a no-op mutate would only ever create).
func Apply(ctx context.Context, c client.Client, scheme *runtime.Scheme, s *neo4jv1.Neo4jStudio, resolved []renderstudio.ResolvedDeployment, caNeeds []rendertrust.SecretKeyNeed) error {
	saDesired := renderstudio.ServiceAccount(s)
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saDesired.Name, Namespace: saDesired.Namespace}}
	if err := shared.Apply(ctx, c, scheme, s, sa, func() error {
		sa.Labels = saDesired.Labels
		return nil
	}); err != nil {
		return err
	}

	depDesired := renderstudio.Deployment(s, resolved, caNeeds)
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
