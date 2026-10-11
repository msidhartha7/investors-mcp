package db

import "testing"

func TestParseDoc(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{``, false},
		{`{"city": "Bangalore", "aum": {"$gte": 1000000}}`, false},
		{`{"_id": {"$oid": "507f1f77bcf86cd799439011"}}`, false},
		{`{"$or": [{"a": 1}, {"b": 2}]}`, false},
		{`{"$where": "this.a > 1"}`, true},
		{`{"$or": [{"a": 1}, {"$where": "sleep(1000)"}]}`, true},
		{`{"$expr": {"$function": {"body": "x", "args": [], "lang": "js"}}}`, true},
		{`not json`, true},
	}
	for _, c := range cases {
		_, err := ParseDoc(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ParseDoc(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
	}
}

func TestParsePipeline(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{`[{"$match": {"city": "Mumbai"}}, {"$group": {"_id": "$type", "n": {"$sum": 1}}}]`, false},
		{`[]`, false},
		{`[{"$out": "stolen"}]`, true},
		{`[{"$match": {}}, {"$merge": {"into": "x"}}]`, true},
		{`[{"$lookup": {"from": "x", "pipeline": [{"$out": "y"}], "as": "z"}}]`, true},
		{`[{"$group": {"_id": null, "x": {"$accumulator": {}}}}]`, true},
		{`{"$match": {}}`, true},
	}
	for _, c := range cases {
		_, err := ParsePipeline(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ParsePipeline(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
	}
}
