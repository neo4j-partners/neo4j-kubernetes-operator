package serverconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/plugins"
)

// ConfigChecksumAnnotation triggers a rolling restart when server config changes (AC-NEO-CONFIG-CHANGE).
const ConfigChecksumAnnotation = "neo4j.com/config-checksum"

// ConfigChecksumEnv is set on the Neo4j container so pod template changes force a rollout.
const ConfigChecksumEnv = "NEO4J_OPERATOR_CONFIG_CHECKSUM"

// ConfigMap builds the neo4j.conf fragment ConfigMap for a pool (Helm parity: one key per setting).
func ConfigMap(ctx render.Context) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.ConfigMapName(),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("config"),
		},
		Data: neo4jConfData(ctx),
	}
}

// ApocConfigMap builds apoc.conf when APOC is assigned and spec.config.apoc is set.
func ApocConfigMap(ctx render.Context) *corev1.ConfigMap {
	if !HasApocConfig(ctx) {
		return nil
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.ApocConfigMapName(),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("config"),
		},
		Data: map[string]string{
			"apoc.conf": renderApocConf(ctx),
		},
	}
}

// HasApocConfig reports whether apoc.conf should be mounted for this pool.
func HasApocConfig(ctx render.Context) bool {
	return plugins.Assigned(ctx.PoolPluginIDs(), "apoc") && renderApocConf(ctx) != ""
}

func neo4jConfData(ctx render.Context) map[string]string {
	data := mergedNeo4jConf(ctx)
	if jvm := renderJVMConf(ctx); jvm != "" {
		data["server.jvm.additional"] = strings.TrimSuffix(jvm, "\n")
	}
	return data
}

// bootstrapOnlyKeys are settings Neo4j reads once, when the DBMS initialises, and that formation
// re-applies to a running cluster over Bolt (dbms.setDefaultAllocationNumbers, ADR-007). They stay
// in the ConfigMap so a member created later starts with the current value, but they are kept out
// of the checksum: both track the pool sizes, so hashing them would roll every member each time a
// pool is resized, to deliver a value Neo4j will not read again. On a single-primary cluster that
// restart is a full outage — for nothing. Same reasoning as render.Context.MinimumMembers, which
// refuses to track the pool for exactly this reason.
var bootstrapOnlyKeys = map[string]bool{
	"initial.dbms.default_primaries_count":   true,
	"initial.dbms.default_secondaries_count": true,
}

// ConfigChecksum returns a stable SHA-256 digest of rendered ConfigMap data.
func ConfigChecksum(ctx render.Context) string {
	data := neo4jConfData(ctx)
	keys := make([]string, 0, len(data))
	for k := range data {
		if bootstrapOnlyKeys[k] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(data[k]))
		h.Write([]byte{0})
	}
	if HasServerLogsXml(ctx) {
		h.Write([]byte(serverLogsFileName))
		h.Write([]byte{0})
		h.Write([]byte(ctx.Neo4j.Spec.Logging.ServerLogsXml))
		h.Write([]byte{0})
	} else if HasServerLogsConfigMapRef(ctx) {
		// External CM content is not hashed — changing the CM alone does not roll pods.
		h.Write([]byte("serverLogsConfigMapRef"))
		h.Write([]byte{0})
		h.Write([]byte(ServerLogsConfigMapName(ctx)))
		h.Write([]byte{0})
		h.Write([]byte(ServerLogsConfigMapKey(ctx)))
		h.Write([]byte{0})
	}
	if HasUserLogsXml(ctx) {
		h.Write([]byte(userLogsFileName))
		h.Write([]byte{0})
		h.Write([]byte(ctx.Neo4j.Spec.Logging.UserLogsXml))
		h.Write([]byte{0})
	} else if HasUserLogsConfigMapRef(ctx) {
		h.Write([]byte("userLogsConfigMapRef"))
		h.Write([]byte{0})
		h.Write([]byte(UserLogsConfigMapName(ctx)))
		h.Write([]byte{0})
		h.Write([]byte(UserLogsConfigMapKey(ctx)))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// neo4jDefaultJVMAdditional matches uncommented server.jvm.additional lines from
// helm-charts/neo4j/neo4j-enterprise.conf (same active set as community).
// Helm: neo4j.configJvmAdditionalYaml when jvm.useNeo4jDefaultJvmArguments is true.
var neo4jDefaultJVMAdditional = []string{
	"-XX:+UseG1GC",
	"-XX:-OmitStackTraceInFastThrow",
	"-XX:+AlwaysPreTouch",
	"-XX:+UnlockExperimentalVMOptions",
	"-XX:+TrustFinalNonStaticFields",
	"-XX:+DisableExplicitGC",
	"-Djdk.nio.maxCachedBufferSize=1024",
	"-Dio.netty.tryReflectionSetAccessible=true",
	"-Djdk.tls.ephemeralDHKeySize=2048",
	"-Djdk.tls.rejectClientInitiatedRenegotiation=true",
	"-XX:FlightRecorderOptions=stackdepth=256",
	"-XX:+UnlockDiagnosticVMOptions",
	"-XX:+DebugNonSafepoints",
	"--add-opens=java.base/java.nio=ALL-UNNAMED",
	"--add-opens=java.base/java.io=ALL-UNNAMED",
	"--add-opens=java.base/sun.nio.ch=ALL-UNNAMED",
	"-Dlog4j2.disable.jmx=true",
}

// CR fields whose rendering merges layers, and can therefore drop a value (render.Duplicate).
const (
	FieldJVMArguments = "spec.config.jvm.additionalArguments"
	FieldConfigNeo4j  = "spec.config.neo4j"
)

// Duplicates reports every value this package dropped while rendering the server config:
// JVM arguments colliding on the same flag (NEO-3-003-JVM-01) and neo4j.conf keys colliding
// across the defaults / plugin / user / injected layers (BDR-008). Empty when nothing collides.
func Duplicates(neo4j *neo4jv1beta1.Neo4j) []render.Duplicate {
	if neo4j == nil {
		return nil
	}
	var jvm *neo4jv1beta1.JVMSpec
	if neo4j.Spec.Config != nil {
		jvm = neo4j.Spec.Config.JVM
	}
	_, dups := mergeJVMArgs(jvm)

	// neo4j.conf is rendered per pool; a collision usually repeats across pools, report it once.
	seen := map[render.Duplicate]struct{}{}
	for _, pool := range render.ActivePools(neo4j) {
		_, poolDups := mergeNeo4jConf(render.ContextForPool(neo4j, pool))
		for _, d := range poolDups {
			if _, ok := seen[d]; ok {
				continue
			}
			seen[d] = struct{}{}
			dups = append(dups, d)
		}
	}
	return render.SortDuplicates(dups)
}

func renderJVMConf(ctx render.Context) string {
	var jvm *neo4jv1beta1.JVMSpec
	if ctx.Neo4j.Spec.Config != nil {
		jvm = ctx.Neo4j.Spec.Config.JVM
	}
	ordered, _ := mergeJVMArgs(jvm)
	if len(ordered) == 0 {
		return ""
	}
	return strings.Join(ordered, "\n") + "\n"
}

// mergeJVMArgs merges Neo4j defaults with additionalArguments (user wins, in place) and
// reports every argument dropped on the way.
func mergeJVMArgs(jvm *neo4jv1beta1.JVMSpec) ([]string, []render.Duplicate) {
	// CRD / Helm default: useDefaults is true when unset.
	useDefaults := jvm == nil || jvm.UseDefaults == nil || *jvm.UseDefaults
	var args []string
	if jvm != nil {
		args = jvm.AdditionalArguments
	}
	if !useDefaults && len(args) == 0 {
		return nil, nil
	}

	var ordered []string
	var dups []render.Duplicate
	indexByKey := map[string]int{}
	originByKey := map[string]string{}
	put := func(raw, origin string) {
		arg := normalizeJVMArg(raw)
		if arg == "" {
			return
		}
		key := jvmArgKey(arg)
		if i, ok := indexByKey[key]; ok {
			// An exact repeat loses nothing, so it is not worth reporting.
			if ordered[i] != arg {
				dups = append(dups, render.Duplicate{
					Field: FieldJVMArguments, Key: key,
					Kept: arg, KeptFrom: origin,
					Dropped: ordered[i], DroppedFrom: originByKey[key],
				})
			}
			ordered[i] = arg // later value wins; keep first-seen position
			originByKey[key] = origin
			return
		}
		indexByKey[key] = len(ordered)
		originByKey[key] = origin
		ordered = append(ordered, arg)
	}
	if useDefaults {
		for _, arg := range neo4jDefaultJVMAdditional {
			put(arg, render.OriginNeo4jDefault)
		}
	}
	for _, arg := range args {
		put(arg, render.OriginUser)
	}
	return ordered, dups
}

func normalizeJVMArg(arg string) string {
	arg = strings.TrimSpace(arg)
	arg = strings.TrimSpace(strings.TrimPrefix(arg, "server.jvm.additional="))
	return strings.TrimSpace(arg)
}

// jvmArgKey identifies a JVM flag for dedupe/override (value-agnostic).
func jvmArgKey(arg string) string {
	switch {
	case strings.HasPrefix(arg, "-XX:+") || strings.HasPrefix(arg, "-XX:-"):
		return "-XX:" + arg[len("-XX:+"):]
	case strings.HasPrefix(arg, "-XX:"):
		if i := strings.IndexByte(arg, '='); i >= 0 {
			return arg[:i]
		}
		return arg
	case strings.HasPrefix(arg, "-D"):
		if i := strings.IndexByte(arg, '='); i >= 0 {
			return arg[:i]
		}
		return arg
	case strings.HasPrefix(arg, "--add-opens="),
		strings.HasPrefix(arg, "--add-exports="),
		strings.HasPrefix(arg, "--add-reads="):
		// Keep module/package in the key so distinct opens don't collide.
		if i := strings.LastIndexByte(arg, '='); i >= 0 {
			return arg[:i]
		}
		return arg
	default:
		if i := strings.IndexByte(arg, '='); i >= 0 {
			return arg[:i]
		}
		return arg
	}
}

func renderApocConf(ctx render.Context) string {
	if ctx.Neo4j.Spec.Config == nil || ctx.Neo4j.Spec.Config.Apoc == nil || len(ctx.Neo4j.Spec.Config.Apoc) == 0 {
		return ""
	}
	keys := make([]string, 0, len(ctx.Neo4j.Spec.Config.Apoc))
	for k := range ctx.Neo4j.Spec.Config.Apoc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := []string{"# Generated by neo4j-operator"}
	for _, k := range keys {
		// APOC settings only — dbms.*/server.* belong in neo4j.conf (config.neo4j).
		// See https://neo4j.com/docs/apoc/current/config/
		if !strings.HasPrefix(k, "apoc.") {
			continue
		}
		// Values are validated by ValidateConfig (no newlines); join stays one setting per line.
		lines = append(lines, k+"="+ctx.Neo4j.Spec.Config.Apoc[k])
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// configKeyPattern is the allowlist for user-supplied neo4j.conf / apoc.conf keys (NEO-006).
var configKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidateConfig rejects config that enables shell/JVM injection or line-smuggling (NEO-006).
// Operator-owned expand-commands remain in rendered defaults; user values must not contain them.
func ValidateConfig(neo4j *neo4jv1beta1.Neo4j) error {
	if neo4j == nil {
		return nil
	}
	if neo4j.Spec.Config != nil {
		if err := validateStringMap("config.neo4j", neo4j.Spec.Config.Neo4j); err != nil {
			return err
		}
		if err := validateApocMap(neo4j.Spec.Config.Apoc); err != nil {
			return err
		}
		if neo4j.Spec.Config.JVM != nil {
			for i, arg := range neo4j.Spec.Config.JVM.AdditionalArguments {
				field := fmt.Sprintf("config.jvm.additionalArguments[%d]", i)
				if err := rejectUnsafeConfigValue(field, arg); err != nil {
					return err
				}
				if err := rejectDangerousJVMArg(field, arg); err != nil {
					return err
				}
			}
		}
	}
	if neo4j.Spec.Connectivity != nil && neo4j.Spec.Connectivity.ClusterDomain != "" {
		if err := rejectUnsafeConfigValue("connectivity.clusterDomain", neo4j.Spec.Connectivity.ClusterDomain); err != nil {
			return err
		}
	}
	for id, def := range neo4j.Spec.PluginDefinitions {
		if err := validateStringMap(fmt.Sprintf("pluginDefinitions[%q].config", id), def.Config); err != nil {
			return err
		}
	}
	return nil
}

func validateApocMap(m map[string]string) error {
	if m == nil {
		return nil
	}
	var bad []string
	for k, v := range m {
		if !strings.HasPrefix(k, "apoc.") {
			bad = append(bad, k)
			continue
		}
		if err := rejectUnsafeConfigKey("config.apoc", k); err != nil {
			return err
		}
		if err := rejectUnsafeConfigValue(fmt.Sprintf("config.apoc[%q]", k), v); err != nil {
			return err
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("config.apoc only accepts apoc.* keys for apoc.conf; move %v to config.neo4j (neo4j.conf)", bad)
}

func validateStringMap(field string, m map[string]string) error {
	for k, v := range m {
		if err := rejectUnsafeConfigKey(field, k); err != nil {
			return err
		}
		if err := rejectUnsafeConfigValue(fmt.Sprintf("%s[%q]", field, k), v); err != nil {
			return err
		}
	}
	return nil
}

func rejectUnsafeConfigKey(field, key string) error {
	if !configKeyPattern.MatchString(key) {
		return fmt.Errorf("%s key %q contains forbidden characters (NEO-006)", field, key)
	}
	return nil
}

func rejectUnsafeConfigValue(field, value string) error {
	switch {
	case strings.Contains(value, "$("):
		return fmt.Errorf("%s must not contain command substitution $(...) (NEO-006)", field)
	case strings.Contains(value, "`"):
		return fmt.Errorf("%s must not contain backticks (NEO-006)", field)
	case strings.ContainsAny(value, "\n\r"):
		return fmt.Errorf("%s must not contain newlines (NEO-006)", field)
	default:
		return nil
	}
}

func rejectDangerousJVMArg(field, raw string) error {
	arg := normalizeJVMArg(raw)
	lower := strings.ToLower(arg)
	for _, p := range []string{
		"-javaagent:",
		"-agentlib:",
		"-agentpath:",
		"-xx:onoutofmemoryerror=",
		"-xx:onerror=",
	} {
		if strings.HasPrefix(lower, p) {
			return fmt.Errorf("%s %q is not allowed (NEO-006)", field, arg)
		}
	}
	return nil
}
