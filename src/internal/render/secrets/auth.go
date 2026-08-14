package secrets

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// AuthKey is the only key the Neo4j image reads for bootstrap credentials, in the
// form "neo4j/<password>". Both the operator-generated Secret and a BYO Secret use it.
const AuthKey = "NEO4J_AUTH"

// ErrAuthValueRejected is a refusal sentinel (status.PipelineErrorReason) for an auth
// Secret the Neo4j image entrypoint cannot consume.
var ErrAuthValueRejected = errors.New("auth secret value rejected")

// authValuePattern mirrors the image entrypoint's own parse of NEO4J_AUTH
// (docker-neo4j set_initial_password): user and password may not contain "/", and an
// optional trailing "/true" asks Neo4j to force a password change.
var authValuePattern = regexp.MustCompile(`^([^/]+)/([^/]+)/?([tT][rR][uU][eE])?$`)

// RequireUsableAuthValue rejects an auth Secret whose value makes the Neo4j container
// crash-loop at boot instead of starting.
//
// The image entrypoint passes the password as a *positional* argument to
// `neo4j-admin dbms set-initial-password` without a "--" separator. Its CLI parser
// (picocli) treats a token starting with "-" as an option, so the positional parameter
// is never filled and the command exits with a usage error under `bash -eu`. The
// entrypoint also exits on a value it cannot parse, on a non-neo4j user, and on the
// default password. None of these are visible on the CR, hence this pre-flight: the
// caller turns the refusal into a stable reason plus a Warning Event.
//
// Messages never quote the secret value.
func RequireUsableAuthValue(secret *corev1.Secret) error {
	auth, ok := authValue(secret)
	if !ok {
		return fmt.Errorf("%w: %q has no %s key (expected %q)",
			ErrAuthValueRejected, secret.Name, AuthKey, "neo4j/<password>")
	}
	// "none" disables authentication in the image; the operator does not reject it here,
	// steps that need credentials fail on their own terms.
	if auth == "none" {
		return nil
	}

	match := authValuePattern.FindStringSubmatch(auth)
	if match == nil {
		return fmt.Errorf("%w: %q key %s must be \"neo4j/<password>\" and the password must not contain %q",
			ErrAuthValueRejected, secret.Name, AuthKey, "/")
	}
	user, password := match[1], match[2]

	if user != "neo4j" {
		return fmt.Errorf("%w: %q key %s names user %q; the Neo4j image only accepts %q",
			ErrAuthValueRejected, secret.Name, AuthKey, user, "neo4j")
	}
	if password == "neo4j" {
		return fmt.Errorf("%w: %q key %s uses the default password, which the Neo4j image refuses",
			ErrAuthValueRejected, secret.Name, AuthKey)
	}
	if strings.HasPrefix(password, "-") {
		return fmt.Errorf("%w: %q key %s has a password starting with %q, which the image entrypoint passes as an option to neo4j-admin instead of a value",
			ErrAuthValueRejected, secret.Name, AuthKey, "-")
	}
	return nil
}

// authValue reads the credentials from either representation: the API server merges
// StringData into Data, but a Secret the operator just rendered only has StringData.
func authValue(secret *corev1.Secret) (string, bool) {
	if value, ok := secret.Data[AuthKey]; ok {
		return string(value), true
	}
	value, ok := secret.StringData[AuthKey]
	return value, ok
}
