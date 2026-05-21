# Contributing

Thanks for considering a contribution! The most common kind is **adding a new database adapter**, and the project is designed so that's a small, isolated PR.

## Adding a new database adapter

Say you want to add DuckDB. The complete diff is:

1. **Create `internal/adapters/duckdb/duckdb.go`.** Copy `internal/adapters/sqlite/sqlite.go` as a starting point (it's the closest in structure).

2. **Implement the `adapters.Adapter` interface.** The contract is in `internal/adapters/adapter.go`:

   ```go
   type Adapter interface {
       Kind() Kind
       EnvPrefix() string                                          // e.g. "DUCKDB_"
       Discover(env map[string]string) ([]Source, error)
       Connect(ctx context.Context, src Source) (Conn, error)
       CloseAll() error
   }
   ```

   `Conn` is the per-source live connection. Methods you must implement:

   - `ListSchemas`, `ListTables`, `ListColumns`
   - `TableStats`, `SampleRows`
   - `FindRelationships` (return `adapters.ErrNotSupported` if your DB has none)
   - `ExecuteQuery` (call `adapters.AssertReadOnly` first!)
   - `Close` (usually a no-op; the Adapter owns the pool)

3. **Register in `init()`.** Last lines of the file:

   ```go
   func init() {
       adapters.Register(New())
   }
   ```

4. **Add a blank import in `cmd/server/main.go`:**

   ```go
   _ "github.com/open-db-mcp/open-db-mcp/internal/adapters/duckdb"
   ```

5. **Document the env vars in `.env.example`.** Add a commented block:

   ```env
   # === DuckDB ===
   # DUCKDB_LOCAL_PATH=/data/analytics.duckdb
   ```

6. **(Optional) Add a `Client(name string) *yourDriverType` method on your Adapter** if you also want to ship driver-specific tools (e.g. `duckdb_export`). The mongo and redis adapters do this — see how `internal/tools/mongo_tools.go` reaches `*mongo.Client` through the registry.

7. **Open a PR.** That's it.

## Code style

- One adapter = one file = one package. Do not split a single database across multiple files unless it gets above ~500 lines.
- Use `log/slog` for logging (stdlib, structured).
- No global state outside `init()`. Pool maps live inside the `Adapter` struct.
- Mutate maps under `sync.Mutex` — Adapters are accessed concurrently from tool handlers.
- Read-only is non-negotiable for `ExecuteQuery`. Always call `adapters.AssertReadOnly(q.SQL)` first.

## Tools that span multiple databases

If your DB is SQL-like, the generic `db_*` tools work for free via the `Adapter` interface. No additional code.

If you want a DB-specific tool (e.g. `mongo_aggregate`), put it in `internal/tools/<db>_tools.go` and call your adapter's typed `Client()` method.

## Local development

```bash
cp .env.example .env
docker compose -f docker-compose.dev.yml up -d   # spin up test databases
# fill in .env with PG_DEV_HOST=host.docker.internal, etc.
make build
./bin/open-db-mcp
```

To run from a checkout without Docker, set the env in your shell, then `make run`.

## Reporting bugs / asking for new databases

Open an issue with:

- which database family
- a link to the official Go driver
- one example URI / DSN
- whether read-only or read-write semantics are expected
