package serverconfig

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const (
	serverLogsFileName = "server-logs.xml"
	userLogsFileName   = "user-logs.xml"
	// Paths match subPath file mounts (cleaner than Helm's directory-mount quirk).
	serverLogsConfigPath = "/config/" + serverLogsFileName
	userLogsConfigPath   = "/config/" + userLogsFileName
)

// HasServerLogsConfig reports whether custom server Log4j should be mounted (LOG-02).
func HasServerLogsConfig(ctx render.Context) bool {
	return HasServerLogsXml(ctx) || HasServerLogsConfigMapRef(ctx)
}

// HasUserLogsConfig reports whether custom user Log4j should be mounted (LOG-02).
func HasUserLogsConfig(ctx render.Context) bool {
	return HasUserLogsXml(ctx) || HasUserLogsConfigMapRef(ctx)
}

// HasServerLogsXml reports inline server-logs.xml.
func HasServerLogsXml(ctx render.Context) bool {
	return ctx.Neo4j.Spec.Logging != nil && strings.TrimSpace(ctx.Neo4j.Spec.Logging.ServerLogsXml) != ""
}

// HasUserLogsXml reports inline user-logs.xml.
func HasUserLogsXml(ctx render.Context) bool {
	return ctx.Neo4j.Spec.Logging != nil && strings.TrimSpace(ctx.Neo4j.Spec.Logging.UserLogsXml) != ""
}

// HasServerLogsConfigMapRef reports an existing ConfigMap for server logs.
func HasServerLogsConfigMapRef(ctx render.Context) bool {
	return ctx.Neo4j.Spec.Logging != nil &&
		ctx.Neo4j.Spec.Logging.ServerLogsConfigMapRef != nil &&
		ctx.Neo4j.Spec.Logging.ServerLogsConfigMapRef.Name != ""
}

// HasUserLogsConfigMapRef reports an existing ConfigMap for user logs.
func HasUserLogsConfigMapRef(ctx render.Context) bool {
	return ctx.Neo4j.Spec.Logging != nil &&
		ctx.Neo4j.Spec.Logging.UserLogsConfigMapRef != nil &&
		ctx.Neo4j.Spec.Logging.UserLogsConfigMapRef.Name != ""
}

// ServerLogsConfigMapName resolves the ConfigMap name to mount for server logs.
func ServerLogsConfigMapName(ctx render.Context) string {
	if HasServerLogsConfigMapRef(ctx) {
		return ctx.Neo4j.Spec.Logging.ServerLogsConfigMapRef.Name
	}
	return ctx.ServerLogsConfigMapName()
}

// UserLogsConfigMapName resolves the ConfigMap name to mount for user logs.
func UserLogsConfigMapName(ctx render.Context) string {
	if HasUserLogsConfigMapRef(ctx) {
		return ctx.Neo4j.Spec.Logging.UserLogsConfigMapRef.Name
	}
	return ctx.UserLogsConfigMapName()
}

// ServerLogsConfigMapKey is the ConfigMap data key mounted as /config/server-logs.xml.
func ServerLogsConfigMapKey(ctx render.Context) string {
	if HasServerLogsConfigMapRef(ctx) {
		if k := strings.TrimSpace(ctx.Neo4j.Spec.Logging.ServerLogsConfigMapRef.Key); k != "" {
			return k
		}
	}
	return serverLogsFileName
}

// UserLogsConfigMapKey is the ConfigMap data key mounted as /config/user-logs.xml.
func UserLogsConfigMapKey(ctx render.Context) string {
	if HasUserLogsConfigMapRef(ctx) {
		if k := strings.TrimSpace(ctx.Neo4j.Spec.Logging.UserLogsConfigMapRef.Key); k != "" {
			return k
		}
	}
	return userLogsFileName
}

// ServerLogsConfigMap builds the operator-managed server-logs.xml ConfigMap (inline only).
func ServerLogsConfigMap(ctx render.Context) *corev1.ConfigMap {
	if !HasServerLogsXml(ctx) {
		return nil
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.ServerLogsConfigMapName(),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("config"),
		},
		Data: map[string]string{
			serverLogsFileName: ctx.Neo4j.Spec.Logging.ServerLogsXml,
		},
	}
}

// UserLogsConfigMap builds the operator-managed user-logs.xml ConfigMap (inline only).
func UserLogsConfigMap(ctx render.Context) *corev1.ConfigMap {
	if !HasUserLogsXml(ctx) {
		return nil
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.UserLogsConfigMapName(),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("config"),
		},
		Data: map[string]string{
			userLogsFileName: ctx.Neo4j.Spec.Logging.UserLogsXml,
		},
	}
}

// loggingNeo4jConfKeys points Neo4j at mounted Log4j XML when custom logging is set.
func loggingNeo4jConfKeys(ctx render.Context) map[string]string {
	keys := map[string]string{}
	if HasServerLogsConfig(ctx) {
		keys["server.logs.config"] = serverLogsConfigPath
	}
	if HasUserLogsConfig(ctx) {
		keys["server.logs.user.config"] = userLogsConfigPath
	}
	return keys
}

// ValidateLogging rejects inline+ref on the same side (defense in depth beside CEL).
func ValidateLogging(neo4j *neo4jv1beta1.Neo4j) error {
	if neo4j.Spec.Logging == nil {
		return nil
	}
	l := neo4j.Spec.Logging
	if strings.TrimSpace(l.ServerLogsXml) != "" && l.ServerLogsConfigMapRef != nil && l.ServerLogsConfigMapRef.Name != "" {
		return fmt.Errorf("provide logging.serverLogsXml or logging.serverLogsConfigMapRef, not both")
	}
	if strings.TrimSpace(l.UserLogsXml) != "" && l.UserLogsConfigMapRef != nil && l.UserLogsConfigMapRef.Name != "" {
		return fmt.Errorf("provide logging.userLogsXml or logging.userLogsConfigMapRef, not both")
	}
	return nil
}
