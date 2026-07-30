package neo4j

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type driverAdmin struct {
	driver neo4j.DriverWithContext
}

// Connect opens a Bolt admin session to uri with basic auth.
// ponytail: bolt+ssc when useTLS (skip verify) — proper trust material is a follow-up.
func Connect(ctx context.Context, uri, user, password string, useTLS bool) (Admin, error) {
	if useTLS && !strings.Contains(uri, "+s") && !strings.Contains(uri, "+ssc") {
		uri = strings.Replace(uri, "bolt://", "bolt+ssc://", 1)
		uri = strings.Replace(uri, "neo4j://", "neo4j+ssc://", 1)
	}
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""),
		func(c *neo4j.Config) {
			c.SocketConnectTimeout = 10 * time.Second
		})
	if err != nil {
		return nil, err
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, fmt.Errorf("bolt connect: %w", err)
	}
	return &driverAdmin{driver: driver}, nil
}

func (a *driverAdmin) Close(ctx context.Context) error {
	return a.driver.Close(ctx)
}

func (a *driverAdmin) ShowServers(ctx context.Context) ([]Server, error) {
	result, err := neo4j.ExecuteQuery(ctx, a.driver,
		"SHOW SERVERS YIELD name, address, state",
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
		out = append(out, s)
	}
	return out, nil
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
