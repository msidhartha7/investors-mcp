// Command investors-mcp is a read-only MCP server that lets LLMs query a MongoDB database.
package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"investors-mcp/internal/tools"
)

func main() {
	// stdout is the MCP stdio channel; logs must go to stderr.
	log.SetOutput(os.Stderr)

	uri := mustEnv("MONGODB_URI")
	dbName := mustEnv("MONGODB_DB")
	maxLimit := envInt("MAX_LIMIT", 50)
	timeout := envDuration("QUERY_TIMEOUT", 10*time.Second)
	var allowed []string
	for _, c := range strings.Split(os.Getenv("ALLOWED_COLLECTIONS"), ",") {
		if c = strings.TrimSpace(c); c != "" {
			allowed = append(allowed, c)
		}
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetAppName("investors-mcp"))
	if err != nil {
		log.Fatalf("mongo connect: %v", err)
	}
	ctx := context.Background()
	defer func() {
		if err := client.Disconnect(ctx); err != nil {
			log.Printf("mongo disconnect: %v", err)
		}
	}()

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	if err := client.Ping(pingCtx, nil); err != nil {
		log.Fatalf("mongo ping: %v", err)
	}
	cancel()

	server := mcp.NewServer(&mcp.Implementation{Name: "investors-mcp", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: "Read-only access to the investors MongoDB database. " +
			"Workflow: list_collections -> get_schema -> count/find/aggregate. " +
			"Prefer count and aggregate for questions about totals or breakdowns instead of paging through find.",
	})
	tools.Register(server, tools.Config{
		DB:                 client.Database(dbName),
		AllowedCollections: allowed,
		MaxLimit:           int64(maxLimit),
		Timeout:            timeout,
	})

	log.Printf("investors-mcp serving db %q over stdio", dbName)
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("%s is required", k)
	}
	return v
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil && v > 0 {
		return v
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(k)); err == nil && v > 0 {
		return v
	}
	return def
}
