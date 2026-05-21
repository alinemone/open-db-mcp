package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	mongoad "github.com/open-db-mcp/open-db-mcp/internal/adapters/mongodb"
	"github.com/open-db-mcp/open-db-mcp/internal/format"
	"github.com/open-db-mcp/open-db-mcp/internal/mcp"
)

// RegisterMongo attaches mongo_* tools. They no-op on non-MongoDB sources.
func RegisterMongo(s *mcp.Server, d *Deps) {
	s.RegisterTool(mcp.Tool{
		Name:        "mongo_list_collections",
		Description: "List collections in a MongoDB database.",
		InputSchema: schemaObj(map[string]any{
			"source":   map[string]any{"type": "string"},
			"database": map[string]any{"type": "string"},
		}, "source", "database"),
		Handler: d.mongoListCollections,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "mongo_find",
		Description: "Run a MongoDB find query and return up to N documents.",
		InputSchema: schemaObj(map[string]any{
			"source":     map[string]any{"type": "string"},
			"database":   map[string]any{"type": "string"},
			"collection": map[string]any{"type": "string"},
			"filter":     map[string]any{"type": "object", "description": "BSON-style filter document"},
			"limit":      map[string]any{"type": "number", "default": 20},
		}, "source", "database", "collection"),
		Handler: d.mongoFind,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "mongo_aggregate",
		Description: "Run a MongoDB aggregation pipeline (array of stages) on a collection.",
		InputSchema: schemaObj(map[string]any{
			"source":     map[string]any{"type": "string"},
			"database":   map[string]any{"type": "string"},
			"collection": map[string]any{"type": "string"},
			"pipeline":   map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"limit":      map[string]any{"type": "number", "default": 50},
		}, "source", "database", "collection", "pipeline"),
		Handler: d.mongoAggregate,
	})
}

func (d *Deps) mongoClient(name string) (*mongo.Client, error) {
	sr, err := d.findSource(name)
	if err != nil {
		return nil, err
	}
	if sr.Source.Kind != adapters.KindMongoDB {
		return nil, fmt.Errorf("source %s is not mongodb (kind=%s)", name, sr.Source.Kind)
	}
	a, ok := sr.Adapter.(*mongoad.Adapter)
	if !ok {
		return nil, fmt.Errorf("internal: source %s is not registered as a mongodb adapter", name)
	}
	cli := a.Client(sr.Source.Name)
	if cli == nil {
		if _, err := sr.Adapter.Connect(context.Background(), sr.Source); err != nil {
			return nil, err
		}
		cli = a.Client(sr.Source.Name)
	}
	if cli == nil {
		return nil, fmt.Errorf("mongo client unavailable for %s", name)
	}
	return cli, nil
}

func (d *Deps) mongoListCollections(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	db, _ := args["database"].(string)
	cli, err := d.mongoClient(src)
	if err != nil {
		return "", err
	}
	names, err := cli.Database(db).ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return "", err
	}
	rows := make([]map[string]any, len(names))
	for i, n := range names {
		rows[i] = map[string]any{"collection": n}
	}
	return format.ToTOON("Collections", rows), nil
}

func (d *Deps) mongoFind(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	db, _ := args["database"].(string)
	coll, _ := args["collection"].(string)
	filter, _ := args["filter"].(map[string]any)
	if filter == nil {
		filter = map[string]any{}
	}
	limit := int64(20)
	if v, ok := args["limit"].(float64); ok {
		limit = int64(v)
	}
	cli, err := d.mongoClient(src)
	if err != nil {
		return "", err
	}
	bsonFilter, err := toBSON(filter)
	if err != nil {
		return "", fmt.Errorf("invalid filter: %w", err)
	}
	cur, err := cli.Database(db).Collection(coll).Find(ctx, bsonFilter,
		mongoFindOpts(limit))
	if err != nil {
		return "", err
	}
	defer cur.Close(ctx)
	var docs []map[string]any
	for cur.Next(ctx) {
		var m map[string]any
		if err := cur.Decode(&m); err != nil {
			return "", err
		}
		docs = append(docs, m)
	}
	return format.ToTOON("Documents", docs), nil
}

func (d *Deps) mongoAggregate(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	db, _ := args["database"].(string)
	coll, _ := args["collection"].(string)
	pipeline, _ := args["pipeline"].([]any)
	limit := 50
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	cli, err := d.mongoClient(src)
	if err != nil {
		return "", err
	}
	stages := make([]bson.D, 0, len(pipeline)+1)
	for _, raw := range pipeline {
		m, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("pipeline stages must be objects")
		}
		b, err := toBSON(m)
		if err != nil {
			return "", err
		}
		stages = append(stages, b)
	}
	stages = append(stages, bson.D{{Key: "$limit", Value: limit}})
	cur, err := cli.Database(db).Collection(coll).Aggregate(ctx, stages)
	if err != nil {
		return "", err
	}
	defer cur.Close(ctx)
	var docs []map[string]any
	for cur.Next(ctx) {
		var m map[string]any
		if err := cur.Decode(&m); err != nil {
			return "", err
		}
		docs = append(docs, m)
	}
	return format.ToTOON("AggResults", docs), nil
}

func toBSON(m map[string]any) (bson.D, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var d bson.D
	if err := bson.UnmarshalExtJSON(b, true, &d); err != nil {
		return nil, err
	}
	return d, nil
}
