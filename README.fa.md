<div dir="rtl">

# open-db-mcp

🌐 **[English](./README.md)** · فارسی

> یک سرور MCP برای همه‌ی دیتابیس‌ها. فقط `.env` رو پر کن و `docker compose up` بزن — هر LLM ای (Claude، Codex، Gemini، Cursor، Windsurf) می‌تونه PostgreSQL، MySQL، ClickHouse، MongoDB، Redis، SQLite و Elasticsearch تو رو بخونه.

<p align="left" dir="ltr">
  <a href="https://github.com/alinemone/open-db-mcp/actions"><img alt="build" src="https://img.shields.io/github/actions/workflow/status/alinemone/open-db-mcp/ci.yml?branch=main"></a>
  <a href="./LICENSE"><img alt="license" src="https://img.shields.io/badge/license-MIT-blue"></a>
  <a href="https://golang.org"><img alt="go" src="https://img.shields.io/badge/go-1.25-00ADD8?logo=go&logoColor=white"></a>
  <img alt="docker" src="https://img.shields.io/badge/docker-ready-2496ED?logo=docker&logoColor=white">
  <img alt="mcp" src="https://img.shields.io/badge/MCP-Streamable_HTTP-5C2D91">
</p>

`open-db-mcp` یک سرور خود-میزبان **[Model Context Protocol (MCP)](https://modelcontextprotocol.io)** نوشته‌شده با **Go** است که هر دیتابیسی رو که بهش دسترسی داری، تبدیل به ابزاری می‌کنه که LLM ها می‌تونن صدا بزنن. فقط یه خط در `.env` اضافه کن — بدون تغییر در کد — و Claude/Codex/Gemini/Cursor می‌تونن بلافاصله schema ها رو ببینن، table ها رو بررسی کنن، column ها رو جستجو کنن، نمونه‌داده بگیرن، و SQL فقط-خواندنی روی همه‌ی منابع اجرا کنن.

---

## چرا open-db-mcp؟

- 🔌 **اضافه کردن دیتابیس بدون کدنویسی** — متغیرهای محیطی با prefix درست (`PG_`, `MYSQL_`, `CH_`, `MONGO_`, `REDIS_`, `SQLITE_`, `ES_`) رو بذار، restart کن، تمام.
- 🧩 **معماری plugin-per-database** — یک فایل Go برای هر خانواده‌ی دیتابیس. اضافه کردن DuckDB / Snowflake / Cassandra یک PR یک‌فایلی است.
- 🛡️ **فقط-خواندنی به‌صورت پیش‌فرض** — `db_execute_query` در زمان parse، دستورات `INSERT/UPDATE/DELETE/DROP/ALTER` رو رد می‌کنه. می‌تونی به production هم وصلش کنی. برای write یک ابزار جداگانه به اسم `db_execute_write` هست که فقط روی منابعی کار می‌کنه که صریحاً `*_WRITE=true` دارن.
- 🐳 **Docker image کوچک** — مبتنی بر Alpine، حدود ۱۲ مگابایت. زیر یک ثانیه بالا میاد.
- ⚡ **Streamable HTTP** — مستقیم با **Claude Desktop**، **Claude Code**، **Codex**، **Gemini**، **Cursor**، **Windsurf**، **Zed**، **Continue**، **Cline** و هر چیزی که MCP HTTP بفهمه کار می‌کنه.
- 🔑 **کلید API به ازای هر کاربر** — به سبک `MCP_USER_<NAME>=<token>`. راحت grep می‌شه، راحت rotate می‌شه، راحت audit می‌شه.
- 📦 **خروجی TOON-encoded** — فرمت فشرده و token-friendly که می‌ذاره LLM ردیف‌های بیشتری در همون context ببینه.
- 🐘🐬🟦🍃🟥🔵🔍 **یک سرور، چندین دیتابیس** — PostgreSQL، MySQL/MariaDB، ClickHouse، MongoDB، Redis، SQLite، Elasticsearch (به‌علاوه پروفایل اختیاری CLOG برای تحلیل لاگ‌های Kubernetes).

---

## دیتابیس‌های پشتیبانی‌شده

<div dir="ltr">

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

</div>

> می‌خوای **DuckDB**, **Snowflake**, **BigQuery**, **Cassandra**, **MSSQL** اضافه بشه؟ Issue باز کن یا PR بفرست — اضافه کردن adapter جدید فقط یک فایل Go است. [CONTRIBUTING.md](./CONTRIBUTING.md) رو ببین.

---

## شروع سریع

<div dir="ltr">

```bash
git clone https://github.com/alinemone/open-db-mcp.git
cd open-db-mcp
cp .env.example .env             # PG_*, MYSQL_*, … رو پر کن
docker compose up -d
curl http://localhost:3000/health
```

</div>

به MCP client ت وصلش کن:

<div dir="ltr">

```jsonc
// Claude Desktop / Codex / Gemini / Cursor / Windsurf / Continue / Zed
{
  "mcpServers": {
    "open-db": {
      "url": "http://localhost:3000/mcp?api_key=changeme"
    }
  }
}
```

</div>

بعد از مدل بپرس: *«همه‌ی دیتابیس‌هام رو لیست کن و ۵ تا بزرگ‌ترین جدول هرکدوم رو نشون بده»*. خودش zincir می‌کنه `db_list_sources` → `db_list_tables` → `db_table_card`.

> 📖 راهنمای کامل کلاینت‌ها: [docs/MCP_CLIENTS.md](./docs/MCP_CLIENTS.md)

---

## اضافه کردن چندین دیتابیس از یک نوع

می‌خوای ۳ تا Postgres، ۲ تا MySQL، و یه ClickHouse داشته باشی؟

<div dir="ltr">

```env
PG_MAIN_HOST=10.0.0.1       PG_ANALYTICS_HOST=10.0.0.2   PG_BILLING_HOST=10.0.0.3
PG_MAIN_USER=postgres       PG_ANALYTICS_USER=postgres   PG_BILLING_USER=postgres
PG_MAIN_PASS=...            PG_ANALYTICS_PASS=...        PG_BILLING_PASS=...
PG_MAIN_DB=app              PG_ANALYTICS_DB=warehouse    PG_BILLING_DB=billing

MYSQL_CRM_HOST=10.0.0.4     MYSQL_LEGACY_HOST=10.0.0.5
CH_OLAP_HOST=10.0.0.6
```

</div>

سپس `db_list_sources` هر شش‌تا رو برمی‌گردونه و LLM می‌تونه با اسم ارجاع بده.

---

## ابزارهای MCP موجود

**عمومی (روی همه‌ی منابع SQL-like کار می‌کنن):**

- `db_list_sources` · `db_list_schemas` · `db_list_tables` · `db_list_columns`
- `db_table_card` · `db_table_card_full` (column ها + آمار + نمونه‌داده + index ها + FK ها)
- `db_find_relationships` (روابط PK/FK)
- `db_execute_query` (فقط-خواندنی، خروجی TOON)
- `db_execute_write` (write اختیاری — فقط روی منابعی که `<DB>_<NAME>_WRITE=true` دارن. پیش‌فرض خاموش)
- `search_tables` (جستجوی fuzzy روی جدول/ستون‌های همه‌ی منابع)

**اختصاصی هر دیتابیس:**

- MongoDB — `mongo_list_collections`, `mongo_find`, `mongo_aggregate`
- Redis — `redis_keys`, `redis_get`, `redis_info`
- Elasticsearch — `es_list_sources`, `es_list_indices`, `es_field_caps`, `es_search`
- CLOG (اختیاری) — `clog_profile`, `clog_container_logs`

---

## Write mode (اختیاری، به ازای هر منبع)

به‌صورت پیش‌فرض **همه‌ی منابع فقط-خواندنی هستن**. ابزار `db_execute_query` هر دستوری غیر از `SELECT/WITH/EXPLAIN/SHOW/DESCRIBE` رو در زمان parse رد می‌کنه، و در driver هایی که پشتیبانی می‌کنن، transaction زیرین هم به‌صورت read-only باز می‌شه (Postgres) یا connection با pragma `query_only` ساخته می‌شه (SQLite).

برای فعال‌سازی write روی یک منبع خاص، `<PREFIX>_<NAME>_WRITE=true` رو ست کن:

<div dir="ltr">

```env
PG_DEV_HOST=host.docker.internal
PG_DEV_WRITE=true            # ← فقط این منبع writable می‌شه

MYSQL_LOCAL_WRITE=true       # برای MySQL
CH_PLAYGROUND_WRITE=true     # برای ClickHouse
SQLITE_SCRATCH_WRITE=true    # برای SQLite (pragma query_only حذف می‌شه)
```

</div>

ابزار جدید `db_execute_write` تا وقتی منبع explicitly writable نشده، اجرا نمی‌شه:

<div dir="ltr">

```
Error: source PROD is read-only;
       set PG_PROD_WRITE=true in env to enable db_execute_write
```

</div>

این رفتار به‌صورت یک‌دست در **PostgreSQL · MySQL · ClickHouse · SQLite** اعمال می‌شه. MongoDB / Redis / Elasticsearch ابزارهای اختصاصی خودشون (`mongo_*`، `redis_*`، `es_*`) رو دارن و از مسیر `db_execute_*` رد نمی‌شن.

> 💡 در production بهتره `WRITE` خاموش بمونه و از DB user ای استفاده کنی که فقط grant `SELECT` داره — این بهت دفاع دولایه می‌ده.

---

## معماری

<div dir="ltr">

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

</div>

هر `internal/adapters/<dbname>/<dbname>.go` خودکفا است: discovery، connection pool، schema introspection، اجرای query. اضافه کردن خانواده‌ی جدید یعنی یک فایل و یک blank import.

---

## مقایسه

<div dir="ltr">

|                                  | open-db-mcp | manual `psql`-MCP scripts | Single-DB MCP servers (e.g. `postgres-mcp`) |
|----------------------------------|:-----------:|:-------------------------:|:-------------------------------------------:|
| Multiple databases per server    | ✅           | ❌                         | ❌                                           |
| Add a DB without code change     | ✅           | ❌                         | ❌                                           |
| Read-only enforced               | ✅           | varies                    | varies                                      |
| TOON token-efficient output      | ✅           | ❌                         | varies                                      |
| Per-user API keys                | ✅           | ❌                         | ❌                                           |
| Single Docker container          | ✅           | n/a                       | ✅ (one per DB)                              |

</div>

---

## مستندات

- **[docs/MCP_CLIENTS.md](./docs/MCP_CLIENTS.md)** — راهنمای ست‌آپ برای هر کلاینت محبوب MCP
- **[CONTRIBUTING.md](./CONTRIBUTING.md)** — نحوه‌ی اضافه کردن adapter جدید (یک فایل، حدود ۱۵۰ خط کد)
- **[.env.example](./.env.example)** — مرجع کامل متغیرهای محیطی با توضیحات

---

## لایسنس

MIT — [LICENSE](./LICENSE). مشارکت‌ها استقبال می‌شوند.

---

## کلیدواژه‌ها

> MCP server · Model Context Protocol · LLM database tools · Claude MCP · Claude Code · Codex MCP · Gemini CLI MCP · Cursor MCP · Windsurf MCP · Continue MCP · PostgreSQL MCP · MySQL MCP · ClickHouse MCP · MongoDB MCP · Redis MCP · SQLite MCP · Elasticsearch MCP · self-hosted MCP · open source MCP · Docker MCP · AI database access · LLM SQL · agentic SQL · LLM data platform · TOON format

</div>
