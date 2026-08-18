package validation

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/imagepolicy"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/plugins"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
	rendercfg "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/serverconfig"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
	renderwl "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
)

// Neo4jValidator is the validating admission webhook for Neo4j (ADR-001).
// Slice 1: NEO-001 security/hostPath; NEO-002 SA IAM; NEO-005 secrets; NEO-006 config injection.
type Neo4jValidator struct {
	Client client.Client // optional; when set, checks mountable Secret labels (NEO-005)
}

var _ admission.CustomValidator = &Neo4jValidator{}

func (v *Neo4jValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, v.validate(ctx, obj)
}

func (v *Neo4jValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	// Finalizer removal is an UPDATE. Do not re-apply spec checks or a stale/invalid
	// CR cannot finish deleting (NEO-010).
	if neo4j, ok := newObj.(*neo4jv1beta1.Neo4j); ok && neo4j.DeletionTimestamp != nil && !neo4j.DeletionTimestamp.IsZero() {
		return nil, nil
	}
	return nil, v.validate(ctx, newObj)
}

func (v *Neo4jValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *Neo4jValidator) validate(ctx context.Context, obj runtime.Object) error {
	if err := ValidateNeo4j(obj); err != nil {
		return err
	}
	if v.Client == nil {
		return nil
	}
	neo4j, ok := obj.(*neo4jv1beta1.Neo4j)
	if !ok {
		return fmt.Errorf("expected a Neo4j object, got %T", obj)
	}
	return rendersecrets.EnsureMountable(ctx, v.Client, neo4j)
}

// ValidateNeo4j runs CR-only admission checks (no API reads).
func ValidateNeo4j(obj runtime.Object) error {
	neo4j, ok := obj.(*neo4jv1beta1.Neo4j)
	if !ok {
		return fmt.Errorf("expected a Neo4j object, got %T", obj)
	}
	if err := renderwl.ValidateSecurity(neo4j); err != nil {
		return err
	}
	if err := renderwl.ValidateNetworkPolicy(neo4j); err != nil {
		return err
	}
	if err := renderwl.ValidatePDB(neo4j); err != nil {
		return err
	}
	if err := renderstorage.Validate(neo4j); err != nil {
		return err
	}
	if err := rendersecrets.ValidateSpec(neo4j); err != nil {
		return err
	}
	if err := rendercfg.ValidateConfig(neo4j); err != nil {
		return err
	}
	if err := imagepolicy.Validate(neo4j); err != nil {
		return err
	}
	if err := plugins.Validate(neo4j); err != nil {
		return err
	}
	if err := validateScaleCaps(neo4j); err != nil {
		return err
	}
	if err := validateListenPorts(neo4j); err != nil {
		return err
	}
	return nil
}

const (
	maxPrimaryMembers   int32 = 15
	maxSecondaryMembers int32 = 25
)

func validateScaleCaps(n *neo4jv1beta1.Neo4j) error {
	t := n.Spec.Topology
	if t.Primaries != nil && t.Primaries.Members > maxPrimaryMembers {
		return fmt.Errorf("topology.primaries.members %d exceeds maximum %d (NEO-014)", t.Primaries.Members, maxPrimaryMembers)
	}
	if t.DefaultPrimariesCount != nil && *t.DefaultPrimariesCount > maxPrimaryMembers {
		return fmt.Errorf("topology.defaultPrimariesCount %d exceeds maximum %d (NEO-014)", *t.DefaultPrimariesCount, maxPrimaryMembers)
	}
	if t.Secondaries == nil {
		return nil
	}
	if t.Secondaries.Analytics != nil && t.Secondaries.Analytics.Members > maxSecondaryMembers {
		return fmt.Errorf("topology.secondaries.analytics.members %d exceeds maximum %d (NEO-014)", t.Secondaries.Analytics.Members, maxSecondaryMembers)
	}
	if t.Secondaries.Read != nil && t.Secondaries.Read.Members > maxSecondaryMembers {
		return fmt.Errorf("topology.secondaries.read.members %d exceeds maximum %d (NEO-014)", t.Secondaries.Read.Members, maxSecondaryMembers)
	}
	return nil
}

func validateListenPorts(n *neo4jv1beta1.Neo4j) error {
	if n.Spec.Connectivity == nil {
		return nil
	}
	c := n.Spec.Connectivity
	if l := c.Listeners; l != nil {
		for _, err := range []error{
			checkPort("connectivity.listeners.bolt", l.Bolt),
			checkPort("connectivity.listeners.http", l.HTTP),
			checkPort("connectivity.listeners.https", l.HTTPS),
			checkPort("connectivity.listeners.backup", l.Backup),
			checkPort("connectivity.listeners.metrics", l.Metrics),
		} {
			if err != nil {
				return err
			}
		}
	}
	if c.Service != nil {
		if err := checkServicePorts("connectivity.service.ports", c.Service.Ports); err != nil {
			return err
		}
	}
	if c.ReverseProxy != nil && c.ReverseProxy.Service != nil {
		if err := checkServicePorts("connectivity.reverseProxy.service.ports", c.ReverseProxy.Service.Ports); err != nil {
			return err
		}
	}
	return nil
}

func checkServicePorts(prefix string, ports *neo4jv1beta1.ServicePortsSpec) error {
	if ports == nil {
		return nil
	}
	for _, err := range []error{
		checkPort(prefix+".bolt", ports.Bolt),
		checkPort(prefix+".http", ports.HTTP),
		checkPort(prefix+".https", ports.HTTPS),
		checkPort(prefix+".backup", ports.Backup),
		checkPort(prefix+".metrics", ports.Metrics),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

func checkPort(field string, p *int32) error {
	if p == nil {
		return nil
	}
	if *p < 1 || *p > 65535 {
		return fmt.Errorf("%s %d is not a valid port (1-65535) (NEO-014)", field, *p)
	}
	return nil
}
