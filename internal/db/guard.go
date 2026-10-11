// Package db holds helpers that keep queries read-only and parse LLM-supplied JSON.
package db

import (
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// blockedKeys are operators/stages that either write data or run server-side JavaScript.
var blockedKeys = map[string]string{
	"$out":         "writes to a collection",
	"$merge":       "writes to a collection",
	"$where":       "executes server-side JavaScript",
	"$function":    "executes server-side JavaScript",
	"$accumulator": "executes server-side JavaScript",
}

// CheckReadOnly walks a parsed filter/pipeline and rejects any blocked operator, at any depth.
func CheckReadOnly(v any) error {
	switch t := v.(type) {
	case bson.D:
		for _, e := range t {
			if reason, ok := blockedKeys[e.Key]; ok {
				return fmt.Errorf("operator %q is not allowed (%s); this server is read-only", e.Key, reason)
			}
			if err := CheckReadOnly(e.Value); err != nil {
				return err
			}
		}
	case bson.M:
		for k, val := range t {
			if reason, ok := blockedKeys[k]; ok {
				return fmt.Errorf("operator %q is not allowed (%s); this server is read-only", k, reason)
			}
			if err := CheckReadOnly(val); err != nil {
				return err
			}
		}
	case bson.A:
		for _, val := range t {
			if err := CheckReadOnly(val); err != nil {
				return err
			}
		}
	case []any:
		return CheckReadOnly(bson.A(t))
	}
	return nil
}

// ParseDoc parses an Extended JSON object (e.g. a filter) and validates it. Empty input yields an empty doc.
func ParseDoc(s string) (bson.D, error) {
	if s == "" {
		return bson.D{}, nil
	}
	var d bson.D
	if err := bson.UnmarshalExtJSON([]byte(s), false, &d); err != nil {
		return nil, fmt.Errorf("invalid JSON object: %w", err)
	}
	return d, CheckReadOnly(d)
}

// ParsePipeline parses an Extended JSON array of aggregation stages and validates it.
func ParsePipeline(s string) (bson.A, error) {
	var wrapper struct {
		P bson.A `bson:"p"`
	}
	if err := bson.UnmarshalExtJSON([]byte(`{"p":`+s+`}`), false, &wrapper); err != nil {
		return nil, fmt.Errorf("invalid pipeline (expected a JSON array of stages): %w", err)
	}
	return wrapper.P, CheckReadOnly(wrapper.P)
}
