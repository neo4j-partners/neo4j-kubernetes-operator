/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package validation

import (
	"fmt"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
)

// IsClusterShardingReady reports whether cluster.spec is statically configured
// to host property-sharded databases: propertySharding.enabled=true AND Neo4j
// image tag is on the 2025.12+ CalVer line. Returns nil on success or an error
// describing the missing precondition.
//
// This is a static (spec-only) check — it does not consult status conditions
// for runtime readiness. Callers wanting to gate work on the cluster actually
// being live for shards should additionally check cluster.Status.PropertyShardingReady
// or, more specifically, the sharded DB's own Status.ShardingReady.
func IsClusterShardingReady(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	if cluster == nil {
		return fmt.Errorf("cluster is nil")
	}
	if cluster.Spec.PropertySharding == nil || !cluster.Spec.PropertySharding.Enabled {
		return fmt.Errorf("cluster %q does not have property sharding enabled (spec.propertySharding.enabled=true required)", cluster.Name)
	}
	if !resources.IsNeo4jVersion202512OrHigher(cluster.Spec.Image.Tag) {
		return fmt.Errorf("cluster %q image tag %q is below the 2025.12 minimum for property sharding", cluster.Name, cluster.Spec.Image.Tag)
	}
	return nil
}
