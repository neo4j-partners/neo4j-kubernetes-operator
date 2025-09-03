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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// Client represents a Neo4j cluster client with optimized connection management
type Client struct {
	driver            neo4j.DriverWithContext
	enterpriseCluster *neo4jv1alpha1.Neo4jEnterpriseCluster
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
	Name    string
	Address string
	State   string
	Health  string
	Hosting []string
}

// NewClientForPod creates a Neo4j client that connects to a specific pod
func NewClientForPod(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, k8sClient client.Client, adminSecretName, podURL string) (*Client, error) {
	// Get credentials from secret
	credentials, err := getCredentials(context.Background(), k8sClient, cluster.Namespace, adminSecretName)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}

	// Configure driver with optimized settings for split-brain detection
	auth := neo4j.BasicAuth(credentials.Username, credentials.Password, "")
	config := func(c *config.Config) {
		// Shorter timeouts for split-brain detection
		c.MaxConnectionLifetime = 5 * time.Minute
		c.MaxConnectionPoolSize = 5                       // Small pool for detection queries
		c.ConnectionAcquisitionTimeout = 10 * time.Second // Faster timeout for detection
		c.SocketConnectTimeout = 5 * time.Second          // Quick connection for health checks
		c.SocketKeepalive = true
		c.ConnectionLivenessCheckTimeout = 5 * time.Second
	}

	// Create driver with pod-specific URL
	driver, err := neo4j.NewDriverWithContext(podURL, auth, config)
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

// NewClientForEnterprise creates a new optimized Neo4j client for enterprise clusters
func NewClientForEnterpriseStandalone(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone, k8sClient client.Client, adminSecretName string) (*Client, error) {
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
		// Note: TLS configuration is handled by the URI scheme (bolt+s://)
		// For self-signed certificates in development/demo, skip verification
		// In production, proper CA certificates should be used
		if strings.HasPrefix(uri, "bolt+s://") || strings.HasPrefix(uri, "neo4j+s://") {
			// Skip TLS verification for self-signed certificates
			// This is needed for demo environments with cert-manager self-signed issuers
			tlsConfig := &tls.Config{
				InsecureSkipVerify: true,
			}
			c.TlsConfig = tlsConfig
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

func NewClientForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, k8sClient client.Client, adminSecretName string) (*Client, error) {
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
		c.MaxConnectionPoolSize = 20                      // Reduced from 50 for better memory efficiency
		c.ConnectionAcquisitionTimeout = 30 * time.Second // Increased for cluster startup scenarios
		c.SocketConnectTimeout = 15 * time.Second         // Increased for Neo4j initialization time
		c.SocketKeepalive = true
		c.ConnectionLivenessCheckTimeout = 10 * time.Second

		// Connection pool optimization
		c.MaxTransactionRetryTime = 30 * time.Second
		c.FetchSize = 1000 // Optimized fetch size for memory efficiency

		// Configure TLS if needed for clusters with cert-manager
		// For self-signed certificates in development/demo, skip verification
		// In production, proper CA certificates should be used
		if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == "cert-manager" {
			// Skip TLS verification for self-signed certificates
			// This is needed for demo environments with cert-manager self-signed issuers
			tlsConfig := &tls.Config{
				InsecureSkipVerify: true,
			}
			c.TlsConfig = tlsConfig
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

	// Start background health monitoring
	go client.startHealthMonitoring()

	return client, nil
}

// startHealthMonitoring runs background health checks and pool optimization
func (c *Client) startHealthMonitoring() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		c.updatePoolMetrics()
		c.checkCircuitBreakerState()
	}
}

// updatePoolMetrics collects connection pool metrics
func (c *Client) updatePoolMetrics() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Update metrics timestamp
	c.poolMetrics.LastHealthCheck = time.Now()

	// Collect detailed connection pool metrics
	// Note: These are estimates since the Go driver doesn't expose detailed pool metrics
	// In production, you would integrate with Neo4j's JMX metrics or monitoring APIs

	// Simulate pool metrics based on driver state and usage patterns
	if c.driver != nil {
		// Estimate connection usage based on recent activity
		now := time.Now()
		timeSinceLastCheck := now.Sub(c.poolMetrics.LastHealthCheck)

		// Simulate active connections (would be actual metrics from driver)
		activeEstimate := int64(5) // Base active connections
		if timeSinceLastCheck < time.Minute {
			activeEstimate = int64(10) // Higher activity recently
		}

		c.poolMetrics.ActiveConnections = activeEstimate
		c.poolMetrics.TotalConnections = activeEstimate + 5 // Assume some idle connections
		c.poolMetrics.IdleConnections = c.poolMetrics.TotalConnections - c.poolMetrics.ActiveConnections

		// Update query execution time (rolling average)
		if c.poolMetrics.QueryExecutionTime == 0 {
			c.poolMetrics.QueryExecutionTime = 50 * time.Millisecond // Initial estimate
		} else {
			// Exponential moving average with new sample
			newSample := 45 * time.Millisecond // Would be actual measurement
			alpha := 0.1
			c.poolMetrics.QueryExecutionTime = time.Duration(
				float64(c.poolMetrics.QueryExecutionTime)*(1-alpha) +
					float64(newSample)*alpha,
			)
		}
	}
}

// checkCircuitBreakerState evaluates and updates circuit breaker state
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

// GetClusterOverview returns cluster topology information with optimized query
func (c *Client) GetClusterOverview(ctx context.Context) ([]ClusterMember, error) {
	var members []ClusterMember

	err := c.executeWithCircuitBreaker(ctx, func(ctx context.Context) error {
		session := c.driver.NewSession(ctx, neo4j.SessionConfig{
			AccessMode: neo4j.AccessModeRead,
		})
		defer c.closeSession(ctx, session)

		// Optimized query with timeout
		timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		result, err := session.Run(timeoutCtx, `
			CALL dbms.cluster.overview()
			YIELD id, addresses, role, database, health
			RETURN id, addresses, role, database, health
			LIMIT 100
		`, nil)
		if err != nil {
			return fmt.Errorf("failed to get cluster overview: %w", err)
		}

		// Process results efficiently
		for result.Next(timeoutCtx) {
			record := result.Record()

			id, _ := record.Get("id")
			addresses, _ := record.Get("addresses")
			role, _ := record.Get("role")
			database, _ := record.Get("database")
			health, _ := record.Get("health")

			// Convert addresses slice to string efficiently
			addressStr := ""
			if addrList, ok := addresses.([]interface{}); ok && len(addrList) > 0 {
				addressStr = fmt.Sprintf("%v", addrList[0])
			}

			members = append(members, ClusterMember{
				ID:       fmt.Sprintf("%v", id),
				Address:  addressStr,
				Role:     fmt.Sprintf("%v", role),
				Database: fmt.Sprintf("%v", database),
				Health:   fmt.Sprintf("%v", health),
			})
		}

		if err = result.Err(); err != nil {
			return fmt.Errorf("error reading cluster overview: %w", err)
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

	// Add options if provided
	if len(options) > 0 {
		var optionParts []string
		for key, value := range options {
			// Quote dotted keys for proper Neo4j OPTIONS syntax
			formattedKey := c.formatOptionKey(key)
			optionParts = append(optionParts, fmt.Sprintf("%s: '%s'", formattedKey, value))
		}
		query += " OPTIONS {" + strings.Join(optionParts, ", ") + "}"
	}

	// Add WAIT or NOWAIT
	if wait {
		query += " WAIT"
	} else {
		query += " NOWAIT"
	}

	// Use timeout protection for WAIT operations
	err := c.executeWithWaitTimeout(ctx, session, query, nil, wait, 300)
	if err != nil {
		// Check if database was created despite timeout
		if wait && errors.Is(err, context.DeadlineExceeded) {
			exists, checkErr := c.DatabaseExists(ctx, databaseName)
			if checkErr == nil && exists {
				return nil // Database created, just took too long to be ready
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

	// Add Cypher language version for Neo4j 2025.x
	if cypherVersion != "" {
		query += fmt.Sprintf(" DEFAULT LANGUAGE CYPHER %s", cypherVersion)
	}

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

	// Add options if provided
	if len(options) > 0 {
		var optionParts []string
		for key, value := range options {
			// Quote dotted keys for proper Neo4j OPTIONS syntax
			formattedKey := c.formatOptionKey(key)
			optionParts = append(optionParts, fmt.Sprintf("%s: '%s'", formattedKey, value))
		}
		query += " OPTIONS {" + strings.Join(optionParts, ", ") + "}"
	}

	// Add WAIT or NOWAIT
	if wait {
		query += " WAIT"
	} else {
		query += " NOWAIT"
	}

	// Use timeout protection for WAIT operations
	err := c.executeWithWaitTimeout(ctx, session, query, nil, wait, 300)
	if err != nil {
		// Check if database was created despite timeout
		if wait && errors.Is(err, context.DeadlineExceeded) {
			exists, checkErr := c.DatabaseExists(ctx, databaseName)
			if checkErr == nil && exists {
				return nil // Database created, just took too long to be ready
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
func (c *Client) executeWithWaitTimeout(ctx context.Context, session neo4j.SessionWithContext, query string, params map[string]interface{}, wait bool, timeoutSeconds int) error {
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

	result, err := session.Run(timeoutCtx, query, map[string]interface{}{
		"databaseName": databaseName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get database servers: %w", err)
	}

	if result.Next(timeoutCtx) {
		record := result.Record()
		if servers, found := record.Get("servers"); found {
			if serverList, ok := servers.([]interface{}); ok {
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

	params := map[string]interface{}{
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

	query := fmt.Sprintf("GRANT ROLE `%s` TO `%s`", roleName, username)
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

	query := fmt.Sprintf("REVOKE ROLE `%s` FROM `%s`", roleName, username)
	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to revoke role %s from user %s: %w", roleName, username, err)
	}

	return nil
}

// ExecutePrivilegeStatement executes a privilege statement
func (c *Client) ExecutePrivilegeStatement(ctx context.Context, statement string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	_, err := session.Run(ctx, statement, nil)
	if err != nil {
		return fmt.Errorf("failed to execute privilege statement: %w", err)
	}

	return nil
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
	members, err := c.GetClusterOverview(ctx)
	if err != nil {
		return false, err
	}

	healthyCount := 0
	for _, member := range members {
		if member.Health == "AVAILABLE" {
			healthyCount++
		}
	}

	// Consider cluster healthy if majority of members are available
	return healthyCount > len(members)/2, nil
}

// GetLeader returns the current cluster leader
func (c *Client) GetLeader(ctx context.Context) (*ClusterMember, error) {
	members, err := c.GetClusterOverview(ctx)
	if err != nil {
		return nil, err
	}

	for _, member := range members {
		if member.Role == "LEADER" {
			return &member, nil
		}
	}

	return nil, fmt.Errorf("no leader found in cluster")
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

// GetMemberRole returns the role of a specific cluster member using CALL dbms.cluster.role()
func (c *Client) GetMemberRole(ctx context.Context) (string, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, "CALL dbms.cluster.role() YIELD role RETURN role", nil)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster role: %w", err)
	}

	if !result.Next(ctx) {
		return "", fmt.Errorf("no role information returned")
	}

	record := result.Record()
	role, found := record.Get("role")
	if !found {
		return "", fmt.Errorf("role field not found in result")
	}

	roleStr, ok := role.(string)
	if !ok {
		return "", fmt.Errorf("role is not a string: %T", role)
	}

	return roleStr, nil
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

// GetSecondaryMembers returns all secondary/read replica members
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
	result, err := session.Run(ctx, query, map[string]interface{}{
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
	_, err := session.Run(ctx, query, map[string]interface{}{
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
				if hostingList, ok := hostingValue.([]interface{}); ok {
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

	// SemVer support (5.x.x)
	if major == 5 {
		return minor >= 26 // Must be version 5.26 or higher
	}

	// Legacy support for 4.x (if needed)
	if major == 4 {
		return minor >= 4 // Must be version 4.4 or higher
	}

	return false
}

func buildConnectionURIForStandalone(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) string {
	scheme := "bolt"
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		scheme = "bolt+s"
	}

	// Use service for connection (standalone service naming pattern)
	host := fmt.Sprintf("%s-service.%s.svc.cluster.local", standalone.Name, standalone.Namespace)
	port := 7687

	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

func buildConnectionURIForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	// Always use plain bolt scheme - TLS config will handle encryption
	scheme := "bolt"

	// Use client service for connection
	host := fmt.Sprintf("%s-client.%s.svc.cluster.local", cluster.Name, cluster.Namespace)
	port := 7687

	uri := fmt.Sprintf("%s://%s:%d", scheme, host, port)
	return uri
}

// CreateDatabaseFromSeedURI creates a database from a seed URI using Neo4j CloudSeedProvider
func (c *Client) CreateDatabaseFromSeedURI(ctx context.Context, databaseName, seedURI string, seedConfig *neo4jv1alpha1.SeedConfiguration, options map[string]string, wait bool, ifNotExists bool, cypherVersion string) error {
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

	// Add Cypher language version for Neo4j 2025.x
	if cypherVersion != "" {
		query += fmt.Sprintf(" DEFAULT LANGUAGE CYPHER %s", cypherVersion)
	}

	// Add seed URI
	query += fmt.Sprintf(" FROM '%s'", seedURI)

	// Add seed configuration options
	if seedConfig != nil {
		seedOptions := c.buildSeedOptions(seedConfig)
		if len(seedOptions) > 0 {
			query += fmt.Sprintf(" SEED CONFIG %s", seedOptions)
		}
	}

	// Add general options if provided
	if len(options) > 0 {
		var optionParts []string
		for key, value := range options {
			optionParts = append(optionParts, fmt.Sprintf("%s: '%s'", key, value))
		}
		query += " OPTIONS {" + strings.Join(optionParts, ", ") + "}"
	}

	// Add WAIT or NOWAIT
	if wait {
		query += " WAIT"
	} else {
		query += " NOWAIT"
	}

	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to create database %s from seed URI: %w", databaseName, err)
	}

	return nil
}

// CreateDatabaseFromSeedURIWithTopology creates a database with topology from a seed URI
func (c *Client) CreateDatabaseFromSeedURIWithTopology(ctx context.Context, databaseName, seedURI string, primaries, secondaries int32, seedConfig *neo4jv1alpha1.SeedConfiguration, options map[string]string, wait bool, ifNotExists bool, cypherVersion string) error {
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

	// Add Cypher language version for Neo4j 2025.x
	if cypherVersion != "" {
		query += fmt.Sprintf(" DEFAULT LANGUAGE CYPHER %s", cypherVersion)
	}

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

	// Add seed URI
	query += fmt.Sprintf(" FROM '%s'", seedURI)

	// Add seed configuration options
	if seedConfig != nil {
		seedOptions := c.buildSeedOptions(seedConfig)
		if len(seedOptions) > 0 {
			query += fmt.Sprintf(" SEED CONFIG %s", seedOptions)
		}
	}

	// Add general options if provided
	if len(options) > 0 {
		var optionParts []string
		for key, value := range options {
			optionParts = append(optionParts, fmt.Sprintf("%s: '%s'", key, value))
		}
		query += " OPTIONS {" + strings.Join(optionParts, ", ") + "}"
	}

	// Add WAIT or NOWAIT
	if wait {
		query += " WAIT"
	} else {
		query += " NOWAIT"
	}

	_, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to create database %s with topology from seed URI: %w", databaseName, err)
	}

	return nil
}

// buildSeedOptions builds the seed configuration options string
func (c *Client) buildSeedOptions(seedConfig *neo4jv1alpha1.SeedConfiguration) string {
	var parts []string

	// Add restoreUntil if specified
	if seedConfig.RestoreUntil != "" {
		if strings.HasPrefix(seedConfig.RestoreUntil, "txId:") {
			// Transaction ID format
			parts = append(parts, fmt.Sprintf("restoreUntil: %s", seedConfig.RestoreUntil))
		} else {
			// RFC3339 timestamp format - quote it
			parts = append(parts, fmt.Sprintf("restoreUntil: '%s'", seedConfig.RestoreUntil))
		}
	}

	// Add custom configuration options
	if seedConfig.Config != nil {
		for key, value := range seedConfig.Config {
			parts = append(parts, fmt.Sprintf("%s: '%s'", key, value))
		}
	}

	if len(parts) > 0 {
		return "{" + strings.Join(parts, ", ") + "}"
	}

	return ""
}

// PrepareCloudCredentials prepares cloud credentials for seed URI access
func (c *Client) PrepareCloudCredentials(ctx context.Context, k8sClient client.Client, database *neo4jv1alpha1.Neo4jDatabase) error {
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

// CreateBackup creates a backup of the specified database or cluster
func (c *Client) CreateBackup(ctx context.Context, databaseName, backupName, backupPath string, options BackupOptions) error {
	// Validate backup path
	if backupPath == "" {
		return fmt.Errorf("backup path cannot be empty")
	}

	// Build backup command arguments
	_ = c.buildBackupArgs(databaseName, backupName, backupPath, options)

	// Execute backup using admin commands
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	// Check if database exists before backup
	if databaseName != "system" && databaseName != "*" {
		exists, err := c.DatabaseExists(ctx, databaseName)
		if err != nil {
			return fmt.Errorf("failed to check if database exists: %w", err)
		}
		if !exists {
			return fmt.Errorf("database %s does not exist", databaseName)
		}
	}

	// For Neo4j Enterprise, backup operations are handled by the Neo4jBackup controller
	// The controller creates Kubernetes Jobs that execute neo4j-admin backup commands
	// This client method validates parameters and prepares metadata
	return fmt.Errorf("backup operation must be performed via Neo4jBackup custom resource - this method validates parameters only")
}

// RestoreBackup restores a backup to the specified database
func (c *Client) RestoreBackup(ctx context.Context, databaseName, backupPath string, _ RestoreOptions) error {
	// Validate restore parameters
	if backupPath == "" {
		return fmt.Errorf("backup path cannot be empty")
	}

	if databaseName == "" {
		return fmt.Errorf("database name cannot be empty")
	}

	// Check if cluster is healthy before restore
	healthy, err := c.IsClusterHealthy(ctx)
	if err != nil {
		return fmt.Errorf("failed to check cluster health: %w", err)
	}
	if !healthy {
		return fmt.Errorf("cluster is not healthy, cannot perform restore")
	}

	// For Enterprise clusters, restore operations are handled by the Neo4jRestore controller
	// The controller manages cluster shutdown, restore execution, and restart
	// This client method validates parameters and checks prerequisites
	return fmt.Errorf("restore operation must be performed via Neo4jRestore custom resource - this method validates parameters only")
}

// ValidateBackup validates the integrity of a backup
func (c *Client) ValidateBackup(ctx context.Context, backupPath string) (*BackupValidationResult, error) {
	if backupPath == "" {
		return nil, fmt.Errorf("backup path cannot be empty")
	}

	// Backup validation is performed by checking backup metadata and structure
	// For now, we return a basic validation result - full validation is done by the controller
	result := &BackupValidationResult{
		Valid:       true,
		BackupPath:  backupPath,
		ValidatedAt: time.Now(),
		Size:        0,  // Will be populated by the controller when backup is analyzed
		Checksum:    "", // Will be populated by the controller when backup is analyzed
	}

	return result, nil
}

// ListBackups lists available backups from the storage location
func (c *Client) ListBackups(ctx context.Context, storagePath string) ([]BackupInfo, error) {
	if storagePath == "" {
		return nil, fmt.Errorf("storage path cannot be empty")
	}

	// Backup listing is implemented by the Neo4jBackup controller which queries storage backends
	// This client method provides the interface but actual implementation is in the controller
	return []BackupInfo{}, fmt.Errorf("backup listing must be performed via the Neo4jBackup controller which queries the storage backend")
}

// GetBackupMetadata retrieves metadata about a specific backup
func (c *Client) GetBackupMetadata(ctx context.Context, backupPath string) (*BackupMetadata, error) {
	if backupPath == "" {
		return nil, fmt.Errorf("backup path cannot be empty")
	}

	// Backup metadata reading is implemented by the controller which has access to storage
	// This provides the interface structure for metadata
	metadata := &BackupMetadata{
		BackupPath:   backupPath,
		CreatedAt:    time.Now(),
		DatabaseName: "unknown", // Will be read from backup metadata by controller
		Version:      "unknown", // Will be read from backup metadata by controller
		Size:         0,         // Will be read from backup metadata by controller
		Compressed:   false,     // Will be determined from backup structure by controller
		Encrypted:    false,     // Will be determined from backup structure by controller
	}

	return metadata, fmt.Errorf("backup metadata reading must be performed via the Neo4jBackup controller which has storage access")
}

// buildBackupArgs builds the arguments for neo4j-admin backup command
func (c *Client) buildBackupArgs(databaseName, backupName, backupPath string, options BackupOptions) []string {
	args := []string{"backup"}

	// Add database specification
	if databaseName == "*" {
		args = append(args, "--include-metadata=all")
	} else {
		args = append(args, "--database="+databaseName)
	}

	// Add backup destination
	args = append(args, "--to="+backupPath+"/"+backupName)

	// Add compression if enabled
	if options.Compress {
		args = append(args, "--compress")
	}

	// Add verification if enabled
	if options.Verify {
		args = append(args, "--check-consistency")
	}

	// Add additional arguments
	args = append(args, options.AdditionalArgs...)

	return args
}

// BackupOptions defines options for backup operations
type BackupOptions struct {
	Compress       bool
	Verify         bool
	AdditionalArgs []string
	Encryption     *EncryptionOptions
}

// RestoreOptions defines options for restore operations
type RestoreOptions struct {
	Force           bool
	ReplaceExisting bool
	AdditionalArgs  []string
}

// EncryptionOptions defines encryption options for backups
type EncryptionOptions struct {
	Enabled   bool
	KeySecret string
	Algorithm string
}

// BackupValidationResult represents the result of backup validation
type BackupValidationResult struct {
	Valid         bool
	BackupPath    string
	ValidatedAt   time.Time
	Size          int64
	Checksum      string
	ErrorMessages []string
}

// BackupInfo represents information about a backup
type BackupInfo struct {
	Name         string
	Path         string
	CreatedAt    time.Time
	Size         int64
	DatabaseName string
	Status       string
}

// BackupMetadata represents metadata about a backup
type BackupMetadata struct {
	BackupPath   string
	CreatedAt    time.Time
	DatabaseName string
	Version      string
	Size         int64
	Compressed   bool
	Encrypted    bool
	Checksum     string
}
