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
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// SecurityCoordinator manages the ordering of security resource reconciliation
// Ensures: Roles → Grants → Users dependency chain
type SecurityCoordinator struct {
	client.Client

	// Channels for coordinating reconciliation order
	roleQueue  chan types.NamespacedName
	grantQueue chan types.NamespacedName
	userQueue  chan types.NamespacedName

	// Tracking maps
	rolesMutex  sync.RWMutex
	grantsMutex sync.RWMutex
	usersMutex  sync.RWMutex

	pendingRoles  map[string][]types.NamespacedName // cluster -> roles
	pendingGrants map[string][]types.NamespacedName // cluster -> grants
	pendingUsers  map[string][]types.NamespacedName // cluster -> users

	// Completed tracking
	completedRoles  map[types.NamespacedName]time.Time
	completedGrants map[types.NamespacedName]time.Time

	// Reconciler references
	roleReconciler  *Neo4jRoleReconciler
	grantReconciler *Neo4jGrantReconciler
	userReconciler  *Neo4jUserReconciler

	// Control channels
	stopChan chan struct{}
	started  bool
	mutex    sync.Mutex
}

// NewSecurityCoordinator creates a new security coordinator
func NewSecurityCoordinator(client client.Client) *SecurityCoordinator {
	return &SecurityCoordinator{
		Client:          client,
		roleQueue:       make(chan types.NamespacedName, 100),
		grantQueue:      make(chan types.NamespacedName, 100),
		userQueue:       make(chan types.NamespacedName, 100),
		pendingRoles:    make(map[string][]types.NamespacedName),
		pendingGrants:   make(map[string][]types.NamespacedName),
		pendingUsers:    make(map[string][]types.NamespacedName),
		completedRoles:  make(map[types.NamespacedName]time.Time),
		completedGrants: make(map[types.NamespacedName]time.Time),
		stopChan:        make(chan struct{}),
	}
}

// SetReconcilers sets the reconciler references
func (sc *SecurityCoordinator) SetReconcilers(
	roleReconciler *Neo4jRoleReconciler,
	grantReconciler *Neo4jGrantReconciler,
	userReconciler *Neo4jUserReconciler,
) {
	sc.roleReconciler = roleReconciler
	sc.grantReconciler = grantReconciler
	sc.userReconciler = userReconciler
}

// Start begins the security coordination process
func (sc *SecurityCoordinator) Start(ctx context.Context) error {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	if sc.started {
		return fmt.Errorf("security coordinator already started")
	}

	sc.started = true

	// Start processing goroutines
	go sc.processRoles(ctx)
	go sc.processGrants(ctx)
	go sc.processUsers(ctx)
	go func() { sc.cleanupCompleted() }()

	return nil
}

// Stop stops the security coordination process
func (sc *SecurityCoordinator) Stop() {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	if !sc.started {
		return
	}

	close(sc.stopChan)
	sc.started = false
}

// ScheduleRoleReconcile schedules a role for reconciliation
func (sc *SecurityCoordinator) ScheduleRoleReconcile(req types.NamespacedName, clusterName string) {
	sc.rolesMutex.Lock()
	defer sc.rolesMutex.Unlock()

	// Add to pending roles for the cluster
	sc.pendingRoles[clusterName] = append(sc.pendingRoles[clusterName], req)

	// Queue for immediate processing
	select {
	case sc.roleQueue <- req:
	default:
		// Queue full, log warning
		log.Log.Info("Role reconcile queue full, dropping request", "role", req)
	}
}

// ScheduleGrantReconcile schedules a grant for reconciliation (after roles are ready)
func (sc *SecurityCoordinator) ScheduleGrantReconcile(req types.NamespacedName, clusterName string) {
	sc.grantsMutex.Lock()
	defer sc.grantsMutex.Unlock()

	// Add to pending grants for the cluster
	sc.pendingGrants[clusterName] = append(sc.pendingGrants[clusterName], req)

	// Check if roles are ready for this cluster
	if sc.areRolesReadyForCluster(clusterName) {
		select {
		case sc.grantQueue <- req:
		default:
			log.Log.Info("Grant reconcile queue full, dropping request", "grant", req)
		}
	}
}

// ScheduleUserReconcile schedules a user for reconciliation (after roles and grants are ready)
func (sc *SecurityCoordinator) ScheduleUserReconcile(req types.NamespacedName, clusterName string) {
	sc.usersMutex.Lock()
	defer sc.usersMutex.Unlock()

	// Add to pending users for the cluster
	sc.pendingUsers[clusterName] = append(sc.pendingUsers[clusterName], req)

	// Check if roles and grants are ready for this cluster
	if sc.areRolesReadyForCluster(clusterName) && sc.areGrantsReadyForCluster(clusterName) {
		select {
		case sc.userQueue <- req:
		default:
			log.Log.Info("User reconcile queue full, dropping request", "user", req)
		}
	}
}

// OnRoleReconcileComplete notifies that a role reconciliation is complete
func (sc *SecurityCoordinator) OnRoleReconcileComplete(req types.NamespacedName, clusterName string, success bool) {
	sc.rolesMutex.Lock()
	defer sc.rolesMutex.Unlock()

	if success {
		sc.completedRoles[req] = time.Now()

		// Remove from pending
		sc.removePendingRole(clusterName, req)

		// Check if we can now process grants for this cluster
		if sc.areRolesReadyForCluster(clusterName) {
			sc.triggerGrantsForCluster(clusterName)
		}
	}
}

// OnGrantReconcileComplete notifies that a grant reconciliation is complete
func (sc *SecurityCoordinator) OnGrantReconcileComplete(req types.NamespacedName, clusterName string, success bool) {
	sc.grantsMutex.Lock()
	defer sc.grantsMutex.Unlock()

	if success {
		sc.completedGrants[req] = time.Now()

		// Remove from pending
		sc.removePendingGrant(clusterName, req)

		// Check if we can now process users for this cluster
		if sc.areGrantsReadyForCluster(clusterName) {
			sc.triggerUsersForCluster(clusterName)
		}
	}
}

// OnUserReconcileComplete notifies that a user reconciliation is complete
func (sc *SecurityCoordinator) OnUserReconcileComplete(req types.NamespacedName, clusterName string, success bool) {
	sc.usersMutex.Lock()
	defer sc.usersMutex.Unlock()

	if success {
		// Remove from pending
		sc.removePendingUser(clusterName, req)
	}
}

// processRoles processes roles in dependency order
func (sc *SecurityCoordinator) processRoles(ctx context.Context) {
	logger := log.FromContext(ctx)
	logger.Info("Starting role processing goroutine")

	for {
		select {
		case <-sc.stopChan:
			logger.Info("Stopping role processing")
			return
		case roleKey := <-sc.roleQueue:
			logger.Info("Processing role", "role", roleKey)

			if sc.roleReconciler != nil {
				// Reconcile the role
				req := ctrl.Request{NamespacedName: roleKey}
				result, err := sc.roleReconciler.Reconcile(ctx, req)

				if err != nil {
					logger.Error(err, "Failed to reconcile role", "role", roleKey)
					// Retry after delay
					go func() {
						time.Sleep(30 * time.Second)
						select {
						case sc.roleQueue <- roleKey:
						case <-sc.stopChan:
						}
					}()
					continue
				}

				// Mark as completed
				sc.rolesMutex.Lock()
				sc.completedRoles[roleKey] = time.Now()
				sc.rolesMutex.Unlock()

				// Trigger dependent grants
				sc.triggerDependentGrants(ctx, roleKey)

				if result.RequeueAfter > 0 {
					go func() {
						time.Sleep(result.RequeueAfter)
						select {
						case sc.roleQueue <- roleKey:
						case <-sc.stopChan:
						}
					}()
				}
			}
		}
	}
}

// processGrants processes grants after their role dependencies are ready
func (sc *SecurityCoordinator) processGrants(ctx context.Context) {
	logger := log.FromContext(ctx)
	logger.Info("Starting grant processing goroutine")

	for {
		select {
		case <-sc.stopChan:
			logger.Info("Stopping grant processing")
			return
		case grantKey := <-sc.grantQueue:
			logger.Info("Processing grant", "grant", grantKey)

			// Check if role dependencies are satisfied
			if !sc.areRoleDependenciesSatisfied(ctx, grantKey) {
				logger.Info("Role dependencies not satisfied, requeueing grant", "grant", grantKey)
				go func() {
					time.Sleep(10 * time.Second)
					select {
					case sc.grantQueue <- grantKey:
					case <-sc.stopChan:
					}
				}()
				continue
			}

			if sc.grantReconciler != nil {
				// Reconcile the grant
				req := ctrl.Request{NamespacedName: grantKey}
				result, err := sc.grantReconciler.Reconcile(ctx, req)

				if err != nil {
					logger.Error(err, "Failed to reconcile grant", "grant", grantKey)
					// Retry after delay
					go func() {
						time.Sleep(30 * time.Second)
						select {
						case sc.grantQueue <- grantKey:
						case <-sc.stopChan:
						}
					}()
					continue
				}

				// Mark as completed
				sc.grantsMutex.Lock()
				sc.completedGrants[grantKey] = time.Now()
				sc.grantsMutex.Unlock()

				// Trigger dependent users
				sc.triggerDependentUsers(ctx, grantKey)

				if result.RequeueAfter > 0 {
					go func() {
						time.Sleep(result.RequeueAfter)
						select {
						case sc.grantQueue <- grantKey:
						case <-sc.stopChan:
						}
					}()
				}
			}
		}
	}
}

// processUsers processes users after their role and grant dependencies are ready
func (sc *SecurityCoordinator) processUsers(ctx context.Context) {
	logger := log.FromContext(ctx)
	logger.Info("Starting user processing goroutine")

	for {
		select {
		case <-sc.stopChan:
			logger.Info("Stopping user processing")
			return
		case userKey := <-sc.userQueue:
			logger.Info("Processing user", "user", userKey)

			// Check if grant dependencies are satisfied
			if !sc.areGrantDependenciesSatisfied(ctx, userKey) {
				logger.Info("Grant dependencies not satisfied, requeueing user", "user", userKey)
				go func() {
					time.Sleep(10 * time.Second)
					select {
					case sc.userQueue <- userKey:
					case <-sc.stopChan:
					}
				}()
				continue
			}

			if sc.userReconciler != nil {
				// Reconcile the user
				req := ctrl.Request{NamespacedName: userKey}
				result, err := sc.userReconciler.Reconcile(ctx, req)

				if err != nil {
					logger.Error(err, "Failed to reconcile user", "user", userKey)
					// Retry after delay
					go func() {
						time.Sleep(30 * time.Second)
						select {
						case sc.userQueue <- userKey:
						case <-sc.stopChan:
						}
					}()
					continue
				}

				if result.RequeueAfter > 0 {
					go func() {
						time.Sleep(result.RequeueAfter)
						select {
						case sc.userQueue <- userKey:
						case <-sc.stopChan:
						}
					}()
				}
			}
		}
	}
}

// cleanupCompleted periodically cleans up old completion records
func (sc *SecurityCoordinator) cleanupCompleted() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-sc.stopChan:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-24 * time.Hour)

			sc.rolesMutex.Lock()
			for key, completedAt := range sc.completedRoles {
				if completedAt.Before(cutoff) {
					delete(sc.completedRoles, key)
				}
			}
			sc.rolesMutex.Unlock()

			sc.grantsMutex.Lock()
			for key, completedAt := range sc.completedGrants {
				if completedAt.Before(cutoff) {
					delete(sc.completedGrants, key)
				}
			}
			sc.grantsMutex.Unlock()
		}
	}
}

// Helper functions

func (sc *SecurityCoordinator) triggerDependentGrants(ctx context.Context, roleKey types.NamespacedName) {
	logger := log.FromContext(ctx)

	// Find grants that depend on this role
	sc.grantsMutex.RLock()
	clusterName := sc.extractClusterName(roleKey.Name)
	pendingGrants := sc.pendingGrants[clusterName]
	sc.grantsMutex.RUnlock()

	for _, grantKey := range pendingGrants {
		logger.Info("Triggering dependent grant", "grant", grantKey, "role", roleKey)
		select {
		case sc.grantQueue <- grantKey:
		case <-sc.stopChan:
			return
		default:
			// Queue is full, skip
		}
	}
}

func (sc *SecurityCoordinator) triggerDependentUsers(ctx context.Context, grantKey types.NamespacedName) {
	logger := log.FromContext(ctx)

	// Find users that depend on this grant
	sc.usersMutex.RLock()
	clusterName := sc.extractClusterName(grantKey.Name)
	pendingUsers := sc.pendingUsers[clusterName]
	sc.usersMutex.RUnlock()

	for _, userKey := range pendingUsers {
		logger.Info("Triggering dependent user", "user", userKey, "grant", grantKey)
		select {
		case sc.userQueue <- userKey:
		case <-sc.stopChan:
			return
		default:
			// Queue is full, skip
		}
	}
}

func (sc *SecurityCoordinator) areRoleDependenciesSatisfied(ctx context.Context, grantKey types.NamespacedName) bool {
	// Get the grant resource to check its role dependencies
	grant := &neo4jv1alpha1.Neo4jGrant{}
	if err := sc.Get(ctx, grantKey, grant); err != nil {
		return false
	}

	// Check if the target is a role and if it exists
	sc.rolesMutex.RLock()
	defer sc.rolesMutex.RUnlock()

	if grant.Spec.Target.Kind == "Role" {
		roleKey := types.NamespacedName{
			Name:      grant.Spec.Target.Name,
			Namespace: grantKey.Namespace,
		}
		if _, completed := sc.completedRoles[roleKey]; !completed {
			return false
		}
	}

	// Check privilege rules for role dependencies
	for _, rule := range grant.Spec.PrivilegeRules {
		if rule.RoleName != "" {
			roleKey := types.NamespacedName{
				Name:      rule.RoleName,
				Namespace: grantKey.Namespace,
			}
			if _, completed := sc.completedRoles[roleKey]; !completed {
				return false
			}
		}
	}

	return true
}

func (sc *SecurityCoordinator) areGrantDependenciesSatisfied(ctx context.Context, userKey types.NamespacedName) bool {
	// Get the user resource to check its role dependencies
	user := &neo4jv1alpha1.Neo4jUser{}
	if err := sc.Get(ctx, userKey, user); err != nil {
		return false
	}

	// Check if all required roles are completed
	sc.rolesMutex.RLock()
	defer sc.rolesMutex.RUnlock()

	for _, roleRef := range user.Spec.Roles {
		roleKey := types.NamespacedName{
			Name:      roleRef,
			Namespace: userKey.Namespace,
		}
		if _, completed := sc.completedRoles[roleKey]; !completed {
			return false
		}
	}

	// Check if all grants targeting this user are completed
	sc.grantsMutex.RLock()
	defer sc.grantsMutex.RUnlock()

	// In a real implementation, we would list all grants and check which ones target this user
	// For now, we assume grants are satisfied if roles are satisfied
	return true
}

func (sc *SecurityCoordinator) extractClusterName(resourceName string) string {
	// Extract cluster name from resource name (assuming format: cluster-name-role-name)
	parts := strings.Split(resourceName, "-")
	if len(parts) >= 2 {
		return strings.Join(parts[:len(parts)-1], "-")
	}
	return resourceName
}

// Helper methods

func (sc *SecurityCoordinator) areRolesReadyForCluster(clusterName string) bool {
	sc.rolesMutex.RLock()
	defer sc.rolesMutex.RUnlock()

	pending, exists := sc.pendingRoles[clusterName]
	return !exists || len(pending) == 0
}

func (sc *SecurityCoordinator) areGrantsReadyForCluster(clusterName string) bool {
	sc.grantsMutex.RLock()
	defer sc.grantsMutex.RUnlock()

	pending, exists := sc.pendingGrants[clusterName]
	return !exists || len(pending) == 0
}

func (sc *SecurityCoordinator) triggerGrantsForCluster(clusterName string) {
	sc.grantsMutex.RLock()
	grants := make([]types.NamespacedName, len(sc.pendingGrants[clusterName]))
	copy(grants, sc.pendingGrants[clusterName])
	sc.grantsMutex.RUnlock()

	for _, grant := range grants {
		select {
		case sc.grantQueue <- grant:
		default:
			return // Queue full
		}
	}
}

func (sc *SecurityCoordinator) triggerUsersForCluster(clusterName string) {
	sc.usersMutex.RLock()
	users := make([]types.NamespacedName, len(sc.pendingUsers[clusterName]))
	copy(users, sc.pendingUsers[clusterName])
	sc.usersMutex.RUnlock()

	for _, user := range users {
		select {
		case sc.userQueue <- user:
		default:
			return // Queue full
		}
	}
}

func (sc *SecurityCoordinator) removePendingRole(clusterName string, req types.NamespacedName) {
	pending := sc.pendingRoles[clusterName]
	for i, r := range pending {
		if r == req {
			sc.pendingRoles[clusterName] = append(pending[:i], pending[i+1:]...)
			break
		}
	}
	if len(sc.pendingRoles[clusterName]) == 0 {
		delete(sc.pendingRoles, clusterName)
	}
}

func (sc *SecurityCoordinator) removePendingGrant(clusterName string, req types.NamespacedName) {
	pending := sc.pendingGrants[clusterName]
	for i, r := range pending {
		if r == req {
			sc.pendingGrants[clusterName] = append(pending[:i], pending[i+1:]...)
			break
		}
	}
	if len(sc.pendingGrants[clusterName]) == 0 {
		delete(sc.pendingGrants, clusterName)
	}
}

func (sc *SecurityCoordinator) removePendingUser(clusterName string, req types.NamespacedName) {
	pending := sc.pendingUsers[clusterName]
	for i, r := range pending {
		if r == req {
			sc.pendingUsers[clusterName] = append(pending[:i], pending[i+1:]...)
			break
		}
	}
	if len(sc.pendingUsers[clusterName]) == 0 {
		delete(sc.pendingUsers, clusterName)
	}
}

// GetClusterNameFromRole extracts cluster name from role spec
func (sc *SecurityCoordinator) GetClusterNameFromRole(ctx context.Context, req types.NamespacedName) (string, error) {
	role := &neo4jv1alpha1.Neo4jRole{}
	if err := sc.Get(ctx, req, role); err != nil {
		return "", err
	}
	return role.Spec.ClusterRef, nil
}

// GetClusterNameFromGrant extracts cluster name from grant spec
func (sc *SecurityCoordinator) GetClusterNameFromGrant(ctx context.Context, req types.NamespacedName) (string, error) {
	grant := &neo4jv1alpha1.Neo4jGrant{}
	if err := sc.Get(ctx, req, grant); err != nil {
		return "", err
	}
	return grant.Spec.ClusterRef, nil
}

// GetClusterNameFromUser extracts cluster name from user spec
func (sc *SecurityCoordinator) GetClusterNameFromUser(ctx context.Context, req types.NamespacedName) (string, error) {
	user := &neo4jv1alpha1.Neo4jUser{}
	if err := sc.Get(ctx, req, user); err != nil {
		return "", err
	}
	return user.Spec.ClusterRef, nil
}
