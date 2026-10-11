// Package tools registers the read-only MongoDB tools on an MCP server.
package tools

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"investors-mcp/internal/db"
)

// maxResponseBytes caps a single tool response so one query can't flood the LLM's context.
const maxResponseBytes = 100_000

type Config struct {
	DB                 *mongo.Database
	AllowedCollections []string // empty = all collections
	MaxLimit           int64
	Timeout            time.Duration
}

type handler struct{ Config }

// Register adds all tools to the server.
func Register(s *mcp.Server, cfg Config) {
	h := &handler{cfg}
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}

	mcp.AddTool(s, &mcp.Tool{Name: "list_collections", Annotations: ro,
		Description: "List the collections in the database with their approximate document counts. Start here."}, h.listCollections)
	mcp.AddTool(s, &mcp.Tool{Name: "get_schema", Annotations: ro,
		Description: "Infer a collection's schema from a random sample: every field path, its BSON types, how often it appears, and an example value. Call this before writing filters."}, h.getSchema)
	mcp.AddTool(s, &mcp.Tool{Name: "find", Annotations: ro,
		Description: fmt.Sprintf("Query documents with a MongoDB filter. Returns at most %d documents; use skip to page and projection to keep results small.", cfg.MaxLimit)}, h.find)
	mcp.AddTool(s, &mcp.Tool{Name: "count", Annotations: ro,
		Description: "Count documents matching a MongoDB filter."}, h.count)
	mcp.AddTool(s, &mcp.Tool{Name: "distinct", Annotations: ro,
		Description: "List the distinct values of a field, optionally within a filter. Useful for discovering categories (e.g. cities, investor types)."}, h.distinct)
	mcp.AddTool(s, &mcp.Tool{Name: "aggregate", Annotations: ro,
		Description: fmt.Sprintf("Run a read-only aggregation pipeline (grouping, stats, top-N). $out, $merge and server-side JavaScript are rejected. Output is capped at %d documents.", cfg.MaxLimit)}, h.aggregate)
	mcp.AddTool(s, &mcp.Tool{Name: "list_indexes", Annotations: ro,
		Description: "List a collection's indexes. Prefer filtering/sorting on indexed fields for large collections."}, h.listIndexes)
}

// ---- inputs ----

type collectionIn struct {
	Collection string `json:"collection" jsonschema:"collection name"`
}

type schemaIn struct {
	Collection string `json:"collection" jsonschema:"collection name"`
	SampleSize int64  `json:"sample_size,omitempty" jsonschema:"number of random documents to sample (default 50, max 100)"`
}

type findIn struct {
	Collection string `json:"collection" jsonschema:"collection name"`
	Filter     string `json:"filter,omitempty" jsonschema:"MongoDB filter as an Extended JSON object, e.g. {\"city\": \"Mumbai\", \"aum\": {\"$gte\": 1000000}}. Empty matches all."`
	Projection string `json:"projection,omitempty" jsonschema:"fields to include/exclude as a JSON object, e.g. {\"name\": 1, \"email\": 1}"`
	Sort       string `json:"sort,omitempty" jsonschema:"sort spec as a JSON object, e.g. {\"aum\": -1}"`
	Limit      int64  `json:"limit,omitempty" jsonschema:"max documents to return (capped by the server)"`
	Skip       int64  `json:"skip,omitempty" jsonschema:"documents to skip, for paging"`
}

type countIn struct {
	Collection string `json:"collection" jsonschema:"collection name"`
	Filter     string `json:"filter,omitempty" jsonschema:"MongoDB filter as an Extended JSON object. Empty counts all."`
}

type distinctIn struct {
	Collection string `json:"collection" jsonschema:"collection name"`
	Field      string `json:"field" jsonschema:"field path, e.g. \"address.city\""`
	Filter     string `json:"filter,omitempty" jsonschema:"optional MongoDB filter as an Extended JSON object"`
}

type aggregateIn struct {
	Collection string `json:"collection" jsonschema:"collection name"`
	Pipeline   string `json:"pipeline" jsonschema:"aggregation pipeline as an Extended JSON array of stages, e.g. [{\"$group\": {\"_id\": \"$city\", \"n\": {\"$sum\": 1}}}, {\"$sort\": {\"n\": -1}}]"`
}

// ---- handlers ----

func (h *handler) listCollections(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	names, err := h.DB.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		if !h.allowed(n) || strings.HasPrefix(n, "system.") {
			continue
		}
		cnt, err := h.DB.Collection(n).EstimatedDocumentCount(ctx)
		if err != nil {
			fmt.Fprintf(&b, "- %s (count unavailable: %v)\n", n, err)
			continue
		}
		fmt.Fprintf(&b, "- %s (~%d documents)\n", n, cnt)
	}
	if b.Len() == 0 {
		return text("No accessible collections."), nil, nil
	}
	return text(b.String()), nil, nil
}

func (h *handler) getSchema(ctx context.Context, _ *mcp.CallToolRequest, in schemaIn) (*mcp.CallToolResult, any, error) {
	coll, err := h.coll(in.Collection)
	if err != nil {
		return nil, nil, err
	}
	n := in.SampleSize
	if n <= 0 {
		n = 50
	}
	n = min(n, 100)

	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	cur, err := coll.Aggregate(ctx, mongo.Pipeline{{{Key: "$sample", Value: bson.D{{Key: "size", Value: n}}}}})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	fields := map[string]*fieldStats{}
	var sampled int
	for cur.Next(ctx) {
		sampled++
		collectFields(fields, "", cur.Current, 0)
	}
	if err := cur.Err(); err != nil {
		return nil, nil, err
	}
	if sampled == 0 {
		return text("Collection is empty."), nil, nil
	}

	paths := make([]string, 0, len(fields))
	for p := range fields {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	fmt.Fprintf(&b, "Schema of %q inferred from %d sampled documents (field: types, presence, example):\n", in.Collection, sampled)
	for _, p := range paths {
		f := fields[p]
		types := make([]string, 0, len(f.types))
		for t := range f.types {
			types = append(types, t)
		}
		sort.Strings(types)
		fmt.Fprintf(&b, "- %s: %s, %d%%", p, strings.Join(types, "|"), f.count*100/sampled)
		if f.example != "" {
			fmt.Fprintf(&b, ", e.g. %s", f.example)
		}
		b.WriteByte('\n')
	}
	return text(b.String()), nil, nil
}

func (h *handler) find(ctx context.Context, _ *mcp.CallToolRequest, in findIn) (*mcp.CallToolResult, any, error) {
	coll, err := h.coll(in.Collection)
	if err != nil {
		return nil, nil, err
	}
	filter, err := db.ParseDoc(in.Filter)
	if err != nil {
		return nil, nil, fmt.Errorf("filter: %w", err)
	}
	limit := h.MaxLimit
	if in.Limit > 0 {
		limit = min(in.Limit, h.MaxLimit)
	}
	// Fetch one extra document to tell the caller whether more results exist.
	opts := options.Find().SetLimit(limit + 1).SetSkip(max(in.Skip, 0))
	if in.Projection != "" {
		p, err := db.ParseDoc(in.Projection)
		if err != nil {
			return nil, nil, fmt.Errorf("projection: %w", err)
		}
		opts.SetProjection(p)
	}
	if in.Sort != "" {
		s, err := db.ParseDoc(in.Sort)
		if err != nil {
			return nil, nil, fmt.Errorf("sort: %w", err)
		}
		opts.SetSort(s)
	}

	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	cur, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, nil, err
	}
	docs, err := readAll(ctx, cur)
	if err != nil {
		return nil, nil, err
	}
	hasMore := int64(len(docs)) > limit
	if hasMore {
		docs = docs[:limit]
	}
	header := fmt.Sprintf("Returned %d documents (skip=%d).", len(docs), in.Skip)
	if hasMore {
		header += fmt.Sprintf(" More results exist; use skip=%d for the next page, or narrow the filter.", in.Skip+limit)
	}
	return docsResult(header, docs)
}

func (h *handler) count(ctx context.Context, _ *mcp.CallToolRequest, in countIn) (*mcp.CallToolResult, any, error) {
	coll, err := h.coll(in.Collection)
	if err != nil {
		return nil, nil, err
	}
	filter, err := db.ParseDoc(in.Filter)
	if err != nil {
		return nil, nil, fmt.Errorf("filter: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	n, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("%d", n)), nil, nil
}

func (h *handler) distinct(ctx context.Context, _ *mcp.CallToolRequest, in distinctIn) (*mcp.CallToolResult, any, error) {
	coll, err := h.coll(in.Collection)
	if err != nil {
		return nil, nil, err
	}
	if in.Field == "" {
		return nil, nil, fmt.Errorf("field is required")
	}
	filter, err := db.ParseDoc(in.Filter)
	if err != nil {
		return nil, nil, fmt.Errorf("filter: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	res := coll.Distinct(ctx, in.Field, filter)
	var vals bson.A
	if err := res.Decode(&vals); err != nil {
		return nil, nil, err
	}
	const maxDistinct = 500
	header := fmt.Sprintf("%d distinct values for %q.", len(vals), in.Field)
	if len(vals) > maxDistinct {
		header += fmt.Sprintf(" Showing the first %d; use aggregate with $group for counts.", maxDistinct)
		vals = vals[:maxDistinct]
	}
	out, err := bson.MarshalExtJSON(bson.D{{Key: "values", Value: vals}}, false, false)
	if err != nil {
		return nil, nil, err
	}
	return text(header + "\n" + truncate(string(out))), nil, nil
}

func (h *handler) aggregate(ctx context.Context, _ *mcp.CallToolRequest, in aggregateIn) (*mcp.CallToolResult, any, error) {
	coll, err := h.coll(in.Collection)
	if err != nil {
		return nil, nil, err
	}
	pipeline, err := db.ParsePipeline(in.Pipeline)
	if err != nil {
		return nil, nil, err
	}
	// $lookup/$unionWith can read other collections; enforce the allowlist on them too.
	if err := h.checkReferencedCollections(pipeline); err != nil {
		return nil, nil, err
	}
	pipeline = append(pipeline, bson.D{{Key: "$limit", Value: h.MaxLimit}})

	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	cur, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, nil, err
	}
	docs, err := readAll(ctx, cur)
	if err != nil {
		return nil, nil, err
	}
	header := fmt.Sprintf("Pipeline returned %d documents (capped at %d).", len(docs), h.MaxLimit)
	return docsResult(header, docs)
}

func (h *handler) listIndexes(ctx context.Context, _ *mcp.CallToolRequest, in collectionIn) (*mcp.CallToolResult, any, error) {
	coll, err := h.coll(in.Collection)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	cur, err := coll.Indexes().List(ctx)
	if err != nil {
		return nil, nil, err
	}
	docs, err := readAll(ctx, cur)
	if err != nil {
		return nil, nil, err
	}
	return docsResult(fmt.Sprintf("%d indexes.", len(docs)), docs)
}

// ---- helpers ----

func (h *handler) allowed(name string) bool {
	return len(h.AllowedCollections) == 0 || slices.Contains(h.AllowedCollections, name)
}

func (h *handler) coll(name string) (*mongo.Collection, error) {
	if name == "" {
		return nil, fmt.Errorf("collection is required")
	}
	if !h.allowed(name) {
		return nil, fmt.Errorf("collection %q is not accessible; call list_collections", name)
	}
	return h.DB.Collection(name), nil
}

func (h *handler) checkReferencedCollections(v any) error {
	switch t := v.(type) {
	case bson.D:
		for _, e := range t {
			switch e.Key {
			case "$lookup", "$graphLookup", "$unionWith":
				var from string
				switch spec := e.Value.(type) {
				case string: // {$unionWith: "coll"}
					from = spec
				case bson.D:
					for _, f := range spec {
						if f.Key == "from" || f.Key == "coll" {
							from, _ = f.Value.(string)
						}
					}
				}
				if from != "" && !h.allowed(from) {
					return fmt.Errorf("%s references collection %q, which is not accessible", e.Key, from)
				}
			}
			if err := h.checkReferencedCollections(e.Value); err != nil {
				return err
			}
		}
	case bson.A:
		for _, x := range t {
			if err := h.checkReferencedCollections(x); err != nil {
				return err
			}
		}
	}
	return nil
}

func readAll(ctx context.Context, cur *mongo.Cursor) ([]bson.Raw, error) {
	defer func() { _ = cur.Close(ctx) }()
	var docs []bson.Raw
	for cur.Next(ctx) {
		docs = append(docs, slices.Clone(cur.Current))
	}
	return docs, cur.Err()
}

// docsResult renders documents as a relaxed Extended JSON array.
func docsResult(header string, docs []bson.Raw) (*mcp.CallToolResult, any, error) {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n[")
	for i, d := range docs {
		js, err := bson.MarshalExtJSON(d, false, false)
		if err != nil {
			return nil, nil, err
		}
		if i > 0 {
			b.WriteString(",\n")
		}
		b.Write(js)
		if b.Len() > maxResponseBytes {
			fmt.Fprintf(&b, "\n... truncated after %d of %d documents; use a projection or a smaller limit.", i+1, len(docs))
			return text(b.String()), nil, nil
		}
	}
	b.WriteString("]")
	return text(b.String()), nil, nil
}

func truncate(s string) string {
	if len(s) <= maxResponseBytes {
		return s
	}
	return s[:maxResponseBytes] + "\n... truncated; narrow the query."
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

type fieldStats struct {
	types   map[string]bool
	count   int
	example string
}

// collectFields records the type of every field path in doc, descending into embedded documents
// and arrays of documents (array element paths use a "[]" suffix, e.g. "investments[].amount").
func collectFields(fields map[string]*fieldStats, prefix string, doc bson.Raw, depth int) {
	elems, err := doc.Elements()
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, e := range elems {
		path := prefix + e.Key()
		val := e.Value()
		record(fields, path, val, seen)
		if depth >= 4 {
			continue
		}
		switch val.Type {
		case bson.TypeEmbeddedDocument:
			collectFields(fields, path+".", val.Document(), depth+1)
		case bson.TypeArray:
			vals, _ := val.Array().Values()
			for _, v := range vals {
				record(fields, path+"[]", v, seen)
				if v.Type == bson.TypeEmbeddedDocument {
					collectFields(fields, path+"[].", v.Document(), depth+1)
				}
			}
		}
	}
}

func record(fields map[string]*fieldStats, path string, val bson.RawValue, seen map[string]bool) {
	f, ok := fields[path]
	if !ok {
		f = &fieldStats{types: map[string]bool{}}
		fields[path] = f
	}
	f.types[val.Type.String()] = true
	if !seen[path] { // count presence once per document, even for array elements
		seen[path] = true
		f.count++
	}
	if f.example == "" && val.Type != bson.TypeEmbeddedDocument && val.Type != bson.TypeArray && val.Type != bson.TypeNull {
		ex := val.String()
		// Prefer relaxed JSON (50000 rather than {"$numberInt":"50000"}) so examples read naturally.
		if js, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: val}}, false, false); err == nil {
			ex = strings.TrimSuffix(strings.TrimPrefix(string(js), `{"v":`), "}")
		}
		if len(ex) > 80 {
			ex = ex[:80] + "…"
		}
		f.example = ex
	}
}
