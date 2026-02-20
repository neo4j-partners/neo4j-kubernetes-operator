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
	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

// standaloneAsCluster converts a Neo4jEnterpriseStandalone into a synthetic
// Neo4jEnterpriseCluster for use in backup/restore command generation (image/auth lookup only).
// The returned cluster is NOT reconciled — only use it for field access.
func standaloneAsCluster(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) *neo4jv1alpha1.Neo4jEnterpriseCluster {
	c := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	c.Name = s.Name
	c.Namespace = s.Namespace
	c.Spec.Image = s.Spec.Image
	c.Spec.Auth = s.Spec.Auth
	c.Status.Phase = s.Status.Phase
	c.Spec.Topology.Servers = 1
	return c
}
