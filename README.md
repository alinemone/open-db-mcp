# open-db-mcp

> One MCP server, any database, zero config. Add a `*_HOST=…` line to your env, restart, done.

`open-db-mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) server, written in Go, that exposes any database you configure through environment variables. Drop it into Docker Compose with your `.env`, point Claude / Codex / Gemini / Cursor at it, and the LLM can immediately list schemas, inspect tables, sample rows, and run read-only SQL across every source.

It is opinionated about three things:

1. **No code change to add a database.** Discovery is driven by env prefixes (`PG_`, `MYSQL_`, `CH_`, `MONGO_`, `REDIS_`, `SQLITE_`, `ES_`). Add a block to `.env`, restart, the source shows up in `db_list_sources`.
2. **One file per database.** Each adapter is a single Go file under `internal/adapters/<name>/<name>.go`. Adding DuckDB / Cassandra / Snowflake / … is a single-file PR — see [CONTRIBUTING.md](./CONTRIBUTING.md).
3. **Read-only by default.** `db_execute_query` rejects anything that isn't `SELECT` / `WITH` / `EXPLAIN` / `DESCRIBE` / `SHOW`. No `DELETE`-by-accident on production.

## Supported databases

| Family        | Prefix    | Notes                          |
|---------------|-----------|--------------------------------|
| PostgreSQL    | `PG_`     | Full SQL, FKs, indexes         |
| MySQL/MariaDB | `MYSQL_`  | Full SQL, FKs, indexes         |
| ClickHouse    | `CH_`     | OLAP, native protocol          |
| MongoDB       | `MONGO_`  | `mongo_find`, `mongo_aggregate`|
| Redis         | `REDIS_`  | `redis_keys`, `redis_get`, `redis_info` |
| SQLite        | `SQLITE_` | Single-file embedded           |
| Elasticsearch | `ES_`     | `es_search`, `es_field_caps`, plus optional CLOG log-analysis profile |

## Quick start

```bash
git clone https://github.com/your-org/open-db-mcp.git
cd open-db-mcp
cp .env.example .env       # then edit .env to point at your databases
docker compose up -d
curl http://localhost:3000/health
```

Wire it into your MCP client — Claude Desktop, Claude Code, Codex, Gemini, Cursor, Windsurf, Continue, Zed:

> 📖 **Full client setup guide:** [docs/MCP_CLIENTS.md](./docs/MCP_CLIENTS.md)

Quick example for Claude Desktop (`claude_desktop_config.json`):

```jsonc
{
  "mcpServers": {
    "open-db": {
      "url": "http://localhost:3000/mcp?api_key=changeme"
    }
  }
}
```

Then ask the model: *"list every source you can see"* → it will call `db_list_sources` and report back.

## Configuration

The only required env vars are `MCP_API_KEYS` and at least one source block. Everything else has sane defaults. See [`.env.example`](./.env.example) for the full menu.

| Var              | Default       | What it does                          |
|------------------|---------------|---------------------------------------|
| `PORT`           | `3000`        | HTTP listener                         |
| `MCP_USER_<NAME>` | *(required)* | One line per user: `MCP_USER_ALI=token`. Legacy `MCP_API_KEYS=k1:r1,k2:r2` also works. |
| `LOG_LEVEL`      | `info`        | `debug` / `info` / `warn` / `error`   |
| `TZ`             | `UTC`         | Timezone for log timestamps           |
| `CLOG_ES_SOURCE` | *(unset)*     | Set to an `ES_<NAME>` to enable `clog_*` log-analysis tools |

## Available tools

**Generic (work with any SQL-like source):**

- `db_list_sources` — every configured source
- `db_list_schemas`, `db_list_tables`, `db_list_columns`
- `db_table_card` / `db_table_card_full` — stats, sample rows, FKs
- `db_find_relationships` — PK/FK edges (relational sources only)
- `db_execute_query` — read-only SQL, returns TOON-encoded results
- `search_tables` — fuzzy search across every indexed table/column

**Per-database:**

- MongoDB: `mongo_list_collections`, `mongo_find`, `mongo_aggregate`
- Redis: `redis_keys`, `redis_get`, `redis_info`
- Elasticsearch: `es_list_sources`, `es_list_indices`, `es_field_caps`, `es_search`
- CLOG (opt-in): `clog_profile`, `clog_container_logs`

## Architecture

```
┌────────────────────────────────────────────────┐
│ HTTP /mcp  (Streamable HTTP, JSON-RPC 2.0)     │
│ + Auth: Bearer / X-Api-Key / ?api_key=         │
└────────────────────┬───────────────────────────┘
                     │
              ┌──────▼──────┐
              │   Tools     │   ← db_*, es_*, clog_*, mongo_*, redis_*
              └──────┬──────┘
                     │ Adapter interface
   ┌─────────────────┼─────────────────────────┐
   ▼     ▼     ▼     ▼     ▼     ▼     ▼
 [pg] [mysql] [ch] [mongo] [redis] [sqlite] [es]
        (one file each, env-discovered)
```

## Why TOON?

The default response format is [TOON](https://github.com/toon-format/toon) — a tabular text format that uses fewer tokens than JSON. It looks like:

```
Tables[3]{table,kind}:
orders,table
users,table
user_summary,view
```

LLMs read it fine and your context budget thanks you.

## Adding a new database

See [CONTRIBUTING.md](./CONTRIBUTING.md). The short version: one file under `internal/adapters/<dbname>/`, implement `adapters.Adapter`, add a blank import to `cmd/server/main.go`, PR.

## License

MIT — see [LICENSE](./LICENSE).
