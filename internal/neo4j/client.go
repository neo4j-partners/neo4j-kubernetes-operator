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

// Package neo4j provides a client for interacting with Neo4j databases
package neo4j

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// Client represents a Neo4j cluster client with optimized connection management
type Client struct {
	driver            neo4j.DriverWithContext
	enterpriseCluster *neo4jv1beta1.Neo4jEnterpriseCluster
	credentials       *Credentials

	// Circuit breaker state
	circuitBreaker *CircuitBreaker

	// Connection pool metrics
	poolMetrics *ConnectionPoolMetrics

	// Mutex for thread-safe operations
	mutex sync.RWMutex
}

// CircuitBreaker implements the circuit breaker pattern for connection failures
type CircuitBreaker struct {
	failureCount    int
	lastFailureTime time.Time
	state           CircuitState
	mutex           sync.RWMutex

	// Configuration
	maxFailures      int
	resetTimeout     time.Duration
	halfOpenMaxCalls int
}

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	// CircuitClosed indicates the circuit is closed (normal operation)
	CircuitClosed CircuitState = iota
	// CircuitOpen indicates the circuit is open (failing fast)
	CircuitOpen
	// CircuitHalfOpen indicates the circuit is half-open (testing recovery)
	CircuitHalfOpen

	// Boolean string values
	TrueString = "true"
)

// ConnectionPoolMetrics tracks connection pool performance
type ConnectionPoolMetrics struct {
	TotalConnections   int64
	ActiveConnections  int64
	IdleConnections    int64
	ConnectionErrors   int64
	QueryExecutionTime time.Duration
	LastHealthCheck    time.Time
}

// newCircuitBreaker creates a new circuit breaker with default settings
func newCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:      5,
		resetTimeout:     30 * time.Second,
		halfOpenMaxCalls: 3,
		state:            CircuitClosed,
	}
}

// newConnectionPoolMetrics creates a new connection pool metrics tracker
func newConnectionPoolMetrics() *ConnectionPoolMetrics {
	return &ConnectionPoolMetrics{
		LastHealthCheck: time.Now(),
	}
}

// Credentials holds Neo4j authentication information
type Credentials struct {
	Username string
	Password string
}

// ClusterMember represents a Neo4j cluster member
type ClusterMember struct {
	ID       string
	Address  string
	Role     string
	Database string
	Health   string
}

// DatabaseInfo represents information about a Neo4j database
type DatabaseInfo struct {
	Name            string
	Status          string
	Default         bool
	Home            bool
	Role            string
	RequestedStatus string
}

// ServerInfo represents information about a Neo4j server
type ServerInfo struct {
	// ID is the Neo4j-internal UUID assigned to the server at first
	// bootstrap. Populated by ListServers (via `SHOW SERVERS YIELD
	// serverId, ...`); other code paths that constructed ServerInfo
	// without querying SHOW SERVERS may leave this empty.
	ID      string
	Name    string
	Address string
	State   string
	Health  string
	Hosting []string
}

// NewClientForPod creates a Neo4j client that connects to a specific pod
func NewClientForPod(cluster *neo4jv1beta1.Neo4jEnterpriseCluster, k8sClient client.Client, adminSecretName, podURL string) (*Client, error) {
	// Get credentials from secret
	credentials, err := getCredentials(context.Background(), k8sClient, cluster.Namespace, adminSecretName)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}

	// Configure driver with optimized settings for split-brain detection
	auth := neo4j.BasicAuth(credentials.Username, credentials.Password, "")
	driverConfig := func(c *config.Config) {
		// Shorter timeouts for split-brain detection
		c.MaxConnectionLifetime = 5 * time.Minute
		c.MaxConnectionPoolSize = 5                       // Small pool for detection queries
		c.ConnectionAcquisitionTimeout = 10 * time.Second // Faster timeout for detection
		c.SocketConnectTimeout = 5 * time.Second          // Quick connection for health checks
		c.SocketKeepalive = true
		c.ConnectionLivenessCheckTimeout = 5 * time.Second

		// Configure TLS if enabled
		if tlsCfg := buildTLSConfig(context.Background(), k8sClient, cluster.Namespace, cluster.Name, cluster.Spec.TLS); tlsCfg != nil {
			c.TlsConfig = tlsCfg
		}
	}

	// Create driver with pod-specific URL
	driver, err := neo4j.NewDriverWithContext(podURL, auth, driverConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	return &Client{
		driver:            driver,
		enterpriseCluster: cluster,
		credentials:       credentials,
		circuitBreaker:    newCircuitBreaker(),
		poolMetrics:       newConnectionPoolMetrics(),
	}, nil
}

// buildTLSConfig creates a TLS configuration for Neo4j connections.
//
// Resolution order:
//  1. If tlsSpec.TrustedCASecret is set, load CA cert from that Secret (user override).
//  2. Auto-discover CA cert from the cert-manager-generated Secret ("{resourceName}-tls-secret").
//  3. Fall back to InsecureSkipVerify: true only if no CA cert can be loaded.
//
// This ensures proper TLS verification by default when cert-manager provides a CA,
// while remaining compatible with development setups where CA certs may not be available.
func buildTLSConfig(ctx context.Context, k8sClient client.Client, namespace, resourceName string, tlsSpec *neo4jv1beta1.TLSSpec) *tls.Config {
	if tlsSpec == nil || tlsSpec.Mode != "cert-manager" {
		return nil
	}

	// Try loading CA cert from secrets in priority order:
	// 1. User-specified TrustedCASecret (explicit override)
	// 2. Auto-discovered cert-manager Secret ({name}-tls-secret)
	secretNames := []string{}
	if tlsSpec.TrustedCASecret != "" {
		secretNames = append(secretNames, tlsSpec.TrustedCASecret)
	}
	secretNames = append(secretNames, fmt.Sprintf("%s-tls-secret", resourceName))

	for _, secretName := range secretNames {
		secret := &corev1.Secret{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      secretName,
			Namespace: namespace,
		}, secret)
		if err != nil {
			continue
		}
		if caCert, ok := secret.Data["ca.crt"]; ok && len(caCert) > 0 {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(caCert) {
				return &tls.Config{
					RootCAs:    pool,
					MinVersion: tls.VersionTLS12,
				}
			}
		}
	}

	// Fallback: no CA cert available — skip verification for self-signed certificates.
	// This path is reached during initial startup (before cert-manager issues the cert)
	// or when the issuer doesn't provide ca.crt in the Secret.
	// The operator reconciler will retry, and once cert-manager issues the cert, the
	// proper CA-verified path (above) will be used on subsequent reconciliations.
	// codeql[go/disabled-certificate-check]: Intentional fallback for transient startup window before cert-manager issues certs
	return &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // Fallback when CA cert unavailable
	}
}

// NewClientForEnterpriseStandalone creates a new optimized Neo4j client for standalone deployments
func NewClientForEnterpriseStandalone(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone, k8sClient client.Client, adminSecretName string) (*Client, error) {
	// Get credentials from secret
	credentials, err := getCredentials(context.Background(), k8sClient, standalone.Namespace, adminSecretName)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}

	// Build connection URI
	uri := buildConnectionURIForStandalone(standalone)

	// Configure optimized driver with connection pooling
	auth := neo4j.BasicAuth(credentials.Username, credentials.Password, "")

	config := func(c *config.Config) {
		// Optimized connection pool settings for standalone
		c.MaxConnectionLifetime = 30 * time.Minute
		c.MaxConnectionPoolSize = 10                      // Reduced for standalone deployment
		c.ConnectionAcquisitionTimeout = 30 * time.Second // Increased for Neo4j initialization time
		c.SocketConnectTimeout = 15 * time.Second         // Increased for Neo4j startup time
		c.SocketKeepalive = true
		c.ConnectionLivenessCheckTimeout = 10 * time.Second

		// Connection pool optimization
		c.MaxTransactionRetryTime = 30 * time.Second
		c.FetchSize = 1000 // Optimized fetch size for memory efficiency

		// Configure TLS if enabled
		if tlsCfg := buildTLSConfig(context.Background(), k8sClient, standalone.Namespace, standalone.Name, standalone.Spec.TLS); tlsCfg != nil {
			c.TlsConfig = tlsCfg
		}
	}

	// Create driver with retry logic for connection establishment
	driver, err := neo4j.NewDriverWithContext(uri, auth, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	// Test driver connectivity with proper timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = driver.VerifyConnectivity(ctx)
	if err != nil {
		driver.Close(context.Background())
		return nil, fmt.Errorf("failed to verify Neo4j connectivity: %w", err)
	}

	// Initialize circuit breaker
	circuitBreaker := &CircuitBreaker{
		maxFailures:      5,
		resetTimeout:     30 * time.Second,
		halfOpenMaxCalls: 3,
		state:            CircuitClosed,
	}

	// Initialize connection pool metrics
	poolMetrics := &ConnectionPoolMetrics{
		LastHealthCheck: time.Now(),
	}

	client := &Client{
		driver:            driver,
		enterpriseCluster: nil, // No cluster for standalone
		credentials:       credentials,
		circuitBreaker:    circuitBreaker,
		poolMetrics:       poolMetrics,
	}

	return client, nil
}

func NewClientForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster, k8sClient client.Client, adminSecretName string) (*Client, error) {
	// Get credentials from secret
	credentials, err := getCredentials(context.Background(), k8sClient, cluster.Namespace, adminSecretName)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}

	// Build connection URI
	uri := buildConnectionURIForEnterprise(cluster)

	// Configure optimized driver with connection pooling
	auth := neo4j.BasicAuth(credentials.Username, credentials.Password, "")

	config := func(c *config.Config) {
		// Optimized connection pool settings
		c.MaxConnectionLifetime = 30 * time.Minute
		c.MaxConnectionPoolSize = 20 // Reduced from 50 for better memory efficiency
		// Connection acquisition is gated by SocketConnectTimeout (TCP) plus,
		// for the routing scheme, the time to fetch the routing table from the
		// initial member. 10s is generous for a healthy cluster (sub-second
		// in practice) and tight enough that an unreachable cluster surfaces
		// quickly — important because the operator's reconciles otherwise
		// queue behind a stuck Bolt call.
		c.ConnectionAcquisitionTimeout = 10 * time.Second
		c.SocketConnectTimeout = 5 * time.Second
		c.SocketKeepalive = true
		c.ConnectionLivenessCheckTimeout = 10 * time.Second

		// Connection pool optimization
		c.MaxTransactionRetryTime = 15 * time.Second
		c.FetchSize = 1000 // Optimized fetch size for memory efficiency

		// Configure TLS if enabled
		if tlsCfg := buildTLSConfig(context.Background(), k8sClient, cluster.Namespace, cluster.Name, cluster.Spec.TLS); tlsCfg != nil {
			c.TlsConfig = tlsCfg
		}

		// Add custom resolver for better connection management
		c.AddressResolver = func(address config.ServerAddress) []config.ServerAddress {
			// Return the same address for now, can be extended for load balancing
			return []config.ServerAddress{address}
		}
	}

	driver, err := neo4j.NewDriverWithContext(uri, auth, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	// Initialize circuit breaker
	circuitBreaker := &CircuitBreaker{
		maxFailures:      5,
		resetTimeout:     30 * time.Second,
		halfOpenMaxCalls: 3,
		state:            CircuitClosed,
	}

	// Initialize connection pool metrics
	poolMetrics := &ConnectionPoolMetrics{
		LastHealthCheck: time.Now(),
	}

	client := &Client{
		driver:            driver,
		enterpriseCluster: cluster,
		credentials:       credentials,
		circuitBreaker:    circuitBreaker,
		poolMetrics:       poolMetrics,
	}

	return client, nil
}

// checkCircuitBreakerState evaluates and updates circuit breaker state.
// Called synchronously at the start of executeWithCircuitBreaker so an Open
// circuit can transition to HalfOpen once resetTimeout has elapsed — there is
// no longer a background ticker driving this (a previous implementation leaked
// a never-stopped goroutine + ticker per client, and every controller builds a
// client per reconcile).
func (c *Client) checkCircuitBreakerState() {
	c.circuitBreaker.mutex.Lock()
	defer c.circuitBreaker.mutex.Unlock()

	switch c.circuitBreaker.state {
	case CircuitOpen:
		if time.Since(c.circuitBreaker.lastFailureTime) > c.circuitBreaker.resetTimeout {
			c.circuitBreaker.state = CircuitHalfOpen
		}
	case CircuitHalfOpen:
		// Half-open state is handled in individual operations
	}
}

// executeWithCircuitBreaker wraps Neo4j operations with circuit breaker pattern
func (c *Client) executeWithCircuitBreaker(ctx context.Context, operation func(ctx context.Context) error) error {
	// Re-evaluate the breaker first so an Open circuit can move to HalfOpen
	// once resetTimeout has elapsed (previously driven by a background ticker).
	c.checkCircuitBreakerState()

	c.circuitBreaker.mutex.RLock()
	state := c.circuitBreaker.state
	c.circuitBreaker.mutex.RUnlock()

	if state == CircuitOpen {
		return fmt.Errorf("circuit breaker is open, operation rejected")
	}

	err := operation(ctx)

	c.circuitBreaker.mutex.Lock()
	defer c.circuitBreaker.mutex.Unlock()

	if err != nil {
		c.circuitBreaker.failureCount++
		c.circuitBreaker.lastFailureTime = time.Now()

		if c.circuitBreaker.failureCount >= c.circuitBreaker.maxFailures {
			c.circuitBreaker.state = CircuitOpen
		}
		return err
	}

	// Success - reset failure count and close circuit if half-open
	c.circuitBreaker.failureCount = 0
	if c.circuitBreaker.state == CircuitHalfOpen {
		c.circuitBreaker.state = CircuitClosed
	}

	return nil
}

// Close closes the Neo4j connection with proper cleanup
func (c *Client) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.driver != nil {
		err := c.driver.Close(context.Background())
		c.driver = nil // Set to nil after closing for proper cleanup
		return err
	}
	return nil
}

// formatOptionKey formats option keys for Neo4j CREATE DATABASE OPTIONS syntax
// Neo4j OPTIONS syntax doesn't support dotted keys - only simple identifiers
func (c *Client) formatOptionKey(key string) string {
	// Remove any existing quotes first
	cleanKey := strings.Trim(key, `"`)

	// Neo4j OPTIONS clause only supports simple identifiers (no dots)
	// If the key contains dots, replace dots with underscores to make it a valid identifier
	if strings.Contains(cleanKey, ".") {
		validKey := strings.ReplaceAll(cleanKey, ".", "_")
		return validKey
	}

	// For non-dotted keys, return as-is
	return cleanKey
}

// buildOptionsClause builds a `OPTIONS { … }` clause in which every value is a
// driver parameter, returning the clause text (with a leading space, empty if
// no options) and the params map to pass to session.Run. Keys are interpolated
// — they come from the operator/validator-controlled set (option keys are
// enum-validated; seedURI is a literal) — while values are ALWAYS parameters so
// user-controlled input (seedURI, option values) can never break out of the
// Cypher. seedURI, when non-empty, is added as the documented `seedURI` option
// key (replacing the non-grammar `FROM '<uri>'` clause; see issue #169).
func (c *Client) buildOptionsClause(options map[string]string, seedURI string, seedConfig *neo4jv1beta1.SeedConfiguration) (string, map[string]any) {
	merged := make(map[string]string, len(options)+2)
	for k, v := range options {
		merged[c.formatOptionKey(k)] = v
	}
	if seedURI != "" {
		merged["seedURI"] = seedURI
	}
	// seedConfig: the documented comma-separated provider-config string
	// (e.g. "region=eu-west-1"). Carried as a driver parameter.
	if seedConfig != nil {
		if cfg := serializeSeedConfig(seedConfig.Config); cfg != "" {
			merged["seedConfig"] = cfg
		}
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic clause order
	params := make(map[string]any, len(merged)+1)
	parts := make([]string, 0, len(merged)+1)
	for _, k := range keys {
		p := "opt_" + k
		parts = append(parts, fmt.Sprintf("%s: $%s", k, p))
		params[p] = merged[k]
	}

	// seedRestoreUntil (point-in-time) is expressed per the docs: an integer
	// transaction id as `$p`, an RFC3339 timestamp as `datetime($p)`. CalVer-only
	// — the validator rejects restoreUntil on 5.26. The value is a parameter
	// either way, so it can't inject. Appended after the sorted entries.
	if seedConfig != nil && seedConfig.RestoreUntil != "" {
		if txid, ok := strings.CutPrefix(seedConfig.RestoreUntil, "txId:"); ok {
			if n, err := strconv.ParseInt(txid, 10, 64); err == nil {
				parts = append(parts, "seedRestoreUntil: $opt_seedRestoreUntil")
				params["opt_seedRestoreUntil"] = n
			}
		} else {
			parts = append(parts, "seedRestoreUntil: datetime($opt_seedRestoreUntil)")
			params["opt_seedRestoreUntil"] = seedConfig.RestoreUntil
		}
	}

	if len(parts) == 0 {
		return "", nil
	}
	return " OPTIONS {" + strings.Join(parts, ", ") + "}", params
}

// serializeSeedConfig renders a seed-provider config map as the documented
// comma-separated `key=value` string (e.g. "region=eu-west-1") used by the
// seedConfig OPTIONS key. Keys are sorted for determinism. The result is passed
// to Cypher as a driver parameter; the validator constrains keys/values so the
// comma-separated form is unambiguous and injection-safe.
func serializeSeedConfig(cfg map[string]string) string {
	if len(cfg) == 0 {
		return ""
	}
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(cfg))
	for _, k := range keys {
		parts = append(parts, k+"="+cfg[k])
	}
	return strings.Join(parts, ",")
}

// cypherLanguageClause returns the ` DEFAULT LANGUAGE CYPHER <v>` clause only
// for the two valid language versions, so an unexpected value can never be
// interpolated into the statement (the validator already constrains it).
func cypherLanguageClause(cypherVersion string) string {
	switch cypherVersion {
	case "5", "25":
		return " DEFAULT LANGUAGE CYPHER " + cypherVersion
	default:
		return ""
	}
}

// closeSession safely closes a Neo4j session and logs any errors
func (c *Client) closeSession(ctx context.Context, session neo4j.SessionWithContext) {
	if err := session.Close(ctx); err != nil {
		// Increment error counter but don't fail the operation
		c.poolMetrics.ConnectionErrors++
		// Note: We don't log here to avoid spam, but the error is tracked
	}
}

// VerifyConnectivity verifies that the client can connect to the cluster with circuit breaker
func (c *Client) VerifyConnectivity(ctx context.Context) error {
	return c.executeWithCircuitBreaker(ctx, func(ctx context.Context) error {
		session := c.driver.NewSession(ctx, neo4j.SessionConfig{
			AccessMode: neo4j.AccessModeRead,
		})
		defer c.closeSession(ctx, session)

		_, err := session.Run(ctx, "CALL dbms.components() YIELD name, versions RETURN name, versions[0] as version LIMIT 1", nil)
		if err != nil {
			return fmt.Errorf("connectivity check failed: %w", err)
		}

		return nil
	})
}

// GetClusterOverview returns cluster topology information using SHOW SERVERS (5.26+ / CalVer).
// It does not use the deprecated dbms.cluster.overview() from 4.x.
// Each returned member has Role set to LEADER for the system database primary, FOLLOWER otherwise.
func (c *Client) GetClusterOverview(ctx context.Context) ([]ClusterMember, error) {
	var members []ClusterMember

	err := c.executeWithCircuitBreaker(ctx, func(ctx context.Context) error {
		servers, err := c.GetServerList(ctx)
		if err != nil {
			return fmt.Errorf("failed to get server list: %w", err)
		}

		// Determine system database primary address for Role (LEADER vs FOLLOWER)
		systemPrimaryAddr, _ := c.FindSystemDatabasePrimaryAddress(ctx)

		for _, s := range servers {
			role := "FOLLOWER"
			if systemPrimaryAddr != "" && (s.Address == systemPrimaryAddr || strings.HasPrefix(systemPrimaryAddr, s.Address) || strings.HasPrefix(s.Address, systemPrimaryAddr)) {
				role = "LEADER"
			}
			members = append(members, ClusterMember{
				ID:       s.Name,
				Address:  s.Address,
				Role:     role,
				Database: "system",
				Health:   s.Health,
			})
		}
		return nil
	})

	return members, err
}

// GetDatabases returns information about databases in the cluster with caching
func (c *Client) GetDatabases(ctx context.Context) ([]DatabaseInfo, error) {
	var databases []DatabaseInfo

	err := c.executeWithCircuitBreaker(ctx, func(ctx context.Context) error {
		session := c.driver.NewSession(ctx, neo4j.SessionConfig{
			AccessMode:   neo4j.AccessModeRead,
			DatabaseName: "system",
		})
		defer c.closeSession(ctx, session)

		// Optimized query with timeout
		timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		result, err := session.Run(timeoutCtx, `
			SHOW DATABASES
			YIELD name, currentStatus, default, home, role, requestedStatus
			RETURN name, currentStatus, default, home, role, requestedStatus
		`, nil)
		if err != nil {
			return fmt.Errorf("failed to get databases: %w", err)
		}

		for result.Next(timeoutCtx) {
			record := result.Record()

			name, _ := record.Get("name")
			status, _ := record.Get("currentStatus")
			isDefault, _ := record.Get("default")
			isHome, _ := record.Get("home")
			role, _ := record.Get("role")
			requestedStatus, _ := record.Get("requestedStatus")

			databases = append(databases, DatabaseInfo{
				Name:            fmt.Sprintf("%v", name),
				Status:          fmt.Sprintf("%v", status),
				Default:         fmt.Sprintf("%v", isDefault) == TrueString,
				Home:            fmt.Sprintf("%v", isHome) == TrueString,
				Role:            fmt.Sprintf("%v", role),
				RequestedStatus: fmt.Sprintf("%v", requestedStatus),
			})
		}

		if err = result.Err(); err != nil {
			return fmt.Errorf("error reading databases: %w", err)
		}

		return nil
	})

	return databases, err
}

// GetConnectionPoolMetrics returns current connection pool metrics
func (c *Client) GetConnectionPoolMetrics() *ConnectionPoolMetrics {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// Return a copy of the metrics
	metrics := *c.poolMetrics
	return &metrics
}

// CreateDatabase creates a new database with proper Neo4j 5.26+ syntax
func (c *Client) CreateDatabase(ctx context.Context, databaseName string, options map[string]string, wait bool, ifNotExists bool) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	// Build CREATE DATABASE query with IF NOT EXISTS
	query := fmt.Sprintf("CREATE DATABASE `%s`", databaseName)

	if ifNotExists {
		query = fmt.Sprintf("CREATE DATABASE `%s` IF NOT EXISTS", databaseName)
	}

	// Add options (values are driver parameters — never interpolated)
	optClause, params := c.buildOptionsClause(options, "", nil)
	query += optClause

	// Add WAIT or NOWAIT
	if wait {
		query += " WAIT"
	} else {
		query += " NOWAIT"
	}

	// Use timeout protection for WAIT operations
	err := c.executeWithWaitTimeout(ctx, session, query, params, wait, 300)
	if err != nil {
		// Check if database was created despite connection drop or timeout.
		// With WAIT, Neo4j holds the Bolt connection open until the database is fully online.
		// For multi-server clusters this can exceed TCP idle timeouts (~5 min), causing EOF.
		if wait && (errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "ConnectivityError")) {
			exists, checkErr := c.DatabaseExists(ctx, databaseName)
			if checkErr == nil && exists {
				return nil // Database created, connection dropped before WAIT completed
			}
		}
		return fmt.Errorf("failed to create database %s: %w", databaseName, err)
	}

	return nil
}

// CreateDatabaseWithTopology creates a database with specific topology constraints
func (c *Client) CreateDatabaseWithTopology(ctx context.Context, databaseName string, primaries, secondaries int32, options map[string]string, wait bool, ifNotExists bool, cypherVersion string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	// Build CREATE DATABASE query
	query := fmt.Sprintf("CREATE DATABASE `%s`", databaseName)

	if ifNotExists {
		query = fmt.Sprintf("CREATE DATABASE `%s` IF NOT EXISTS", databaseName)
	}

	// Add Cypher language version for Neo4j 2025.x (only "5"/"25" are emitted)
	query += cypherLanguageClause(cypherVersion)

	// Add topology if specified
	if primaries > 0 || secondaries > 0 {
		topologyParts := []string{}
		if primaries > 0 {
			if primaries == 1 {
				topologyParts = append(topologyParts, "1 PRIMARY")
			} else {
				topologyParts = append(topologyParts, fmt.Sprintf("%d PRIMARIES", primaries))
			}
		}
		if secondaries > 0 {
			if secondaries == 1 {
				topologyParts = append(topologyParts, "1 SECONDARY")
			} else {
				topologyParts = append(topologyParts, fmt.Sprintf("%d SECONDARIES", secondaries))
			}
		}
		if len(topologyParts) > 0 {
			query += " TOPOLOGY " + strings.Join(topologyParts, " ")
		}
	}

	// Add options (values are driver parameters — never interpolated)
	optClause, params := c.buildOptionsClause(options, "", nil)
	query += optClause

	// Add WAIT or NOWAIT
	if wait {
		query += " WAIT"
	} else {
		query += " NOWAIT"
	}

	// Use timeout protection for WAIT operations
	err := c.executeWithWaitTimeout(ctx, session, query, params, wait, 300)
	if err != nil {
		// Check if database was created despite connection drop or timeout.
		if wait && (errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "ConnectivityError")) {
			exists, checkErr := c.DatabaseExists(ctx, databaseName)
			if checkErr == nil && exists {
				return nil // Database created, connection dropped before WAIT completed
			}
		}
		return fmt.Errorf("failed to create database %s with topology: %w", databaseName, err)
	}

	return nil
}

// AlterDatabaseTopology sets database topology using ALTER DATABASE command for Neo4j 5.x
func (c *Client) AlterDatabaseTopology(ctx context.Context, databaseName string, primaries, secondaries int32) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	// Build ALTER DATABASE query with topology
	var topologyParts []string
	if primaries > 0 {
		if primaries == 1 {
			topologyParts = append(topologyParts, "1 PRIMARY")
		} else {
			topologyParts = append(topologyParts, fmt.Sprintf("%d PRIMARIES", primaries))
		}
	}
	if secondaries > 0 {
		if secondaries == 1 {
			topologyParts = append(topologyParts, "1 SECONDARY")
		} else {
			topologyParts = append(topologyParts, fmt.Sprintf("%d SECONDARIES", secondaries))
		}
	}

	if len(topologyParts) == 0 {
		return nil // No topology to set
	}

	query := fmt.Sprintf("ALTER DATABASE `%s` SET TOPOLOGY %s", databaseName, strings.Join(topologyParts, " "))

	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to alter database topology: %w", err)
	}

	return nil
}

// executeWithWaitTimeout executes a Neo4j query with timeout protection for WAIT operations
func (c *Client) executeWithWaitTimeout(ctx context.Context, session neo4j.SessionWithContext, query string, params map[string]any, wait bool, timeoutSeconds int) error {
	if wait && strings.Contains(query, " WAIT") {
		// Create a context with timeout for WAIT operations
		waitCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
		defer cancel()

		_, err := session.Run(waitCtx, query, params)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("database operation timed out after %ds while executing: %s: %w", timeoutSeconds, query, err)
			}
			return fmt.Errorf("database operation failed while executing: %s: %w", query, err)
		}
		return nil
	}

	// For non-WAIT operations, use the original context
	_, err := session.Run(ctx, query, params)
	if err != nil {
		return fmt.Errorf("database operation failed while executing: %s: %w", query, err)
	}
	return nil
}

// StartDatabase starts a stopped database
func (c *Client) StartDatabase(ctx context.Context, databaseName string, wait bool) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("START DATABASE `%s`", databaseName)
	if wait {
		query += " WAIT"
	} else {
		query += " NOWAIT"
	}

	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to start database %s: %w", databaseName, err)
	}

	return nil
}

// StopDatabase stops a running database
func (c *Client) StopDatabase(ctx context.Context, databaseName string, wait bool) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("STOP DATABASE `%s`", databaseName)
	if wait {
		query += " WAIT"
	} else {
		query += " NOWAIT"
	}

	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to stop database %s: %w", databaseName, err)
	}

	return nil
}

// AlterDatabase alters database properties
func (c *Client) AlterDatabase(ctx context.Context, databaseName string, options map[string]string, wait bool) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("ALTER DATABASE `%s`", databaseName)

	// Add options if provided
	if len(options) > 0 {
		var optionParts []string
		for key, value := range options {
			optionParts = append(optionParts, fmt.Sprintf("%s: '%s'", key, value))
		}
		query += " SET OPTIONS {" + strings.Join(optionParts, ", ") + "}"
	}

	// Add WAIT or NOWAIT
	if wait {
		query += " WAIT"
	} else {
		query += " NOWAIT"
	}

	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to alter database %s: %w", databaseName, err)
	}

	return nil
}

// GetDatabaseState returns the current state of a database
func (c *Client) GetDatabaseState(ctx context.Context, databaseName string) (string, error) {
	databases, err := c.GetDatabases(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get database state: %w", err)
	}

	for _, db := range databases {
		if db.Name == databaseName {
			return db.Status, nil
		}
	}

	return "", fmt.Errorf("database %s not found", databaseName)
}

// GetDatabaseServers returns the servers hosting a specific database
func (c *Client) GetDatabaseServers(ctx context.Context, databaseName string) ([]string, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := `
		SHOW DATABASES
		YIELD name, address
		WHERE name = $databaseName
		RETURN collect(address) as servers
	`

	// Add timeout protection for CI environment compatibility
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := session.Run(timeoutCtx, query, map[string]any{
		"databaseName": databaseName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get database servers: %w", err)
	}

	if result.Next(timeoutCtx) {
		record := result.Record()
		if servers, found := record.Get("servers"); found {
			if serverList, ok := servers.([]any); ok {
				var serverAddresses []string
				for _, addr := range serverList {
					if addrStr, ok := addr.(string); ok {
						serverAddresses = append(serverAddresses, addrStr)
					}
				}
				return serverAddresses, nil
			}
		}
	}

	return []string{}, nil
}

// DropDatabase drops a database
func (c *Client) DropDatabase(ctx context.Context, databaseName string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("DROP DATABASE `%s`", databaseName)
	_, err := session.Run(ctx, query, nil)
	if err != nil {
		// Check if the error is "database not found" - this is acceptable for deletion
		// as the desired end state is that the database doesn't exist
		if isDatabaseNotFoundError(err) {
			// Database already doesn't exist - this is success for deletion
			return nil
		}
		return fmt.Errorf("failed to drop database %s: %w", databaseName, err)
	}

	return nil
}

// isDatabaseNotFoundError checks if the error indicates the database doesn't exist
func isDatabaseNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())
	// Check for Neo4j "Database not found" errors in various formats
	return strings.Contains(errMsg, "database.databasenotfound") ||
		strings.Contains(errMsg, "database not found") ||
		strings.Contains(errMsg, "database does not exist") ||
		strings.Contains(errMsg, "databasenotfound")
}

// CreateUser creates a new user
func (c *Client) CreateUser(ctx context.Context, username, password string, mustChangePassword bool) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("CREATE USER `%s` SET PASSWORD $password", username)
	if mustChangePassword {
		query += " CHANGE REQUIRED"
	} else {
		query += " CHANGE NOT REQUIRED"
	}

	params := map[string]any{
		"password": password,
	}

	_, err := session.Run(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to create user %s: %w", username, err)
	}

	return nil
}

// DropUser drops a user
func (c *Client) DropUser(ctx context.Context, username string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("DROP USER `%s`", username)
	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to drop user %s: %w", username, err)
	}

	return nil
}

// CreateRole creates a new role
func (c *Client) CreateRole(ctx context.Context, roleName string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("CREATE ROLE `%s`", roleName)
	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to create role %s: %w", roleName, err)
	}

	return nil
}

// DropRole drops a role
func (c *Client) DropRole(ctx context.Context, roleName string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("DROP ROLE `%s`", roleName)
	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to drop role %s: %w", roleName, err)
	}

	return nil
}

// GrantRoleToUser grants a role to a user
func (c *Client) GrantRoleToUser(ctx context.Context, roleName, username string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("GRANT ROLE `%s` TO `%s`", escapeBackticks(roleName), escapeBackticks(username))
	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to grant role %s to user %s: %w", roleName, username, err)
	}

	return nil
}

// RevokeRoleFromUser revokes a role from a user
func (c *Client) RevokeRoleFromUser(ctx context.Context, roleName, username string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("REVOKE ROLE `%s` FROM `%s`", escapeBackticks(roleName), escapeBackticks(username))
	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to revoke role %s from user %s: %w", roleName, username, err)
	}

	return nil
}

// ExecutePrivilegeStatement executes a privilege statement
func (c *Client) ExecutePrivilegeStatement(ctx context.Context, statement string) error {
	// Privilege DDL runs as an auto-commit statement (session.Run), which —
	// unlike a managed transaction — gets no built-in retry. Concurrent admin
	// writes on the system database can collide with a transient
	// "conflicting transaction state" (GQLSTATUS 25N11) that Neo4j explicitly
	// asks the client to retry. Bounded retries keep the operator's own
	// privilege writes resilient without changing transaction semantics.
	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		session := c.driver.NewSession(ctx, neo4j.SessionConfig{
			AccessMode:   neo4j.AccessModeWrite,
			DatabaseName: "system",
		})
		_, err := session.Run(ctx, statement, nil)
		_ = session.Close(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isTransientNeo4jError(err) || attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
		}
	}
	return fmt.Errorf("failed to execute privilege statement: %w", lastErr)
}

// isTransientNeo4jError reports whether err is a transient Neo4j error worth
// retrying. It trusts the driver's own classification first, then falls back to
// matching the GQLSTATUS 25N11 "conflicting transaction state" markers, which
// 2025+/2026 servers surface and which may not be classified retryable by the
// driver across versions.
func isTransientNeo4jError(err error) bool {
	if err == nil {
		return false
	}
	if neo4j.IsRetryable(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "25n11") ||
		strings.Contains(msg, "conflicting transaction state") ||
		strings.Contains(msg, "please retry the transaction")
}

// CheckUpgradeCompatibility checks if an upgrade to the specified version is compatible
func (c *Client) CheckUpgradeCompatibility(ctx context.Context, targetVersion string) error {
	// Get current version
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, "CALL dbms.components() YIELD versions RETURN versions[0] as version", nil)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	if !result.Next(ctx) {
		return fmt.Errorf("no version information found")
	}

	record := result.Record()
	currentVersion, _ := record.Get("version")

	// Simplified version compatibility check
	// In a real implementation, this would check Neo4j upgrade matrix
	if fmt.Sprintf("%v", currentVersion) == targetVersion {
		return fmt.Errorf("target version %s is the same as current version", targetVersion)
	}

	return nil
}

// IsClusterHealthy checks if the cluster is in a healthy state
func (c *Client) IsClusterHealthy(ctx context.Context) (bool, error) {
	servers, err := c.GetServerList(ctx)
	if err != nil {
		return false, err
	}
	if len(servers) == 0 {
		return false, nil
	}
	healthyCount := 0
	for _, s := range servers {
		if s.Health == "Available" {
			healthyCount++
		}
	}
	return healthyCount > len(servers)/2, nil
}

// ReplicationStatusEntry holds one row returned by dbms.cluster.statusCheck().
type ReplicationStatusEntry struct {
	Database              string
	ServerID              string
	ServerName            string
	Address               string
	ReplicationSuccessful bool
	MemberStatus          string
	RecognisedLeader      string
	RecognisedLeaderTerm  int64
	Requester             bool
	Error                 string
}

// IsClusterReplicationHealthy calls dbms.cluster.statusCheck(["system"], 2000) and
// returns true only when the cluster can replicate to a majority and no member is
// UNAVAILABLE.  It is deliberately slower than IsClusterHealthy (which only uses
// SHOW SERVERS) and should only be used in latency-tolerant code paths such as the
// rolling upgrade post-validation or pre-pod-restart gate.
//
// Available in Neo4j 5.24+ and all CalVer (2025.x+) releases.
// Returns (false, nil) on transient replication failures so callers can retry.
// Returns (false, err) only on connection / query errors.
func (c *Client) IsClusterReplicationHealthy(ctx context.Context) (bool, error) {
	var entries []ReplicationStatusEntry

	err := c.executeWithCircuitBreaker(ctx, func(ctx context.Context) error {
		session := c.driver.NewSession(ctx, neo4j.SessionConfig{
			AccessMode:   neo4j.AccessModeRead,
			DatabaseName: "system",
		})
		defer c.closeSession(ctx, session)

		// 15-second outer timeout; the 2000 ms inner timeout is the replication
		// window granted to Neo4j itself for the dummy transaction.
		timeoutCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()

		result, err := session.Run(timeoutCtx,
			`CALL dbms.cluster.statusCheck($databases, $timeoutMs)
			 YIELD database, serverId, serverName, address, replicationSuccessful,
			       memberStatus, recognisedLeader, recognisedLeaderTerm, requester, error
			 RETURN database, serverId, serverName, address, replicationSuccessful,
			        memberStatus, recognisedLeader, recognisedLeaderTerm, requester, error`,
			map[string]any{
				"databases": []any{"system"},
				"timeoutMs": int64(2000),
			})
		if err != nil {
			return fmt.Errorf("failed to call dbms.cluster.statusCheck: %w", err)
		}

		for result.Next(timeoutCtx) {
			r := result.Record()
			e := ReplicationStatusEntry{}
			if v, ok := r.Get("database"); ok && v != nil {
				e.Database = fmt.Sprintf("%v", v)
			}
			if v, ok := r.Get("serverId"); ok && v != nil {
				e.ServerID = fmt.Sprintf("%v", v)
			}
			if v, ok := r.Get("serverName"); ok && v != nil {
				e.ServerName = fmt.Sprintf("%v", v)
			}
			if v, ok := r.Get("address"); ok && v != nil {
				e.Address = fmt.Sprintf("%v", v)
			}
			if v, ok := r.Get("replicationSuccessful"); ok && v != nil {
				if b, ok := v.(bool); ok {
					e.ReplicationSuccessful = b
				}
			}
			if v, ok := r.Get("memberStatus"); ok && v != nil {
				e.MemberStatus = fmt.Sprintf("%v", v)
			}
			if v, ok := r.Get("recognisedLeader"); ok && v != nil {
				e.RecognisedLeader = fmt.Sprintf("%v", v)
			}
			if v, ok := r.Get("recognisedLeaderTerm"); ok && v != nil {
				if n, ok := v.(int64); ok {
					e.RecognisedLeaderTerm = n
				}
			}
			if v, ok := r.Get("requester"); ok && v != nil {
				if b, ok := v.(bool); ok {
					e.Requester = b
				}
			}
			if v, ok := r.Get("error"); ok && v != nil {
				e.Error = fmt.Sprintf("%v", v)
			}
			entries = append(entries, e)
		}
		return result.Err()
	})
	if err != nil {
		return false, err
	}

	if len(entries) == 0 {
		return false, nil
	}

	for _, e := range entries {
		if !e.ReplicationSuccessful || e.MemberStatus == "UNAVAILABLE" {
			return false, nil
		}
	}
	return true, nil
}

// WaitForReplicationHealthy retries IsClusterReplicationHealthy until it returns
// true or the context deadline is exceeded.  It is designed for upgrade gate-keeping:
// call it after WaitForClusterStabilization or inside post-upgrade validations.
func (c *Client) WaitForReplicationHealthy(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for cluster replication health")
		case <-ticker.C:
			ok, err := c.IsClusterReplicationHealthy(ctx)
			if err != nil {
				continue
			}
			if ok {
				return nil
			}
		}
	}
}

// FindSystemDatabasePrimaryAddress returns the bolt address of the server currently
// holding the primary role for the system database. This is the most important server
// to roll last during a rolling upgrade. Returns ("", nil) if the primary cannot be
// determined (caller should fall back to a safe default).
func (c *Client) FindSystemDatabasePrimaryAddress(ctx context.Context) (string, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// SHOW DATABASES with address and role is available in Neo4j 5.x and 2025.x.
	// It returns one row per (database, member) pair; we filter for the system database primary.
	result, err := session.Run(timeoutCtx,
		"SHOW DATABASES YIELD name, role, address WHERE name = 'system' AND role = 'primary' RETURN address",
		nil)
	if err != nil {
		return "", fmt.Errorf("failed to query system database primary: %w", err)
	}

	if result.Next(timeoutCtx) {
		record := result.Record()
		if addr, ok := record.Get("address"); ok {
			return fmt.Sprintf("%v", addr), nil
		}
	}

	if err := result.Err(); err != nil {
		return "", err
	}

	return "", nil
}

// WaitForServerAvailable waits until the Neo4j server whose bolt address contains
// podName (e.g. "mycluster-server-2") appears in SHOW SERVERS as Enabled+Available.
// This should be called after each pod rolls during a rolling upgrade to ensure the
// server has re-joined the cluster before the next pod is restarted.
func (c *Client) WaitForServerAvailable(ctx context.Context, podName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for server %q to become Enabled/Available in cluster", podName)
		case <-ticker.C:
			servers, err := c.GetServerList(ctx)
			if err != nil {
				continue // Neo4j not yet accepting queries; keep waiting
			}
			for _, s := range servers {
				if strings.Contains(s.Address, podName) &&
					s.State == "Enabled" && s.Health == "Available" {
					return nil
				}
			}
		}
	}
}

// GetLeader returns the cluster member that holds the primary role for the system
// database. Uses SHOW DATABASES and SHOW SERVERS (5.26+ / CalVer).
func (c *Client) GetLeader(ctx context.Context) (*ClusterMember, error) {
	primaryAddr, err := c.FindSystemDatabasePrimaryAddress(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find system database primary: %w", err)
	}
	if primaryAddr == "" {
		return nil, fmt.Errorf("no primary found for system database")
	}

	// Match the address to a known server to populate the ClusterMember fields.
	servers, err := c.GetServerList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list servers: %w", err)
	}
	for _, s := range servers {
		if s.Address == primaryAddr || strings.HasPrefix(primaryAddr, s.Address) {
			return &ClusterMember{
				ID:      s.Name,
				Address: s.Address,
				Role:    "LEADER",
				Health:  s.Health,
			}, nil
		}
	}

	// Address found but no matching server in SHOW SERVERS — return with just the address.
	return &ClusterMember{
		ID:      primaryAddr,
		Address: primaryAddr,
		Role:    "LEADER",
	}, nil
}

// WaitForClusterReady waits for the cluster to be ready
func (c *Client) WaitForClusterReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for cluster to be ready")
		case <-ticker.C:
			healthy, err := c.IsClusterHealthy(ctx)
			if err != nil {
				continue // Keep trying
			}
			if healthy {
				return nil
			}
		}
	}
}

// GetMemberRole returns whether the system database has a primary member in the cluster.
// Uses SHOW DATABASES (5.26+ / CalVer replacement for the removed dbms.cluster.role()).
// Returns "primary" if the system database has at least one primary, "secondary" otherwise.
// Note: dbms.cluster.role() was removed in Neo4j 5.0; SHOW DATABASES is the replacement.
func (c *Client) GetMemberRole(ctx context.Context) (string, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := session.Run(timeoutCtx,
		"SHOW DATABASES YIELD name, role WHERE name = 'system' AND role = 'primary' RETURN count(*) AS primaryCount",
		nil)
	if err != nil {
		return "", fmt.Errorf("failed to get member role: %w", err)
	}

	if result.Next(timeoutCtx) {
		record := result.Record()
		if count, ok := record.Get("primaryCount"); ok {
			if n, ok := count.(int64); ok && n > 0 {
				return "primary", nil
			}
		}
	}

	if err = result.Err(); err != nil {
		return "", fmt.Errorf("error reading member role: %w", err)
	}

	return "secondary", nil
}

// WaitForRoleTransition waits for a member to assume a specific role
func (c *Client) WaitForRoleTransition(ctx context.Context, expectedRole string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			currentRole, err := c.GetMemberRole(ctx)
			if err != nil {
				return fmt.Errorf("failed to get member role: %w", err)
			}
			return fmt.Errorf("timeout waiting for role transition to %s (current: %s)", expectedRole, currentRole)
		case <-ticker.C:
			currentRole, err := c.GetMemberRole(ctx)
			if err != nil {
				continue // Keep trying
			}
			if currentRole == expectedRole {
				return nil
			}
		}
	}
}

// GetClusterConsensusState checks if cluster has achieved consensus
func (c *Client) GetClusterConsensusState(ctx context.Context) (bool, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	// Check cluster topology
	result, err := session.Run(ctx, "SHOW SERVERS YIELD name, address, state, health, hosting", nil)
	if err != nil {
		return false, fmt.Errorf("failed to get server information: %w", err)
	}

	var totalServers, enabledServers int
	for result.Next(ctx) {
		record := result.Record()
		totalServers++
		if state, found := record.Get("state"); found {
			if stateStr, ok := state.(string); ok && stateStr == "Enabled" {
				enabledServers++
			}
		}
	}

	// Require majority to be enabled for consensus
	return enabledServers > totalServers/2, nil
}

// ValidateUpgradeSafety performs comprehensive upgrade safety checks
func (c *Client) ValidateUpgradeSafety(ctx context.Context, targetVersion string) error {
	// 1. Check cluster health
	healthy, err := c.IsClusterHealthy(ctx)
	if err != nil {
		return fmt.Errorf("failed to check cluster health: %w", err)
	}
	if !healthy {
		return fmt.Errorf("cluster is not healthy, upgrade not safe")
	}

	// 2. Check consensus state
	consensus, err := c.GetClusterConsensusState(ctx)
	if err != nil {
		return fmt.Errorf("failed to check consensus state: %w", err)
	}
	if !consensus {
		return fmt.Errorf("cluster does not have consensus, upgrade not safe")
	}

	// 3. Check version compatibility
	if err := c.CheckUpgradeCompatibility(ctx, targetVersion); err != nil {
		return fmt.Errorf("version compatibility check failed: %w", err)
	}

	// 4. Verify we have a leader
	leader, err := c.GetLeader(ctx)
	if err != nil {
		return fmt.Errorf("no cluster leader found: %w", err)
	}
	if leader == nil {
		return fmt.Errorf("cluster leader is nil")
	}

	return nil
}

// WaitForClusterStabilization waits for cluster to stabilize after changes
func (c *Client) WaitForClusterStabilization(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	stableCount := 0
	requiredStableChecks := 3

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for cluster stabilization")
		case <-ticker.C:
			// Check if cluster is both healthy and has consensus
			healthy, err := c.IsClusterHealthy(ctx)
			if err != nil {
				stableCount = 0
				continue
			}

			consensus, err := c.GetClusterConsensusState(ctx)
			if err != nil {
				stableCount = 0
				continue
			}

			if healthy && consensus {
				stableCount++
				if stableCount >= requiredStableChecks {
					return nil
				}
			} else {
				stableCount = 0
			}
		}
	}
}

// GetNonLeaderPrimaries returns primary members that are not the current leader
func (c *Client) GetNonLeaderPrimaries(ctx context.Context) ([]ClusterMember, error) {
	members, err := c.GetClusterOverview(ctx)
	if err != nil {
		return nil, err
	}

	leader, err := c.GetLeader(ctx)
	if err != nil {
		return nil, err
	}

	var nonLeaderPrimaries []ClusterMember
	for _, member := range members {
		if member.Role == "FOLLOWER" && member.ID != leader.ID {
			// Check if this is a primary (not secondary) by checking if it hosts system database
			if member.Database == "system" || strings.Contains(member.Database, "system") {
				nonLeaderPrimaries = append(nonLeaderPrimaries, member)
			}
		}
	}

	return nonLeaderPrimaries, nil
}

// GetSecondaryMembers returns all secondary/read replica members. In 5.26+ server-based
// topology, GetClusterOverview returns one row per server with Database="system", so
// this may return an empty list; role is per-database via SHOW DATABASES if needed.
func (c *Client) GetSecondaryMembers(ctx context.Context) ([]ClusterMember, error) {
	members, err := c.GetClusterOverview(ctx)
	if err != nil {
		return nil, err
	}

	var secondaries []ClusterMember
	for _, member := range members {
		// Secondaries typically don't host the system database
		if !strings.Contains(member.Database, "system") && member.Role != "LEADER" {
			secondaries = append(secondaries, member)
		}
	}

	return secondaries, nil
}

// Helper functions

func getCredentials(ctx context.Context, k8sClient client.Client, namespace, secretName string) (*Credentials, error) {
	secret := &corev1.Secret{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}, secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get admin secret: %w", err)
	}

	// Try to get NEO4J_AUTH format first (neo4j/password)
	if authData, exists := secret.Data["NEO4J_AUTH"]; exists {
		authString := string(authData)
		parts := strings.Split(authString, "/")
		if len(parts) == 2 {
			return &Credentials{
				Username: parts[0],
				Password: parts[1],
			}, nil
		}
	}

	// Fallback to separate username/password keys
	username := string(secret.Data["username"])
	password := string(secret.Data["password"])

	if username == "" {
		username = "neo4j" // Default username
	}

	if password == "" {
		return nil, fmt.Errorf("no password found in secret")
	}

	return &Credentials{
		Username: username,
		Password: password,
	}, nil
}

// ListServers runs `SHOW SERVERS` against the system DB and returns one entry
// per server in the cluster. Used to resolve a Pod-ordinal-relative seed to
// the Neo4j-internal server ID required by dbms.cluster.recreateDatabase.
//
// Returns an empty slice on a standalone (single-node) deployment — the
// procedure still works but produces a list of 1.
func (c *Client) ListServers(ctx context.Context) ([]ServerInfo, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	// YIELD an explicit column set so we don't depend on Neo4j's default
	// projection (which has shifted across 5.x patch releases). Address
	// includes the bolt port; we don't strip it because the column is
	// purely diagnostic — only `id` is consumed by recreate.
	result, err := session.Run(ctx,
		"SHOW SERVERS YIELD serverId, name, address, state, health", nil)
	if err != nil {
		return nil, fmt.Errorf("SHOW SERVERS failed: %w", err)
	}

	var servers []ServerInfo
	for result.Next(ctx) {
		rec := result.Record()
		s := ServerInfo{}
		if v, ok := rec.Get("serverId"); ok {
			if str, ok := v.(string); ok {
				s.ID = str
			}
		}
		if v, ok := rec.Get("name"); ok {
			if str, ok := v.(string); ok {
				s.Name = str
			}
		}
		if v, ok := rec.Get("address"); ok {
			if str, ok := v.(string); ok {
				s.Address = str
			}
		}
		if v, ok := rec.Get("state"); ok {
			if str, ok := v.(string); ok {
				s.State = str
			}
		}
		if v, ok := rec.Get("health"); ok {
			if str, ok := v.(string); ok {
				s.Health = str
			}
		}
		servers = append(servers, s)
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("iterating SHOW SERVERS rows: %w", err)
	}
	return servers, nil
}

// RecreateDatabase invokes `dbms.[cluster.]recreateDatabase(...)` with the
// given seeding server IDs. Picks the correct procedure name based on the
// Neo4j version (`dbms.cluster.recreateDatabase` for 5.24–2025.03,
// `dbms.recreateDatabase` for 2025.04+).
//
// Why this exists: after restoring a backup to a single server's PVC in a
// multi-server cluster, the operator needs to force Neo4j to re-seed every
// other server from that one's restored data. Without this, post-restart
// cluster bootstrap picks the primary non-deterministically — sometimes the
// stale-data server wins consensus and the restored data is overwritten on
// re-sync. See `internal/controller/neo4jrestore_controller.go`'s
// recreateRestoredDatabase phase for the full flow.
//
// An empty seedingServerIDs slice asks Neo4j to auto-select the most
// up-to-date allocation; pass an explicit list to pin the seed (which is
// what the restore flow does — server-0 always holds the restored data).
//
// Returns nil-or-skip-marker (no error, no action) when the procedure isn't
// supported on this version. Callers detect by inspecting the returned
// `applied` bool.
func (c *Client) RecreateDatabase(
	ctx context.Context,
	version *Version,
	databaseName string,
	seedingServerIDs []string,
) (applied bool, err error) {
	procedure := version.RecreateDatabaseProcedure()
	if procedure == "" {
		// Pre-5.24 / pre-2025.02 — recreate isn't available. Caller
		// proceeds without the deterministic-seed guarantee.
		return false, nil
	}

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	// Empty list maps to {seedingServers: []} which is Neo4j's
	// "auto-select" mode. Explicit list pins the seeders.
	seeders := seedingServerIDs
	if seeders == nil {
		seeders = []string{}
	}

	query := fmt.Sprintf("CALL %s($db, {seedingServers: $seeders})", procedure)
	if _, err := session.Run(ctx, query, map[string]any{
		"db":      databaseName,
		"seeders": seeders,
	}); err != nil {
		return false, fmt.Errorf("CALL %s for %q with %d seeders: %w",
			procedure, databaseName, len(seeders), err)
	}
	return true, nil
}

// RecreateDatabaseWithSeedURI invokes `dbms.[cluster.]recreateDatabase` with
// a `seedURI` parameter — the cluster-native restore path documented at
// https://neo4j.com/docs/operations-manual/current/clustering/databases/#restore-database-using-recreate-procedure.
//
// Use this for cluster restores when the target database EXISTS: every server
// pulls the backup chain directly from the URI, preserving previously granted
// user/role privileges, with no need to DROP first. The URI must point at a
// DIRECTORY containing the backup chain (full + diffs); CloudSeedProvider
// scans it and applies the chain.
//
// For NEW databases (database doesn't exist), use CreateDatabaseFromSeedURI
// with the modern `OPTIONS { seedURI }` syntax instead.
//
// The procedure name is version-gated:
//   - 5.24 / 2025.02–2025.03 → `dbms.cluster.recreateDatabase`
//   - 2025.04+               → `dbms.recreateDatabase`
//
// Returns (false, nil) on versions that don't support recreate so the caller
// can route to a different path (e.g. DROP + CREATE).
func (c *Client) RecreateDatabaseWithSeedURI(
	ctx context.Context,
	version *Version,
	databaseName string,
	seedURI string,
) (applied bool, err error) {
	procedure := version.RecreateDatabaseProcedure()
	if procedure == "" {
		return false, nil
	}
	if seedURI == "" {
		return false, fmt.Errorf("seedURI is required for seedURI-based recreate of database %q", databaseName)
	}

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("CALL %s($db, {seedURI: $uri})", procedure)
	if _, err := session.Run(ctx, query, map[string]any{
		"db":  databaseName,
		"uri": seedURI,
	}); err != nil {
		return false, fmt.Errorf("CALL %s for %q with seedURI: %w", procedure, databaseName, err)
	}
	return true, nil
}

// DatabaseOnlineState returns how many allocations of databaseName are online,
// the total allocation count, and a human-readable diagnostic string
// (currentStatus + statusMessage per row) for logging on timeout. It runs a
// single bounded SHOW DATABASE, so callers can poll it across reconciles
// without holding a worker (see pollClusterRestoreOnline).
func (c *Client) DatabaseOnlineState(ctx context.Context, databaseName string) (online, total int, diag string, err error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := session.Run(timeoutCtx,
		"SHOW DATABASE $name YIELD currentStatus, statusMessage",
		map[string]any{"name": databaseName})
	if err != nil {
		return 0, 0, "", fmt.Errorf("SHOW DATABASE %q: %w", databaseName, err)
	}
	var diags []string
	for result.Next(timeoutCtx) {
		rec := result.Record()
		cur, _ := rec.Get("currentStatus")
		msg, _ := rec.Get("statusMessage")
		total++
		if fmt.Sprintf("%v", cur) == "online" {
			online++
		}
		d := fmt.Sprintf("%v", cur)
		if m := fmt.Sprintf("%v", msg); m != "" && m != "<nil>" {
			d += " (" + m + ")"
		}
		diags = append(diags, d)
	}
	if rerr := result.Err(); rerr != nil {
		return 0, 0, "", fmt.Errorf("reading SHOW DATABASE %q: %w", databaseName, rerr)
	}
	return online, total, strings.Join(diags, "; "), nil
}

// CreateDatabaseWithSeedURIOptions creates a new database from a backup chain
// using the modern `CREATE DATABASE … OPTIONS { seedURI }` Cypher syntax —
// the cluster-native "new database from backup" path documented at
// https://neo4j.com/docs/operations-manual/current/clustering/databases/#restore-database-using-uri-approach.
//
// For EXISTING databases use RecreateDatabaseWithSeedURI instead.
//
// The URI must point at a DIRECTORY (with trailing slash) containing the
// backup chain. CloudSeedProvider scans for the chain; URLConnectionSeedProvider
// expects a single artifact path.
func (c *Client) CreateDatabaseWithSeedURIOptions(
	ctx context.Context,
	databaseName string,
	seedURI string,
	ifNotExists bool,
) error {
	if seedURI == "" {
		return fmt.Errorf("seedURI is required for seedURI-based CREATE DATABASE of %q", databaseName)
	}

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	ine := ""
	if ifNotExists {
		ine = " IF NOT EXISTS"
	}
	query := fmt.Sprintf("CREATE DATABASE `%s`%s OPTIONS { seedURI: $uri } WAIT", databaseName, ine)
	if _, err := session.Run(ctx, query, map[string]any{"uri": seedURI}); err != nil {
		return fmt.Errorf("CREATE DATABASE %q OPTIONS{seedURI}: %w", databaseName, err)
	}
	return nil
}

// DatabaseExists checks if a database exists
func (c *Client) DatabaseExists(ctx context.Context, databaseName string) (bool, error) {
	databases, err := c.GetDatabases(ctx)
	if err != nil {
		return false, err
	}

	for _, db := range databases {
		if db.Name == databaseName {
			return true, nil
		}
	}
	return false, nil
}

// ExecuteCypher executes a cypher statement on a specific database
func (c *Client) ExecuteCypher(ctx context.Context, databaseName, statement string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: databaseName,
	})
	defer session.Close(ctx)

	_, err := session.Run(ctx, statement, nil)
	if err != nil {
		return fmt.Errorf("failed to execute cypher: %w", err)
	}

	return nil
}

// GetUserRoles returns roles assigned to a user
func (c *Client) GetUserRoles(ctx context.Context, username string) ([]string, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := "SHOW USER PRIVILEGES WHERE user = $username YIELD role"
	result, err := session.Run(ctx, query, map[string]any{
		"username": username,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get user roles: %w", err)
	}

	var roles []string
	for result.Next(ctx) {
		record := result.Record()
		if role, found := record.Get("role"); found {
			if roleStr, ok := role.(string); ok {
				roles = append(roles, roleStr)
			}
		}
	}

	return roles, nil
}

// SetUserProperty sets a property for a user
func (c *Client) SetUserProperty(ctx context.Context, username, key, value string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("ALTER USER `%s` SET %s = $value", username, key)
	_, err := session.Run(ctx, query, map[string]any{
		"value": value,
	})
	if err != nil {
		return fmt.Errorf("failed to set user property: %w", err)
	}

	return nil
}

// ExecuteQuery executes a query and returns the first result as a string
func (c *Client) ExecuteQuery(ctx context.Context, query string) (string, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, query, nil)
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %w", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		if len(record.Values) > 0 {
			return fmt.Sprintf("%v", record.Values[0]), nil
		}
	}

	return "", nil
}

// GetServerList retrieves the list of servers in the Neo4j cluster
func (c *Client) GetServerList(ctx context.Context) ([]ServerInfo, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, "SHOW SERVERS", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SHOW SERVERS: %w", err)
	}

	var servers []ServerInfo
	for result.Next(ctx) {
		record := result.Record()

		// Extract server information from record
		// SHOW SERVERS returns: name, address, state, health, hosting
		if len(record.Values) >= 5 {
			server := ServerInfo{
				Name:    fmt.Sprintf("%v", record.Values[0]),
				Address: fmt.Sprintf("%v", record.Values[1]),
				State:   fmt.Sprintf("%v", record.Values[2]),
				Health:  fmt.Sprintf("%v", record.Values[3]),
			}

			// Parse hosting databases (array of strings)
			if hostingValue := record.Values[4]; hostingValue != nil {
				if hostingList, ok := hostingValue.([]any); ok {
					for _, db := range hostingList {
						server.Hosting = append(server.Hosting, fmt.Sprintf("%v", db))
					}
				}
			}

			servers = append(servers, server)
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error reading SHOW SERVERS results: %w", err)
	}

	return servers, nil
}

// GetLoadedComponents returns a list of loaded Neo4j components/plugins
func (c *Client) GetLoadedComponents(ctx context.Context) ([]ComponentInfo, error) {
	var components []ComponentInfo

	err := c.executeWithCircuitBreaker(ctx, func(ctx context.Context) error {
		session := c.driver.NewSession(ctx, neo4j.SessionConfig{
			AccessMode: neo4j.AccessModeRead,
		})
		defer session.Close(ctx)

		result, err := session.Run(ctx, "CALL dbms.components() YIELD name, versions, edition RETURN name, versions[0] as version, edition", nil)
		if err != nil {
			return fmt.Errorf("failed to get loaded components: %w", err)
		}

		for result.Next(ctx) {
			record := result.Record()
			name, _ := record.Get("name")
			version, _ := record.Get("version")
			edition, _ := record.Get("edition")

			components = append(components, ComponentInfo{
				Name:    fmt.Sprintf("%v", name),
				Version: fmt.Sprintf("%v", version),
				Edition: fmt.Sprintf("%v", edition),
			})
		}

		return result.Err()
	})

	return components, err
}

// SetConfiguration sets a Neo4j configuration parameter
func (c *Client) SetConfiguration(ctx context.Context, key, value string) error {
	return c.executeWithCircuitBreaker(ctx, func(ctx context.Context) error {
		session := c.driver.NewSession(ctx, neo4j.SessionConfig{
			AccessMode:   neo4j.AccessModeWrite,
			DatabaseName: "system",
		})
		defer session.Close(ctx)

		// Note: Dynamic configuration changes may require restart
		query := fmt.Sprintf("CALL dbms.setConfigValue('%s', '%s')", key, value)
		_, err := session.Run(ctx, query, nil)
		if err != nil {
			return fmt.Errorf("failed to set configuration %s=%s: %w", key, value, err)
		}

		return nil
	})
}

// SetAllowedProcedures sets the allowed procedures for a plugin
func (c *Client) SetAllowedProcedures(ctx context.Context, procedures []string) error {
	return c.executeWithCircuitBreaker(ctx, func(ctx context.Context) error {
		session := c.driver.NewSession(ctx, neo4j.SessionConfig{
			AccessMode:   neo4j.AccessModeWrite,
			DatabaseName: "system",
		})
		defer session.Close(ctx)

		// Set dbms.security.procedures.allowlist
		procedureList := strings.Join(procedures, ",")
		query := fmt.Sprintf("CALL dbms.setConfigValue('dbms.security.procedures.allowlist', '%s')", procedureList)
		_, err := session.Run(ctx, query, nil)
		if err != nil {
			return fmt.Errorf("failed to set allowed procedures: %w", err)
		}

		return nil
	})
}

// SetDeniedProcedures sets the denied procedures for a plugin
func (c *Client) SetDeniedProcedures(ctx context.Context, procedures []string) error {
	return c.executeWithCircuitBreaker(ctx, func(ctx context.Context) error {
		session := c.driver.NewSession(ctx, neo4j.SessionConfig{
			AccessMode:   neo4j.AccessModeWrite,
			DatabaseName: "system",
		})
		defer session.Close(ctx)

		// Set dbms.security.procedures.denylist
		procedureList := strings.Join(procedures, ",")
		query := fmt.Sprintf("CALL dbms.setConfigValue('dbms.security.procedures.denylist', '%s')", procedureList)
		_, err := session.Run(ctx, query, nil)
		if err != nil {
			return fmt.Errorf("failed to set denied procedures: %w", err)
		}

		return nil
	})
}

// EnableSandboxMode enables sandbox mode for plugins
func (c *Client) EnableSandboxMode(ctx context.Context, enabled bool) error {
	return c.executeWithCircuitBreaker(ctx, func(ctx context.Context) error {
		session := c.driver.NewSession(ctx, neo4j.SessionConfig{
			AccessMode:   neo4j.AccessModeWrite,
			DatabaseName: "system",
		})
		defer session.Close(ctx)

		value := "false"
		if enabled {
			value = "true"
		}

		query := fmt.Sprintf("CALL dbms.setConfigValue('dbms.security.procedures.unrestricted', '%s')", value)
		_, err := session.Run(ctx, query, nil)
		if err != nil {
			return fmt.Errorf("failed to set sandbox mode: %w", err)
		}

		return nil
	})
}

// ComponentInfo represents information about a Neo4j component
type ComponentInfo struct {
	Name    string
	Version string
	Edition string
}

// SuspendUser suspends a user account
func (c *Client) SuspendUser(ctx context.Context, username string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("ALTER USER `%s` SET STATUS SUSPENDED", username)
	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to suspend user: %w", err)
	}

	return nil
}

// ActivateUser activates a user account
func (c *Client) ActivateUser(ctx context.Context, username string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("ALTER USER `%s` SET STATUS ACTIVE", username)
	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to activate user: %w", err)
	}

	return nil
}

// ValidateEnterpriseVersion checks if the Neo4j version is Enterprise 5.26 or higher
func (c *Client) ValidateEnterpriseVersion(ctx context.Context) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, "CALL dbms.components() YIELD name, versions, edition RETURN name, versions[0] as version, edition", nil)
	if err != nil {
		return fmt.Errorf("failed to check Neo4j version: %w", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		name, _ := record.Get("name")
		version, _ := record.Get("version")
		edition, _ := record.Get("edition")

		nameStr := fmt.Sprintf("%v", name)
		versionStr := fmt.Sprintf("%v", version)
		editionStr := fmt.Sprintf("%v", edition)

		// Check if it's Neo4j
		if nameStr != "Neo4j Kernel" {
			return fmt.Errorf("expected Neo4j Kernel, got %s", nameStr)
		}

		// Check if it's Enterprise Edition
		if editionStr != "enterprise" {
			return fmt.Errorf("Neo4j Community Edition is not supported. Only Neo4j Enterprise 5.26+ or 2025.1+ is supported. Found edition: %s", editionStr)
		}

		// Check version requirement (supports both SemVer and CalVer)
		if !isVersionSupported(versionStr) {
			return fmt.Errorf("Neo4j version %s is not supported. Minimum required version is 5.26 or 2025.1", versionStr)
		}

		return nil
	}

	return fmt.Errorf("no version information found")
}

// isVersionSupported checks if the version is supported (5.26+ or 2025.1+)
func isVersionSupported(version string) bool {
	// Remove any prefix like "v" and suffixes like "-enterprise"
	version = strings.TrimPrefix(version, "v")
	if idx := strings.Index(version, "-"); idx != -1 {
		version = version[:idx]
	}

	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}

	// CalVer support (2025.x.x)
	if major >= 2025 {
		return true // All CalVer versions are supported
	}

	// SemVer support: only 5.26.x — the last semver LTS release (no 5.27+ exists)
	if major == 5 {
		return minor == 26
	}

	return false
}

// buildConnectionURIForStandalone returns the URI the operator's Bolt client
// uses for a Neo4jEnterpriseStandalone deployment.
//
// We use the routing scheme (`neo4j://` / `neo4j+s://`) for parity with the
// cluster builder. On a single-member topology the routing table contains
// the lone server as both reader and writer, so behavior is equivalent to
// the direct `bolt://` scheme — but staying on `neo4j://` keeps both code
// paths uniform and is forward-compatible if a standalone is ever upgraded
// to a multi-server topology.
func buildConnectionURIForStandalone(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) string {
	scheme := "neo4j"
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		scheme = "neo4j+s"
	}

	// Use service for connection (standalone service naming pattern)
	host := fmt.Sprintf("%s-service.%s.svc.cluster.local", standalone.Name, standalone.Namespace)
	port := 7687

	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

// buildConnectionURIForEnterprise returns the URI the operator's Bolt client
// uses for a Neo4jEnterpriseCluster.
//
// CRITICAL: this MUST use the routing scheme (`neo4j://` / `neo4j+s://`),
// not the direct scheme (`bolt://` / `bolt+s://`). Cluster admin commands
// — `CREATE/DROP/ALTER USER`, `CREATE/DROP ROLE`, `GRANT/DENY/REVOKE`,
// `CREATE/ALTER/DROP DATABASE`, `CREATE OR REPLACE/DROP/GRANT/REVOKE AUTH RULE`
// — must execute on the cluster leader and return
// `Neo.ClientError.Cluster.NotALeader` from any other member. The Go driver
// honors `neo4j.SessionConfig{AccessMode: AccessModeWrite}` only under the
// routing scheme; under the direct scheme it is silently ignored and queries
// land on whatever pod K8s steered the underlying TCP connection to. With
// the operator's `{cluster}-client` ClusterIP load-balancing across all
// members, that yields a 1-in-N chance of hitting the leader on every
// reconcile and produces visible Ready ↔ Failed status flicker on the
// `Neo4jRole` / `Neo4jUser` / `Neo4jAuthRule` controllers.
//
// See CLAUDE.md regression checklist item on routing scheme; do not revert
// to `bolt://` without an explicit replacement for leader routing.
func buildConnectionURIForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	scheme := "neo4j"
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == "cert-manager" {
		scheme = "neo4j+s"
	}

	// Client service (ClusterIP) — selects across all server pods. The
	// driver opens an initial routing connection to any member, calls
	// `dbms.routing.getRoutingTable`, and routes write transactions to the
	// leader.
	host := fmt.Sprintf("%s-client.%s.svc.cluster.local", cluster.Name, cluster.Namespace)
	port := 7687

	uri := fmt.Sprintf("%s://%s:%d", scheme, host, port)
	return uri
}

// CreateDatabaseFromSeedURI creates a database from a seed URI using Neo4j CloudSeedProvider
func (c *Client) CreateDatabaseFromSeedURI(ctx context.Context, databaseName, seedURI string, seedConfig *neo4jv1beta1.SeedConfiguration, options map[string]string, wait bool, ifNotExists bool, cypherVersion string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	// Build CREATE DATABASE query with seed URI
	query := fmt.Sprintf("CREATE DATABASE `%s`", databaseName)

	if ifNotExists {
		query = fmt.Sprintf("CREATE DATABASE `%s` IF NOT EXISTS", databaseName)
	}

	// Add Cypher language version for Neo4j 2025.x (only "5"/"25" are emitted)
	query += cypherLanguageClause(cypherVersion)

	// seedURI, seedConfig (provider-config string), seedRestoreUntil
	// (point-in-time) and any general options are emitted as one documented
	// OPTIONS map; every value is a driver parameter. This replaces the
	// non-grammar `FROM '<uri>'` and `SEED CONFIG {…}` clauses (issue #169).
	optClause, params := c.buildOptionsClause(options, seedURI, seedConfig)
	query += optClause

	// Add WAIT or NOWAIT
	if wait {
		query += " WAIT"
	} else {
		query += " NOWAIT"
	}

	_, err := session.Run(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to create database %s from seed URI: %w", databaseName, err)
	}

	return nil
}

// CreateDatabaseFromSeedURIWithTopology creates a database with topology from a seed URI
func (c *Client) CreateDatabaseFromSeedURIWithTopology(ctx context.Context, databaseName, seedURI string, primaries, secondaries int32, seedConfig *neo4jv1beta1.SeedConfiguration, options map[string]string, wait bool, ifNotExists bool, cypherVersion string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	// Build CREATE DATABASE query
	query := fmt.Sprintf("CREATE DATABASE `%s`", databaseName)

	if ifNotExists {
		query = fmt.Sprintf("CREATE DATABASE `%s` IF NOT EXISTS", databaseName)
	}

	// Add Cypher language version for Neo4j 2025.x (only "5"/"25" are emitted)
	query += cypherLanguageClause(cypherVersion)

	// Add topology if specified
	if primaries > 0 || secondaries > 0 {
		topologyParts := []string{}
		if primaries > 0 {
			if primaries == 1 {
				topologyParts = append(topologyParts, "1 PRIMARY")
			} else {
				topologyParts = append(topologyParts, fmt.Sprintf("%d PRIMARIES", primaries))
			}
		}
		if secondaries > 0 {
			if secondaries == 1 {
				topologyParts = append(topologyParts, "1 SECONDARY")
			} else {
				topologyParts = append(topologyParts, fmt.Sprintf("%d SECONDARIES", secondaries))
			}
		}
		if len(topologyParts) > 0 {
			query += " TOPOLOGY " + strings.Join(topologyParts, " ")
		}
	}

	// seedURI, seedConfig (provider-config string), seedRestoreUntil
	// (point-in-time) and any general options are emitted as one documented
	// OPTIONS map; every value is a driver parameter. This replaces the
	// non-grammar `FROM '<uri>'` and `SEED CONFIG {…}` clauses (issue #169).
	optClause, params := c.buildOptionsClause(options, seedURI, seedConfig)
	query += optClause

	// Add WAIT or NOWAIT
	if wait {
		query += " WAIT"
	} else {
		query += " NOWAIT"
	}

	_, err := session.Run(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to create database %s with topology from seed URI: %w", databaseName, err)
	}

	return nil
}

// PrepareCloudCredentials prepares cloud credentials for seed URI access
func (c *Client) PrepareCloudCredentials(ctx context.Context, k8sClient client.Client, database *neo4jv1beta1.Neo4jDatabase) error {
	// This method prepares cloud credentials in the cluster for CloudSeedProvider
	// It doesn't store credentials directly but ensures the cluster environment is configured

	if database.Spec.SeedCredentials == nil {
		// Using system-wide authentication (IAM roles, workload identity, etc.)
		return nil
	}

	// Get the secret containing credentials
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      database.Spec.SeedCredentials.SecretRef,
		Namespace: database.Namespace,
	}

	if err := k8sClient.Get(ctx, secretKey, secret); err != nil {
		return fmt.Errorf("failed to get seed credentials secret: %w", err)
	}

	// Validate that the secret contains the expected keys based on URI scheme
	seedURI := database.Spec.SeedURI
	if seedURI == "" {
		return fmt.Errorf("seed URI is required when seed credentials are specified")
	}

	// Extract scheme from URI
	schemeEnd := strings.Index(seedURI, "://")
	if schemeEnd == -1 {
		return fmt.Errorf("invalid seed URI format: %s", seedURI)
	}
	scheme := seedURI[:schemeEnd]

	// Validate required keys exist for the scheme
	return c.validateCredentialKeys(secret, scheme)
}

// PrepareCloudCredentialsForShardedDatabase prepares cloud credentials for sharded database seed URIs.
func (c *Client) PrepareCloudCredentialsForShardedDatabase(ctx context.Context, k8sClient client.Client, shardedDB *neo4jv1beta1.Neo4jShardedDatabase) error {
	if shardedDB.Spec.SeedCredentials == nil {
		return nil
	}

	seedURI := shardedDB.Spec.SeedURI
	seedURIs := shardedDB.Spec.SeedURIs
	if seedURI == "" && len(seedURIs) == 0 {
		return fmt.Errorf("seed URI is required when seed credentials are specified")
	}

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      shardedDB.Spec.SeedCredentials.SecretRef,
		Namespace: shardedDB.Namespace,
	}

	if err := k8sClient.Get(ctx, secretKey, secret); err != nil {
		return fmt.Errorf("failed to get seed credentials secret: %w", err)
	}

	schemes := make(map[string]struct{})
	addScheme := func(uri string) error {
		schemeEnd := strings.Index(uri, "://")
		if schemeEnd == -1 {
			return fmt.Errorf("invalid seed URI format: %s", uri)
		}
		schemes[uri[:schemeEnd]] = struct{}{}
		return nil
	}

	if seedURI != "" {
		if err := addScheme(seedURI); err != nil {
			return err
		}
	}
	for _, uri := range seedURIs {
		if err := addScheme(uri); err != nil {
			return err
		}
	}

	if len(schemes) > 1 {
		return fmt.Errorf("seed URIs must use a single scheme when seed credentials are specified")
	}

	for scheme := range schemes {
		if err := c.validateCredentialKeys(secret, scheme); err != nil {
			return err
		}
	}

	return nil
}

// validateCredentialKeys validates that the secret contains required keys for the URI scheme
func (c *Client) validateCredentialKeys(secret *corev1.Secret, scheme string) error {
	hasKey := func(key string) bool {
		_, exists := secret.Data[key]
		return exists
	}

	switch scheme {
	case "s3":
		if !hasKey("AWS_ACCESS_KEY_ID") || !hasKey("AWS_SECRET_ACCESS_KEY") {
			return fmt.Errorf("S3 credentials secret must contain AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY")
		}
	case "gs":
		if !hasKey("GOOGLE_APPLICATION_CREDENTIALS") {
			return fmt.Errorf("GCS credentials secret must contain GOOGLE_APPLICATION_CREDENTIALS")
		}
	case "azb":
		if !hasKey("AZURE_STORAGE_ACCOUNT") {
			return fmt.Errorf("Azure credentials secret must contain AZURE_STORAGE_ACCOUNT")
		}
		if !hasKey("AZURE_STORAGE_KEY") && !hasKey("AZURE_STORAGE_SAS_TOKEN") {
			return fmt.Errorf("Azure credentials secret must contain either AZURE_STORAGE_KEY or AZURE_STORAGE_SAS_TOKEN")
		}
	case "http", "https", "ftp":
		// HTTP/FTP credentials are optional
		break
	default:
		return fmt.Errorf("unsupported seed URI scheme: %s", scheme)
	}

	return nil
}

// RegisterFleetManagementToken calls the fleet management plugin procedure to register
// this Neo4j deployment with Neo4j Aura Fleet Management. The token is obtained from the
// Aura console wizard (Instances → Self-managed → Add deployment).
//
// This only needs to be called once per cluster; the plugin handles subsequent re-connections
// automatically. If auto-rotation is enabled in Aura, the plugin renews the token before expiry.
func (c *Client) RegisterFleetManagementToken(ctx context.Context, token string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	_, err := session.Run(ctx, "CALL fleetManagement.registerToken($token)", map[string]any{
		"token": token,
	})
	if err != nil {
		return fmt.Errorf("failed to register Aura Fleet Management token: %w", err)
	}
	return nil
}

// IsFleetManagementInstalled checks whether the fleet management plugin is loaded and responding.
// Returns true if the plugin is available, false if not (e.g. jar not yet copied to /plugins).
func (c *Client) IsFleetManagementInstalled(ctx context.Context) (bool, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"SHOW PROCEDURES YIELD name WHERE name = 'fleetManagement.status' RETURN count(*) AS n",
		nil,
	)
	if err != nil {
		return false, fmt.Errorf("failed to check fleet management procedures: %w", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		if n, ok := record.Get("n"); ok {
			if count, ok := n.(int64); ok {
				return count > 0, nil
			}
		}
	}
	return false, nil
}

// ===== Property Sharding Support =====
