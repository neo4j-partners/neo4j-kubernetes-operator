package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	authnv1 "k8s.io/api/authentication/v1"
	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	authenticationv1 "k8s.io/client-go/kubernetes/typed/authentication/v1"
	authorizationv1 "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/rest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// metricsServerOptions builds controller-runtime metrics options (NEO-017).
// Bind "0" (or empty) disables the endpoint. Any other bind requires TLS plus
// Kubernetes authn/authz — plaintext :8080 via extraArgs is refused.
func metricsServerOptions(bind string, secure bool) (metricsserver.Options, error) {
	opts := metricsserver.Options{
		BindAddress:   bind,
		SecureServing: secure,
		TLSOpts: []func(*tls.Config){
			func(cfg *tls.Config) { cfg.MinVersion = tls.VersionTLS12 },
		},
	}
	if bind == "" || bind == "0" {
		opts.BindAddress = "0"
		return opts, nil
	}
	if !secure {
		return metricsserver.Options{}, fmt.Errorf(
			"metrics-bind-address %q requires --metrics-secure (NEO-017); enable via Helm metrics.enabled, not extraArgs",
			bind)
	}
	opts.FilterProvider = metricsAuthFilter
	return opts, nil
}

// ponytail: TokenReview + SubjectAccessReview via client-go, not
// filters.WithAuthenticationAndAuthorization — that package links k8s.io/apiserver
// (CEL, otel, grpc). Same ClusterRole contract; no in-process auth cache.
func metricsAuthFilter(config *rest.Config, httpClient *http.Client) (metricsserver.Filter, error) {
	authn, err := authenticationv1.NewForConfigAndClient(config, httpClient)
	if err != nil {
		return nil, err
	}
	authz, err := authorizationv1.NewForConfigAndClient(config, httpClient)
	if err != nil {
		return nil, err
	}
	return func(_ logr.Logger, next http.Handler) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := authorizeMetricsRequest(r, authn, authz); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		}), nil
	}, nil
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) < len(p) || !strings.EqualFold(h[:len(p)], p) {
		return "", false
	}
	t := strings.TrimSpace(h[len(p):])
	return t, t != ""
}

func authorizeMetricsRequest(r *http.Request, authn authenticationv1.AuthenticationV1Interface, authz authorizationv1.AuthorizationV1Interface) error {
	token, ok := bearerToken(r)
	if !ok {
		return fmt.Errorf("unauthorized")
	}
	tr, err := authn.TokenReviews().Create(r.Context(), &authnv1.TokenReview{
		Spec: authnv1.TokenReviewSpec{Token: token},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("unauthorized")
	}
	if !tr.Status.Authenticated {
		return fmt.Errorf("unauthorized")
	}
	extra := map[string]authzv1.ExtraValue{}
	for k, v := range tr.Status.User.Extra {
		extra[k] = authzv1.ExtraValue(v)
	}
	sar, err := authz.SubjectAccessReviews().Create(r.Context(), &authzv1.SubjectAccessReview{
		Spec: authzv1.SubjectAccessReviewSpec{
			NonResourceAttributes: &authzv1.NonResourceAttributes{
				Path: r.URL.Path,
				Verb: "get",
			},
			User:   tr.Status.User.Username,
			Groups: tr.Status.User.Groups,
			UID:    tr.Status.User.UID,
			Extra:  extra,
		},
	}, metav1.CreateOptions{})
	if err != nil || !sar.Status.Allowed {
		return fmt.Errorf("forbidden")
	}
	return nil
}
