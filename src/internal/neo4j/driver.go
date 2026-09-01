package neo4j

import (
	"context"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type driverAdmin struct {
	driver neo4j.DriverWithContext
}

// ConnectOpts configures operator admin Bolt transport (NEO-004).
type ConnectOpts struct {
	// RootCAs enables neo4j+s with certificate verification.
	RootCAs *x509.CertPool
	// AllowPlaintext permits unencrypted neo4j:// (trust.insecureAdminConnection).
	AllowPlaintext bool
}

// Connect opens a Bolt admin session to uri with basic auth.
// TLS uses neo4j+s with RootCAs — never +ssc. Plaintext requires AllowPlaintext (NEO-004).
func Connect(ctx context.Context, uri, user, password string, opts ConnectOpts) (Admin, error) {
	uri, err := rewriteAdminURI(uri, opts)
	if err != nil {
		return nil, err
	}
	d, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""),
		func(c *neo4j.Config) {
			c.SocketConnectTimeout = 10 * time.Second
			if opts.RootCAs != nil {
				c.RootCAs = opts.RootCAs
			}
		})
	if err != nil {
		return nil, err
	}
	if err := d.VerifyConnectivity(ctx); err != nil {
		_ = d.Close(ctx)
		return nil, fmt.Errorf("bolt connect: %w", err)
	}
	return &driverAdmin{driver: d}, nil
}

func rewriteAdminURI(uri string, opts ConnectOpts) (string, error) {
	switch {
	case opts.RootCAs != nil && opts.AllowPlaintext:
		return "", fmt.Errorf("admin Bolt: RootCAs and AllowPlaintext are mutually exclusive (NEO-004)")
	case opts.RootCAs != nil:
		uri = strings.Replace(uri, "bolt+ssc://", "bolt+s://", 1)
		uri = strings.Replace(uri, "neo4j+ssc://", "neo4j+s://", 1)
		if !strings.Contains(uri, "+s://") {
			uri = strings.Replace(uri, "bolt://", "bolt+s://", 1)
			uri = strings.Replace(uri, "neo4j://", "neo4j+s://", 1)
		}
		return uri, nil
	case opts.AllowPlaintext:
		uri = strings.Replace(uri, "bolt+ssc://", "bolt://", 1)
		uri = strings.Replace(uri, "neo4j+ssc://", "neo4j://", 1)
		uri = strings.Replace(uri, "bolt+s://", "bolt://", 1)
		uri = strings.Replace(uri, "neo4j+s://", "neo4j://", 1)
		return uri, nil
	default:
		return "", fmt.Errorf("admin Bolt requires trust.certificates.bolt (verified TLS) or trust.insecureAdminConnection=true (NEO-004)")
	}
}


func (a *driverAdmin) Close(ctx context.Context) error {
	return a.driver.Close(ctx)
}

func (a *driverAdmin) ShowServers(ctx context.Context) ([]Server, error) {
	result, err := neo4j.ExecuteQuery(ctx, a.driver,
		"SHOW SERVERS YIELD name, address, state, hosting",
		nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase("system"))
	if err != nil {
		return nil, err
	}
	out := make([]Server, 0, len(result.Records))
	for _, rec := range result.Records {
		s := Server{}
		if v, ok := rec.Get("name"); ok {
			s.Name, _ = v.(string)
		}
		if v, ok := rec.Get("address"); ok {
			s.Address, _ = v.(string)
		}
		if v, ok := rec.Get("state"); ok {
			s.State, _ = v.(string)
		}
		if v, ok := rec.Get("hosting"); ok {
			s.Hosting = asStringList(v)
		}
		out = append(out, s)
	}
	return out, nil
}

// asStringList reads a Cypher LIST OF STRING column, which the driver hands over as []any.
func asStringList(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (a *driverAdmin) ShowDatabaseTopologies(ctx context.Context) ([]DatabaseTopology, error) {
	result, err := neo4j.ExecuteQuery(ctx, a.driver,
		"SHOW DATABASES YIELD name, type, requestedPrimariesCount, requestedSecondariesCount, currentPrimariesCount, currentSecondariesCount",
		nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase("system"))
	if err != nil {
		return nil, err
	}
	byName := map[string]DatabaseTopology{}
	for _, rec := range result.Records {
		name, _ := rec.Get("name")
		n, _ := name.(string)
		if n == "" {
			continue
		}
		db := byName[n]
		db.Name = n
		if v, ok := rec.Get("type"); ok {
			db.Type, _ = v.(string)
		}
		rp, rpOK := asInt64(rec, "requestedPrimariesCount")
		rs, rsOK := asInt64(rec, "requestedSecondariesCount")
		cp, cpOK := asInt64(rec, "currentPrimariesCount")
		cs, csOK := asInt64(rec, "currentSecondariesCount")
		// system often has NULL requested* counts — still record current topology.
		if rpOK && rsOK {
			db.HasTopology = true
			db.RequestedPrimaries = rp
			db.RequestedSecondaries = rs
		}
		if cpOK {
			db.HasTopology = true
			if cp > db.CurrentPrimaries {
				db.CurrentPrimaries = cp
			}
		}
		if csOK {
			db.HasTopology = true
			if cs > db.CurrentSecondaries {
				db.CurrentSecondaries = cs
			}
		}
		byName[n] = db
	}
	out := make([]DatabaseTopology, 0, len(byName))
	for _, db := range byName {
		out = append(out, db)
	}
	return out, nil
}

func (a *driverAdmin) SetDatabaseTopology(ctx context.Context, name string, primaries, secondaries int64) error {
	_, err := neo4j.ExecuteQuery(ctx, a.driver,
		"ALTER DATABASE $name SET TOPOLOGY $primaries PRIMARIES $secondaries SECONDARIES",
		map[string]any{"name": name, "primaries": primaries, "secondaries": secondaries},
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase("system"))
	return err
}

// SetDefaultAllocationNumbers writes the DBMS-wide creation defaults. initial.dbms.default_*_count
// only seeds them when the DBMS is initialised, so this procedure is the only way to move them on
// a running cluster.
func (a *driverAdmin) SetDefaultAllocationNumbers(ctx context.Context, primaries, secondaries int64) error {
	_, err := neo4j.ExecuteQuery(ctx, a.driver,
		"CALL dbms.setDefaultAllocationNumbers($primaries, $secondaries)",
		map[string]any{"primaries": primaries, "secondaries": secondaries},
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase("system"))
	return err
}

func asInt64(rec *neo4j.Record, key string) (int64, bool) {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case int:
		return int64(n), true
	default:
		return 0, false
	}
}

func (a *driverAdmin) EnableServer(ctx context.Context, name, modeConstraint string) error {
	params := map[string]any{"name": name}
	q := "ENABLE SERVER $name"
	if modeConstraint != "" {
		// OPTIONS values must be literals — Neo4j rejects parameters inside the map.
		switch modeConstraint {
		case "PRIMARY", "SECONDARY", "NONE":
			q = fmt.Sprintf("ENABLE SERVER $name OPTIONS { modeConstraint: '%s' }", modeConstraint)
		default:
			return fmt.Errorf("invalid modeConstraint %q", modeConstraint)
		}
	}
	_, err := neo4j.ExecuteQuery(ctx, a.driver, q, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase("system"))
	return err
}

func (a *driverAdmin) DeallocateDatabases(ctx context.Context, name string) error {
	_, err := neo4j.ExecuteQuery(ctx, a.driver,
		"DEALLOCATE DATABASES FROM SERVER $name",
		map[string]any{"name": name}, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase("system"))
	return err
}

func (a *driverAdmin) DropServer(ctx context.Context, name string) error {
	_, err := neo4j.ExecuteQuery(ctx, a.driver,
		"DROP SERVER $name",
		map[string]any{"name": name}, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase("system"))
	return err
}
