# investors-mcp

A read-only [MCP](https://modelcontextprotocol.io) server, written in Go, that lets an LLM query a MongoDB database. It runs over stdio.

## Tools

| Tool | What it does |
|---|---|
| `list_collections` | Collections and their approximate document counts |
| `get_schema` | Infers field paths, types, presence %, and an example value from a random sample |
| `find` | Filter, projection, sort, and paging (`limit` is capped by `MAX_LIMIT`) |
| `count` | Counts the documents that match a filter |
| `distinct` | Distinct values of a field (first 500) |
| `aggregate` | Aggregation pipeline; output is capped at `MAX_LIMIT` docs |
| `list_indexes` | The collection's indexes |

Filters and pipelines are passed as [Extended JSON](https://www.mongodb.com/docs/manual/reference/mongodb-extended-json/) strings, so `{"$oid": "..."}` and `{"$date": "..."}` work.

## Read-only safety

1. **Use a read-only MongoDB user. This is the real guarantee.** The code checks below are only a second line of defense.
   ```js
   db.getSiblingDB("admin").createUser({
     user: "mcp_reader", pwd: "<strong-password>",
     roles: [{ role: "read", db: "<your-db>" }]
   })
   ```
2. The server only calls read APIs (find, count, distinct, aggregate, list).
3. It rejects `$out` and `$merge` (writes) and `$where`, `$function`, and `$accumulator` (server-side JS) at any depth.
4. Every query has a timeout (`QUERY_TIMEOUT`). Results are capped, and responses are truncated at about 100KB.
5. `ALLOWED_COLLECTIONS` limits which collections can be read, including through `$lookup`, `$graphLookup`, and `$unionWith`.

## Configuration

| Env var | Required | Default |
|---|---|---|
| `MONGODB_URI` | yes | – |
| `MONGODB_DB` | yes | – |
| `ALLOWED_COLLECTIONS` | no | all (comma-separated list) |
| `MAX_LIMIT` | no | `50` |
| `QUERY_TIMEOUT` | no | `10s` |

Tip: add `readPreference=secondaryPreferred` to the URI if you have replicas, so LLM queries don't load the primary.

## Build & run

The Makefile loads `MONGODB_URI` and `MONGODB_DB` from `.env.local`. Run `make` to list the targets.

```bash
make db-up      # start local MongoDB
make run        # build and run over stdio
make inspect    # open in the MCP Inspector
make check      # vet, lint, test
make claude-add # register with Claude Code
```

Try it in the MCP Inspector:

```bash
MONGODB_URI="mongodb://..." MONGODB_DB=mydb npx @modelcontextprotocol/inspector ./investors-mcp
```

Add it to Claude Code:

```bash
claude mcp add investors -e MONGODB_URI="mongodb://..." -e MONGODB_DB=mydb -- /absolute/path/to/investors-mcp
```

Add it to Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "investors": {
      "command": "/absolute/path/to/investors-mcp",
      "env": { "MONGODB_URI": "mongodb://...", "MONGODB_DB": "mydb" }
    }
  }
}
```
