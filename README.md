<!-- SEO description -->
<!-- open-db-mcp is a self-hostable Model Context Protocol (MCP) server in Go.
     It lets LLMs (Claude, GPT, Gemini, …) query PostgreSQL, MySQL, ClickHouse,
     MongoDB, Redis, SQLite, and Elasticsearch through a single Docker
     container, configured entirely via .env. -->

# open-db-mcp

🌐 **[فارسی (Persian)](./README.fa.md)** · English

> One MCP server for every database. Edit one `.env` file, run `docker compose up`, and Claude / Codex / Gemini / Cursor / Windsurf can read your PostgreSQL, MySQL, ClickHouse, MongoDB, Redis, SQLite, and Elasticsearch.

`open-db-mcp` is a self-hostable **[Model Context Protocol (MCP)](https://modelcontextprotocol.io)** server, written in **Go**, that turns any database you can reach into a tool an LLM can call. No code changes — every database is added by editing `.env`.

---

## What you get

- 🔌 **Add a database with one block of env vars.** No code, no plugin install.
- 🛡️ **Read-only by default** — safe to point at production.
- 🐳 **Single tiny Docker image** (~12 MB, Alpine). Starts in under a second.
- ⚡ **Works with every MCP client** — Claude Desktop, Claude Code, Codex, Gemini CLI, Cursor, Windsurf, Zed, Continue, Cline.
- 🔑 **Per-user API tokens with roles** (reader / writer / admin).
- 🐘🐬🟦🍃🟥🔵🔍 **Supports** PostgreSQL · MySQL/MariaDB · ClickHouse · MongoDB · Redis · SQLite · Elasticsearch.

| Family              | Env prefix | What you can do                                 |
|---------------------|------------|-------------------------------------------------|
| **PostgreSQL**      | `PG_`      | full SQL, schema/index/FK introspection         |
| **MySQL / MariaDB** | `MYSQL_`   | full SQL, schema/index/FK introspection         |
| **ClickHouse**      | `CH_`      | full SQL, OLAP-aware, engines & partitions      |
| **MongoDB**         | `MONGO_`   | `mongo_find`, `mongo_aggregate`, collection list|
| **Redis**           | `REDIS_`   | `redis_keys`, `redis_get`, `redis_info`         |
| **SQLite**          | `SQLITE_`  | full SQL on a file path                         |
| **Elasticsearch**   | `ES_`      | list indices, field caps, raw DSL search        |
| **CLOG (k8s logs)** | `CLOG_*`   | namespace/container log search (opt-in)         |

---

## Get it running in 5 minutes

You need **Docker** and **Docker Compose**. That's it.

### Step 1 — Clone the repo

```bash
git clone https://github.com/alinemone/open-db-mcp.git
cd open-db-mcp
cp .env.example .env
```

### Step 2 — Edit `.env` to point at one database

Open `.env` in any editor. At the top you'll see a token line — **this is the secret your AI client will use to call the server.** Change `changeme` to something only you know:

```env
MCP_USER_ADMIN=my-secret-token-123
MCP_USER_ADMIN_ROLE=admin
```

Now scroll down and **uncomment one database block.** Here's a complete PostgreSQL example — just remove the `#` from each line and fill in your real values:

```env
PG_MAIN_HOST=host.docker.internal     # your DB host (see notes below)
PG_MAIN_PORT=5432
PG_MAIN_USER=postgres
PG_MAIN_PASS=your-postgres-password
PG_MAIN_DB=your_database_name
```

> **What goes in `HOST`?**
> - DB on the **same laptop**, outside Docker → `host.docker.internal`
> - DB in **another Docker container** on the same machine → the container's name (e.g. `postgres`) and make sure the two containers share a network
> - DB on a **remote server** → its IP or hostname (e.g. `db.example.com`)

The word `MAIN` in `PG_MAIN_*` is just a name you pick — you'll see this source listed as `MAIN` later. If you have a second Postgres, add another block with a different name (e.g. `PG_ANALYTICS_*`).

### Step 3 — Start the server

```bash
docker compose up -d
```

Check that it's running:

```bash
curl http://localhost:3000/health
# {"status":"healthy"}
```

If you want it on a different port, set `PORT=3001` in `.env` and run `docker compose up -d` again.

### Step 4 — Connect your AI client

Pick your client below. Replace `my-secret-token-123` with whatever you put in `MCP_USER_ADMIN`.

#### Claude Code (CLI)

```bash
claude mcp add open-db \
  --transport http \
  --url "http://localhost:3000/mcp?api_key=my-secret-token-123"
```

Verify with `claude mcp list`.

#### Claude Desktop

Edit `claude_desktop_config.json` (macOS: `~/Library/Application Support/Claude/`, Windows: `%APPDATA%\Claude\`, Linux: `~/.config/Claude/`):

```json
{
  "mcpServers": {
    "open-db": {
      "url": "http://localhost:3000/mcp?api_key=my-secret-token-123"
    }
  }
}
```

Restart Claude Desktop.

#### Cursor

*Settings → Cursor Settings → MCP → Add new MCP Server*:

```json
{
  "open-db": {
    "url": "http://localhost:3000/mcp?api_key=my-secret-token-123"
  }
}
```

> 📖 **Other clients** (Codex, Gemini, Windsurf, Continue, Zed, Cline): see [docs/MCP_CLIENTS.md](./docs/MCP_CLIENTS.md).

### Step 5 — Ask the model something

Try one of these:

- *“List every database I have.”*
- *“Show me the 5 biggest tables in my MAIN database.”*
- *“What columns does the `users` table have?”*
- *“Show me 3 sample rows from `orders`.”*

The model will chain the right tools (`db_list_sources` → `db_list_tables` → `db_table_card` → `db_execute_query`) on its own.

---

## Adding a second database

Just add another block. The pattern is `<PREFIX>_<NAME>_<KEY>` — `<NAME>` is whatever you want to call this source.

**Two Postgres clusters and a MySQL:**

```env
PG_MAIN_HOST=10.0.0.1
PG_MAIN_USER=postgres
PG_MAIN_PASS=secret1
PG_MAIN_DB=app

PG_ANALYTICS_HOST=10.0.0.2
PG_ANALYTICS_USER=postgres
PG_ANALYTICS_PASS=secret2
PG_ANALYTICS_DB=warehouse

MYSQL_CRM_HOST=10.0.0.3
MYSQL_CRM_USER=root
MYSQL_CRM_PASS=secret3
MYSQL_CRM_DB=crm
```

Restart with `docker compose up -d`. `db_list_sources` will now show `MAIN`, `ANALYTICS`, and `CRM`.

### Other database families

```env
# ClickHouse
CH_OLAP_HOST=10.0.0.4
CH_OLAP_PORT=9000
CH_OLAP_USER=default
CH_OLAP_PASS=
CH_OLAP_DB=default

# MongoDB (URI form)
MONGO_LOGS_URI=mongodb://user:pass@host:27017/dbname

# Redis (URL form)
REDIS_CACHE_URL=redis://:password@host:6379/0

# SQLite (path inside the container)
SQLITE_LOCAL_PATH=/data/app.db

# Elasticsearch
ES_LOGS_URL=https://elastic.example.com:9200
ES_LOGS_API_KEY=BASE64_ID_AND_KEY
```

The full env reference with every option is in [.env.example](./.env.example).

---

## Available MCP tools

**Generic (work on every SQL-like source):**

- `db_list_sources` · `db_list_schemas` · `db_list_tables` · `db_list_columns`
- `db_table_card` · `db_table_card_full` — columns + stats + sample rows + indexes + FKs
- `db_find_relationships` — PK/FK edges
- `db_execute_query` — read-only SQL, TOON-encoded output
- `db_execute_write` — mutating SQL (opt-in per source, see below)
- `search_tables` — fuzzy table/column search across every source

**Per-database:**

- **MongoDB** — `mongo_list_collections`, `mongo_find`, `mongo_aggregate`
- **Redis** — `redis_keys`, `redis_get`, `redis_info`
- **Elasticsearch** — `es_list_sources`, `es_list_indices`, `es_field_caps`, `es_search`
- **CLOG** (opt-in) — `clog_profile`, `clog_container_logs`

---

## Common things you might want to change

### Change the port

```env
PORT=3001
```

Then restart: `docker compose up -d`. The URL your AI client uses becomes `http://localhost:3001/mcp?api_key=...`.

### Add more users (with different tokens)

Each `MCP_USER_<NAME>` line creates a token. The role defaults to `reader` if you don't say otherwise:

```env
MCP_USER_ADMIN=my-secret-token-123
MCP_USER_ADMIN_ROLE=admin

MCP_USER_ALI=ali-token-456          # role is "reader" by default → read-only
MCP_USER_DEV=dev-token-789
MCP_USER_DEV_ROLE=writer            # can run db_execute_write on writable sources
```

Hand the `ALI` token to someone who should only read; keep the `ADMIN` one for yourself.

### Allow writes on a specific source (opt-in)

By default **every source is read-only.** To let `db_execute_write` work on one source, add `_WRITE=true`:

```env
PG_DEV_HOST=host.docker.internal
PG_DEV_WRITE=true            # ← this source becomes writable
```

The caller also needs a `writer` or `admin` role. Both gates must agree — even `admin` cannot write to a source where `_WRITE` isn't `true`. This is a deliberate safety: a deployment-level kill switch.

> 💡 For production, prefer leaving `WRITE=false` and creating a DB user with only `SELECT` grants. That gives you defence in depth.

---

## Advanced

### Authorization model

Every authenticated user has a role. **Reads** are unrestricted for any valid token. **Writes** require *two* independent gates:

1. The caller's role is `writer` or `admin`.
2. The source is marked writable (`<PREFIX>_<NAME>_WRITE=true`).

| Caller role | Source `_WRITE=true` | `db_execute_write` result                |
|-------------|----------------------|------------------------------------------|
| reader      | any                  | `forbidden: user X (role=reader)…`       |
| writer      | true                 | ✅ allowed                                |
| writer      | false                | `source X is read-only; set …WRITE=true` |
| admin       | true                 | ✅ allowed                                |
| admin       | false                | `source X is read-only; …`               |

Every call is audit-logged with `user`, `role`, `source`, `tool`, `duration_ms`, and on deny a `reason`. Token comparison is constant-time and tokens are sha256-hashed in memory.

### Read-only enforcement

Three independent layers keep reads honest:

1. **Statement guard** — `db_execute_query` rejects any non-`SELECT/WITH/EXPLAIN/SHOW/DESCRIBE` at parse time.
2. **Driver-level read-only** — Postgres opens a read-only transaction; SQLite carries `query_only`; MySQL wraps reads in a `READ ONLY` transaction; ClickHouse sets `readonly=2` per query.
3. **RBAC** — writes additionally require `role >= writer` (see above).

MongoDB / Redis / Elasticsearch use their own tool families (`mongo_*`, `redis_*`, `es_*`) and don't flow through `db_execute_*`. `mongo_find` / `mongo_aggregate` reject `$out`, `$merge`, `$function`, `$accumulator`, `$where`, `$eval`.

### Architecture

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

Each `internal/adapters/<dbname>/<dbname>.go` is self-contained: discovery, connection pool, schema introspection, query execution. Adding a new family is one file plus a blank import.

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

- **[docs/MCP_CLIENTS.md](./docs/MCP_CLIENTS.md)** — full setup recipes for every popular MCP client
- **[CONTRIBUTING.md](./CONTRIBUTING.md)** — how to add a new database adapter (one file, ~150 LOC)
- **[.env.example](./.env.example)** — complete env-var reference with comments

Want **DuckDB / Snowflake / BigQuery / Cassandra / MSSQL**? Open an issue or send a PR — see [CONTRIBUTING.md](./CONTRIBUTING.md).

---

## License

MIT — see [LICENSE](./LICENSE). Contributions welcome.

---

## Keywords

> MCP server · Model Context Protocol · LLM database tools · Claude MCP · Claude Code · Codex MCP · Gemini CLI MCP · Cursor MCP · Windsurf MCP · Continue MCP · PostgreSQL MCP · MySQL MCP · ClickHouse MCP · MongoDB MCP · Redis MCP · SQLite MCP · Elasticsearch MCP · self-hosted MCP · open source MCP · Docker MCP · AI database access · LLM SQL · agentic SQL · LLM data platform · TOON format
