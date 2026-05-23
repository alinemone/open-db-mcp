<!-- SEO description -->
<!-- open-db-mcp is a self-hostable Model Context Protocol (MCP) server in Go.
     It lets LLMs (Claude, GPT, Gemini, …) query PostgreSQL, MySQL, ClickHouse,
     MongoDB, Redis, SQLite, and Elasticsearch through a single Docker
     container, configured entirely via .env. -->

# open-db-mcp

🌐 **[فارسی (Persian)](./README.fa.md)** · English

> One MCP server for every database. Drop your `.env`, run `docker compose up`, and any LLM — Claude, Codex, Gemini, Cursor, Windsurf — can read your PostgreSQL, MySQL, ClickHouse, MongoDB, Redis, SQLite, and Elasticsearch.



`open-db-mcp` is a self-hostable **[Model Context Protocol (MCP)](https://modelcontextprotocol.io)** server, written in **Go**, that turns any database you can reach into a tool an LLM can call. Add one `.env` line per database — no code change required — and Claude/Codex/Gemini/Cursor can immediately list schemas, inspect tables, search columns, sample rows, and run read-only SQL across all of them.

---

## Why open-db-mcp?

- 🔌 **Zero-code add a database** — drop env vars with the right prefix (`PG_`, `MYSQL_`, `CH_`, `MONGO_`, `REDIS_`, `SQLITE_`, `ES_`), restart, done.
- 🧩 **Plugin-per-database architecture** — one Go file per database family. Adding DuckDB / Snowflake / Cassandra is a single-file PR.
- 🛡️ **Read-only by default** — `db_execute_query` rejects `INSERT/UPDATE/DELETE/DROP/ALTER` at parse time. Safe to point at production. Writes are gated behind a separate `db_execute_write` tool that only fires for sources you explicitly mark `*_WRITE=true`.
- 🐳 **Single tiny Docker image** — Alpine-based, ~12 MB. Starts in under a second.
- ⚡ **Streamable HTTP** — works out of the box with **Claude Desktop**, **Claude Code**, **Codex**, **Gemini**, **Cursor**, **Windsurf**, **Zed**, **Continue**, **Cline**, and anything else that speaks MCP HTTP.
- 🔑 **Per-user API keys with roles** — `MCP_USER_<NAME>=<token>` + `MCP_USER_<NAME>_ROLE=reader|writer|admin`. Hand out a reader token to analysts, a writer token to a dev environment, an admin token to yourself. Constant-time token comparison; the raw token never leaves env.
- 📦 **TOON-encoded results** — compact token-friendly format that lets the LLM see more rows for the same context budget.
- 🐘🐬🟦🍃🟥🔵🔍 **One server, many databases** — PostgreSQL, MySQL/MariaDB, ClickHouse, MongoDB, Redis, SQLite, Elasticsearch (plus an opt-in CLOG profile for Kubernetes log analysis).

---

## Supported databases

| Family                  | Env prefix | Generic SQL tools | Native tools                                    |
|-------------------------|-----------|-------------------|-------------------------------------------------|
| **PostgreSQL**          | `PG_`     | ✅ full           | FK discovery, indexes                          |
| **MySQL / MariaDB**     | `MYSQL_`  | ✅ full           | FK discovery, indexes                          |
| **ClickHouse**          | `CH_`     | ✅ OLAP-aware     | Table engines, partitions                      |
| **MongoDB**             | `MONGO_`  | listing only      | `mongo_find`, `mongo_aggregate`                |
| **Redis**               | `REDIS_`  | —                 | `redis_keys`, `redis_get`, `redis_info`        |
| **SQLite**              | `SQLITE_` | ✅ full           | embedded; FK PRAGMA                            |
| **Elasticsearch**       | `ES_`     | indices listing   | `es_list_indices`, `es_field_caps`, `es_search`|
| **CLOG (k8s logs)**     | `CLOG_*`  | —                 | namespace/container log search                 |

> Want **DuckDB**, **Snowflake**, **BigQuery**, **Cassandra**, **MSSQL**? Open an issue or send a PR — adding a new adapter is one Go file. See [CONTRIBUTING.md](./CONTRIBUTING.md).

---

## Quick start

```bash
git clone https://github.com/alinemone/open-db-mcp.git
cd open-db-mcp
cp .env.example .env             # fill in PG_*, MYSQL_*, … to point at your DBs
docker compose up -d
curl http://localhost:3000/health
```

Wire it into your MCP client:

```jsonc
// Claude Desktop / Codex / Gemini / Cursor / Windsurf / Continue / Zed
{
  "mcpServers": {
    "open-db": {
      "url": "http://localhost:3000/mcp",
      "headers": {
        "Authorization": "Bearer changeme"
      }
    }
  }
}
```

> Tokens travel in the `Authorization` (or `X-Api-Key`) header by default.
> If your client cannot send custom headers, set `MCP_ALLOW_QUERY_KEY=true`
> in `.env` to enable the legacy `?api_key=` URL form.

Then ask the model: *“list every database I have, then show the 5 biggest tables in each”.* It will chain `db_list_sources` → `db_list_tables` → `db_table_card` for you.

> 📖 Full client setup guide: [docs/MCP_CLIENTS.md](./docs/MCP_CLIENTS.md)

---

## Adding multiple databases of the same kind

Want 3 Postgres clusters, 2 MySQL hosts, and a ClickHouse?

```env
PG_MAIN_HOST=10.0.0.1       PG_ANALYTICS_HOST=10.0.0.2   PG_BILLING_HOST=10.0.0.3
PG_MAIN_USER=postgres       PG_ANALYTICS_USER=postgres   PG_BILLING_USER=postgres
PG_MAIN_PASS=...            PG_ANALYTICS_PASS=...        PG_BILLING_PASS=...
PG_MAIN_DB=app              PG_ANALYTICS_DB=warehouse    PG_BILLING_DB=billing

MYSQL_CRM_HOST=10.0.0.4     MYSQL_LEGACY_HOST=10.0.0.5
CH_OLAP_HOST=10.0.0.6
```

`db_list_sources` will then return all six, and the LLM can address them by name.

---

## Authorization (users and roles)

Every authenticated user gets a role. **Read tools are unrestricted** for any
valid token; **writes require two independent gates** to agree:

1. The caller's role is `writer` or `admin` (set per user in env).
2. The target source is explicitly marked writable (`PG_<NAME>_WRITE=true`,
   etc.).

`admin` does **not** bypass the source-level kill-switch — that flag is a
deployment safety, not a permission.

```env
MCP_USER_ADMIN=tok-admin
MCP_USER_ADMIN_ROLE=admin           # may write on any source flagged WRITE=true

MCP_USER_DEV=tok-dev
MCP_USER_DEV_ROLE=writer            # may write on writable sources

MCP_USER_ALI=tok-ali                # role defaults to reader; read-only

PG_DEV_HOST=...                     # writable: dev/admin can mutate
PG_DEV_WRITE=true

PG_PROD_HOST=...                    # NOT writable: even admin gets read-only error
```

Behavior matrix:

| Caller role | Source `_WRITE=true` | `db_execute_write` result            |
|-------------|----------------------|--------------------------------------|
| reader      | any                  | `forbidden: user X (role=reader)…`   |
| writer      | true                 | ✅ allowed                            |
| writer      | false                | `source X is read-only; set …WRITE=true` |
| admin       | true                 | ✅ allowed                            |
| admin       | false                | `source X is read-only; …`           |

Every call is audit-logged with `user`, `role`, `source`, `tool`, `duration_ms`,
and (on deny) a `reason`. Token comparison is constant-time; raw tokens are
sha256-hashed in memory.

> **Tip:** for defence-in-depth, also create a DB-level user with only
> `SELECT` grants for read-only sources, and a separate user with write grants
> for writable sources. The two layers stack.

---

## Available MCP tools

**Generic (any SQL-like source):**

- `db_list_sources` · `db_list_schemas` · `db_list_tables` · `db_list_columns`
- `db_table_card` · `db_table_card_full` (columns + stats + sample rows + indexes + FKs)
- `db_find_relationships` (PK/FK edges)
- `db_execute_query` (read-only, TOON-encoded results)
- `db_execute_write` (opt-in mutating SQL — requires `<DB>_<NAME>_WRITE=true` on the source; defaults off)
- `search_tables` (fuzzy table/column search across every source)

**Per-database:**

- MongoDB — `mongo_list_collections`, `mongo_find`, `mongo_aggregate`
- Redis — `redis_keys`, `redis_get`, `redis_info`
- Elasticsearch — `es_list_sources`, `es_list_indices`, `es_field_caps`, `es_search`
- CLOG (opt-in) — `clog_profile`, `clog_container_logs`

---

## Write mode (opt-in, per source)

By default **every source is read-only**. Three layers stack to keep it that way:

1. **Statement guard** — `db_execute_query` rejects any non-`SELECT/WITH/EXPLAIN/SHOW/DESCRIBE` statement at parse time.
2. **Driver-level read-only** — Postgres opens a read-only transaction, SQLite carries the `query_only` PRAGMA, MySQL wraps reads in a `READ ONLY` transaction, and ClickHouse enables `readonly=2` for the query.
3. **RBAC** — writes additionally require `role >= writer` (see [Authorization](#authorization-users-and-roles)).

To allow writes on a specific source, set `<PREFIX>_<NAME>_WRITE=true`:

```env
PG_DEV_HOST=host.docker.internal
PG_DEV_WRITE=true            # ← only this source becomes writable

MYSQL_LOCAL_WRITE=true       # same idea for MySQL
CH_PLAYGROUND_WRITE=true     # same for ClickHouse
SQLITE_SCRATCH_WRITE=true    # drops the query_only PRAGMA on this DB
```

The new `db_execute_write` tool refuses to run unless the target source has been marked writable **and** the caller has a writer/admin role:

```
Error: forbidden: user ali (role=reader) cannot write
Error: source PROD is read-only;
       set PG_PROD_WRITE=true in env to enable db_execute_write
```

This is enforced uniformly across **PostgreSQL · MySQL · ClickHouse · SQLite**. MongoDB / Redis / Elasticsearch use their own tool families (`mongo_*`, `redis_*`, `es_*`) and don’t flow through `db_execute_*`. The `mongo_find` and `mongo_aggregate` tools also reject `$out`, `$merge`, `$function`, `$accumulator`, `$where`, and `$eval` operators so they remain genuinely read-only.

> 💡 In production, prefer leaving `WRITE` off and using a DB user with only `SELECT` grants — that gives you defence in depth.

---

## Architecture

```
┌──────────────────────────────────────────────────┐
│ HTTP /mcp  (Streamable HTTP, JSON-RPC 2.0)       │
│ + Auth: Bearer | X-Api-Key | ?api_key=           │
└────────────────────┬─────────────────────────────┘
                     │
              ┌──────▼──────┐
              │ Tool router │ db_*, es_*, clog_*, mongo_*, redis_*
              └──────┬──────┘
                     │  adapters.Adapter interface
   ┌─────────────────┼─────────────────────────┐
   ▼     ▼     ▼     ▼     ▼     ▼     ▼
 [pg] [mysql] [ch] [mongo] [redis] [sqlite] [es]
        (one Go file each, env-discovered)
```

Each `internal/adapters/<dbname>/<dbname>.go` is self-contained: discovery, connection pool, schema introspection, query execution. Adding a new family means adding one file and one blank import.

---

## Comparisons

|                                  | open-db-mcp | manual `psql`-MCP scripts | Single-DB MCP servers (e.g. `postgres-mcp`) |
|----------------------------------|:-----------:|:-------------------------:|:-------------------------------------------:|
| Multiple databases per server    | ✅           | ❌                         | ❌                                           |
| Add a DB without code change     | ✅           | ❌                         | ❌                                           |
| Read-only enforced               | ✅           | varies                    | varies                                      |
| TOON token-efficient output      | ✅           | ❌                         | varies                                      |
| Per-user API keys                | ✅           | ❌                         | ❌                                           |
| Single Docker container          | ✅           | n/a                       | ✅ (one per DB)                              |

---

## Documentation

- **[docs/MCP_CLIENTS.md](./docs/MCP_CLIENTS.md)** — setup recipes for every popular MCP client
- **[CONTRIBUTING.md](./CONTRIBUTING.md)** — how to add a new database adapter (one file, ~150 LOC)
- **[.env.example](./.env.example)** — full env-var reference with comments

---

## License

MIT — see [LICENSE](./LICENSE). Contributions welcome.

---

## Keywords

> MCP server · Model Context Protocol · LLM database tools · Claude MCP · Claude Code · Codex MCP · Gemini CLI MCP · Cursor MCP · Windsurf MCP · Continue MCP · PostgreSQL MCP · MySQL MCP · ClickHouse MCP · MongoDB MCP · Redis MCP · SQLite MCP · Elasticsearch MCP · self-hosted MCP · open source MCP · Docker MCP · AI database access · LLM SQL · agentic SQL · LLM data platform · TOON format
