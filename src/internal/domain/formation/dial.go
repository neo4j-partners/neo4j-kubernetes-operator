package formation

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
)

// Dial opens an admin Bolt session to the target's system-database leader, reusing the same
// NEO-004 TLS policy and delegated-auth-secret checks as the formation Reconciler. Satellite
// controllers (e.g. Neo4jRestore) that need admin Bolt but do not own the formation loop call
// this instead of duplicating the dial. Unlike the Reconciler's own path it emits any TLS
// warning directly on recorder (no advisory memo) — a restore is one-shot, not a hot loop.
func Dial(ctx context.Context, c client.Client, recorder record.EventRecorder, neo4j *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
	ctxRender := render.ClientServiceContext(neo4j)
	var secret corev1.Secret
	key := types.NamespacedName{Name: ctxRender.AuthSecretName(), Namespace: ctxRender.Namespace()}
	if err := c.Get(ctx, key, &secret); err != nil {
		return nil, fmt.Errorf("auth secret: %w", err)
	}
	if err := rendersecrets.RequireAuthSecretDelegated(&secret, neo4j); err != nil {
		return nil, err
	}
	user, pass, err := intneo4j.ParseAuthSecret(string(secret.Data["NEO4J_AUTH"]))
	if err != nil {
		return nil, err
	}
	opts, err := dialOpts(ctx, c, recorder, neo4j)
	if err != nil {
		return nil, err
	}
	return intneo4j.Connect(ctx, AdminBoltURI(neo4j), user, pass, opts)
}

// dialOpts builds NEO-004 connect options without the Reconciler's advisory memo. It reuses
// loadBoltRootCAs (verified TLS) and the same insecure/refuse fallbacks.
func dialOpts(ctx context.Context, c client.Client, recorder record.EventRecorder, neo4j *neo4jv1beta1.Neo4j) (intneo4j.ConnectOpts, error) {
	if BoltTLSEnabled(neo4j) {
		pool, err := loadBoltRootCAs(ctx, c, neo4j)
		if err != nil {
			return intneo4j.ConnectOpts{}, err
		}
		return intneo4j.ConnectOpts{RootCAs: pool}, nil
	}
	if neo4j.Spec.Trust != nil && neo4j.Spec.Trust.InsecureAdminConnection {
		if recorder != nil {
			recorder.Event(neo4j, corev1.EventTypeWarning, oracle.ReasonInsecureAdminConnection.String(),
				"operator admin Bolt is unencrypted (trust.insecureAdminConnection=true); prefer trust.certificates.bolt")
		}
		return intneo4j.ConnectOpts{AllowPlaintext: true}, nil
	}
	msg := "admin Bolt requires trust.certificates.bolt (verified TLS) or trust.insecureAdminConnection=true (NEO-004)"
	if recorder != nil {
		recorder.Event(neo4j, corev1.EventTypeWarning, oracle.ReasonAdminBoltTLSRequired.String(), msg)
	}
	return intneo4j.ConnectOpts{}, fmt.Errorf("%s", msg)
}
