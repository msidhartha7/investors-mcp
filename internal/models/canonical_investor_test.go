package models

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestDecodeCanonicalInvestor(t *testing.T) {
	raw, err := bson.Marshal(bson.D{
		{Key: "_id", Value: bson.NewObjectID()},
		{Key: "canonicalId", Value: "01-ventures"},
		{Key: "aliases", Value: bson.A{}},
		{Key: "details", Value: nil},
		{Key: "fieldProvenance", Value: bson.A{}},
		{Key: "freshnessScore", Value: int32(0)},
		{Key: "geographyFocus", Value: bson.A{"Europe"}},
		{Key: "industryFocus", Value: bson.A{"Investment Management"}},
		{Key: "investorStageFocus", Value: bson.A{"Seed Round", "Early stage VC"}},
		{Key: "investorType", Value: "Venture Capital"},
		{Key: "lastVerifiedAt", Value: nil},
		{Key: "name", Value: "01 Ventures"},
		{Key: "qualityScore", Value: int32(0)},
		{Key: "recentInvestments", Value: bson.A{}},
		{Key: "reviewState", Value: "needs_review"},
		{Key: "slug", Value: "01-ventures"},
		{Key: "sourceInvestorIds", Value: bson.A{int32(49190)}},
		{Key: "sourceUrls", Value: bson.A{}},
		{Key: "status", Value: "unknown"},
		{Key: "website", Value: nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	var c CanonicalInvestor
	if err := bson.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if c.InvestorType != InvestorTypeVentureCapital || c.InvestorStageFocus[1] != StageEarlyStageVC ||
		c.SourceInvestorIDs[0] != 49190 || c.Details != "" || c.Website != nil || c.IsLegacySnakeCase() {
		t.Errorf("unexpected decode: %+v", c)
	}
}

// TestDecodeAllLiveDocs decodes every document in the real collection.
// It runs only when MONGODB_URI and MONGODB_DB are set, e.g. via `make test` with .env.local.
func TestDecodeAllLiveDocs(t *testing.T) {
	uri, dbName := os.Getenv("MONGODB_URI"), os.Getenv("MONGODB_DB")
	if uri == "" || dbName == "" || testing.Short() {
		t.Skip("MONGODB_URI/MONGODB_DB not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Disconnect(ctx) }()

	cur, err := client.Database(dbName).Collection(CanonicalInvestorsCollection).Find(ctx, bson.D{})
	if err != nil {
		t.Skipf("database unreachable: %v", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var n, legacy int
	for cur.Next(ctx) {
		var c CanonicalInvestor
		if err := cur.Decode(&c); err != nil {
			t.Fatalf("doc %v: %v", cur.Current.Lookup("_id"), err)
		}
		n++
		if c.IsLegacySnakeCase() {
			legacy++
		}
	}
	if err := cur.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("decoded %d docs (%d legacy snake_case)", n, legacy)
}
