<div dir="rtl">

# open-db-mcp

🌐 **[English](./README.md)** · فارسی

> یک سرور MCP برای همه‌ی دیتابیس‌ها. کافیه یه فایل `.env` رو ویرایش کنی و `docker compose up` بزنی — اون‌وقت Claude / Codex / Gemini / Cursor / Windsurf می‌تونن PostgreSQL، MySQL، ClickHouse، MongoDB، Redis، SQLite و Elasticsearch تو رو بخونن.

<p align="left" dir="ltr">
  <a href="https://github.com/alinemone/open-db-mcp/actions"><img alt="build" src="https://img.shields.io/github/actions/workflow/status/alinemone/open-db-mcp/ci.yml?branch=main"></a>
  <a href="./LICENSE"><img alt="license" src="https://img.shields.io/badge/license-MIT-blue"></a>
  <a href="https://golang.org"><img alt="go" src="https://img.shields.io/badge/go-1.25-00ADD8?logo=go&logoColor=white"></a>
  <img alt="docker" src="https://img.shields.io/badge/docker-ready-2496ED?logo=docker&logoColor=white">
  <img alt="mcp" src="https://img.shields.io/badge/MCP-Streamable_HTTP-5C2D91">
</p>

`open-db-mcp` یک سرور خود-میزبان **[Model Context Protocol (MCP)](https://modelcontextprotocol.io)** نوشته‌شده با **Go** است که هر دیتابیسی رو که بهش دسترسی داری، تبدیل به ابزاری می‌کنه که LLM ها (Claude، Codex، Gemini، Cursor و…) می‌تونن صدا بزنن. هیچ تغییری تو کد لازم نیست — همه‌چیز فقط با ویرایش `.env` انجام می‌شه.

---

## چی به دست میاری

- 🔌 **اضافه کردن دیتابیس فقط با چند خط env.** بدون نوشتن کد، بدون نصب پلاگین.
- 🛡️ **به‌صورت پیش‌فرض فقط-خواندنی** — می‌تونی با خیال راحت به production هم وصلش کنی.
- 🐳 **Docker image کوچیک** (~۱۲ مگابایت، مبتنی بر Alpine). زیر یک ثانیه بالا میاد.
- ⚡ **با همه‌ی کلاینت‌های MCP کار می‌کنه** — Claude Desktop، Claude Code، Codex، Gemini CLI، Cursor، Windsurf، Zed، Continue، Cline.
- 🔑 **توکن مجزا برای هر کاربر همراه نقش** (reader / writer / admin).
- 🐘🐬🟦🍃🟥🔵🔍 **دیتابیس‌های پشتیبانی‌شده:** PostgreSQL · MySQL/MariaDB · ClickHouse · MongoDB · Redis · SQLite · Elasticsearch.

<div dir="ltr">

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

</div>

---

## در ۵ دقیقه بالا بیار

فقط به **Docker** و **Docker Compose** نیاز داری. تمام.

### قدم ۱ — مخزن رو کلون کن

<div dir="ltr">

```bash
git clone https://github.com/alinemone/open-db-mcp.git
cd open-db-mcp
cp .env.example .env
```

</div>

### قدم ۲ — فایل `.env` رو ویرایش کن

فایل `.env` رو با هر ادیتوری باز کن. اول از همه یک خط توکن می‌بینی — **این همون رمزیه که کلاینت AI با اون به سرور وصل می‌شه.** عبارت `changeme` رو با یه چیزی که فقط خودت می‌دونی عوض کن:

<div dir="ltr">

```env
MCP_USER_ADMIN=my-secret-token-123
MCP_USER_ADMIN_ROLE=admin
```

</div>

حالا پایین‌تر برو و **یکی از بلاک‌های دیتابیس رو از حالت کامنت در بیار.** این هم یک مثال کامل برای PostgreSQL — فقط `#` ابتدای خط‌ها رو حذف کن و مقادیر واقعی خودت رو بذار:

<div dir="ltr">

```env
PG_MAIN_HOST=host.docker.internal     # آدرس دیتابیس (پایین توضیح هست)
PG_MAIN_PORT=5432
PG_MAIN_USER=postgres
PG_MAIN_PASS=your-postgres-password
PG_MAIN_DB=your_database_name
```

</div>

> **تو `HOST` چی بنویسم؟**
> - دیتابیس روی **همین لپ‌تاپ** ولی بیرون از Docker است → `host.docker.internal`
> - دیتابیس تو **یه کانتینر Docker دیگه** روی همین ماشینه → اسم اون کانتینر (مثل `postgres`). فقط حواست باشه که دو کانتینر تو یه شبکه باشن.
> - دیتابیس روی **سرور دیگه‌ای** هست → IP یا hostname‌ش (مثل `db.example.com`).

کلمه‌ی `MAIN` تو `PG_MAIN_*` فقط یه اسمه که خودت انتخاب می‌کنی — این سورس بعداً با همون اسم `MAIN` نمایش داده می‌شه. اگه Postgres دوم داری، یه بلاک دیگه با اسم متفاوت بساز (مثلاً `PG_ANALYTICS_*`).

### قدم ۳ — سرور رو بالا بیار

<div dir="ltr">

```bash
docker compose up -d
```

</div>

مطمئن شو که داره کار می‌کنه:

<div dir="ltr">

```bash
curl http://localhost:3000/health
# {"status":"healthy"}
```

</div>

اگه می‌خوای روی پورت دیگه‌ای باشه، تو `.env` بنویس `PORT=3001` و دوباره `docker compose up -d` بزن.

### قدم ۴ — کلاینت AI رو وصل کن

کلاینت خودت رو از پایین انتخاب کن. به‌جای `my-secret-token-123` همون چیزی که تو `MCP_USER_ADMIN` گذاشتی رو بذار.

#### Claude Code (CLI)

<div dir="ltr">

```bash
claude mcp add open-db \
  --transport http \
  --url "http://localhost:3000/mcp?api_key=my-secret-token-123"
```

</div>

با `claude mcp list` چک کن که اضافه شده باشه.

#### Claude Desktop

فایل `claude_desktop_config.json` رو ویرایش کن (macOS: `~/Library/Application Support/Claude/`، Windows: `%APPDATA%\Claude\`، Linux: `~/.config/Claude/`):

<div dir="ltr">

```json
{
  "mcpServers": {
    "open-db": {
      "url": "http://localhost:3000/mcp?api_key=my-secret-token-123"
    }
  }
}
```

</div>

Claude Desktop رو ری‌استارت کن.

#### Cursor

از مسیر *Settings → Cursor Settings → MCP → Add new MCP Server*:

<div dir="ltr">

```json
{
  "open-db": {
    "url": "http://localhost:3000/mcp?api_key=my-secret-token-123"
  }
}
```

</div>

> 📖 **کلاینت‌های دیگه** (Codex، Gemini، Windsurf، Continue، Zed، Cline) رو تو [docs/MCP_CLIENTS.md](./docs/MCP_CLIENTS.md) ببین.

### قدم ۵ — از مدل یه چیزی بپرس

این‌ها رو امتحان کن:

- *«همه‌ی دیتابیس‌هام رو لیست کن.»*
- *«۵ تا بزرگ‌ترین جدول تو دیتابیس MAIN رو نشون بده.»*
- *«جدول `users` چه ستون‌هایی داره؟»*
- *«۳ تا نمونه ردیف از جدول `orders` نشون بده.»*

مدل خودش ابزارهای درست (`db_list_sources` → `db_list_tables` → `db_table_card` → `db_execute_query`) رو پشت سر هم صدا می‌زنه.

---

## اضافه کردن دیتابیس دوم

فقط یه بلاک دیگه اضافه کن. الگوش `<PREFIX>_<NAME>_<KEY>` ـه — `<NAME>` هر اسمی که می‌خوای برای این سورس بذاری.

**دو تا Postgres و یه MySQL:**

<div dir="ltr">

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

</div>

ری‌استارت کن با `docker compose up -d`. حالا `db_list_sources` هر سه‌تا (`MAIN`، `ANALYTICS`، `CRM`) رو نشون می‌ده.

### بقیه‌ی خانواده‌های دیتابیس

<div dir="ltr">

```env
# ClickHouse
CH_OLAP_HOST=10.0.0.4
CH_OLAP_PORT=9000
CH_OLAP_USER=default
CH_OLAP_PASS=
CH_OLAP_DB=default

# MongoDB (با URI)
MONGO_LOGS_URI=mongodb://user:pass@host:27017/dbname

# Redis (با URL)
REDIS_CACHE_URL=redis://:password@host:6379/0

# SQLite (مسیر داخل کانتینر)
SQLITE_LOCAL_PATH=/data/app.db

# Elasticsearch
ES_LOGS_URL=https://elastic.example.com:9200
ES_LOGS_API_KEY=BASE64_ID_AND_KEY
```

</div>

مرجع کامل همه‌ی متغیرها تو [.env.example](./.env.example) هست.

---

## ابزارهای MCP موجود

**عمومی (روی همه‌ی منابع SQL-مانند):**

- `db_list_sources` · `db_list_schemas` · `db_list_tables` · `db_list_columns`
- `db_table_card` · `db_table_card_full` — ستون‌ها + آمار + نمونه‌داده + ایندکس‌ها + FK ها
- `db_find_relationships` — روابط PK/FK
- `db_execute_query` — SQL فقط-خواندنی، خروجی TOON
- `db_execute_write` — SQL نوشتنی (اختیاری، به ازای هر سورس — پایین توضیح هست)
- `search_tables` — جست‌وجوی fuzzy تو جدول/ستون‌های همه‌ی سورس‌ها

**اختصاصی هر دیتابیس:**

- **MongoDB** — `mongo_list_collections`, `mongo_find`, `mongo_aggregate`
- **Redis** — `redis_keys`, `redis_get`, `redis_info`
- **Elasticsearch** — `es_list_sources`, `es_list_indices`, `es_field_caps`, `es_search`
- **CLOG** (اختیاری) — `clog_profile`, `clog_container_logs`

---

## تنظیمات رایجی که شاید بخوای عوض کنی

### عوض کردن پورت

<div dir="ltr">

```env
PORT=3001
```

</div>

بعد `docker compose up -d` بزن. URL ای که کلاینت AI استفاده می‌کنه می‌شه `http://localhost:3001/mcp?api_key=...`.

### اضافه کردن کاربرهای بیشتر (توکن متفاوت)

هر خط `MCP_USER_<NAME>` یه توکن می‌سازه. اگه نقش رو ننویسی، پیش‌فرض `reader` هست:

<div dir="ltr">

```env
MCP_USER_ADMIN=my-secret-token-123
MCP_USER_ADMIN_ROLE=admin

MCP_USER_ALI=ali-token-456          # نقش پیش‌فرض reader → فقط خواندنی
MCP_USER_DEV=dev-token-789
MCP_USER_DEV_ROLE=writer            # روی سورس‌های writable می‌تونه بنویسه
```

</div>

توکن `ALI` رو به کسی بده که فقط باید بخونه؛ توکن `ADMIN` رو برای خودت نگه دار.

### فعال کردن write روی یه سورس خاص (اختیاری)

به‌صورت پیش‌فرض **همه‌ی سورس‌ها فقط-خواندنی هستن.** برای اینکه `db_execute_write` روی یه سورس کار کنه، `_WRITE=true` بذار:

<div dir="ltr">

```env
PG_DEV_HOST=host.docker.internal
PG_DEV_WRITE=true            # ← این سورس writable می‌شه
```

</div>

کاربر صدا‌زننده هم باید نقش `writer` یا `admin` داشته باشه. هر دو شرط باید با هم برقرار باشن — حتی `admin` هم نمی‌تونه روی سورسی که `_WRITE` ندارن چیزی بنویسه. این یک گارد عمدیه: یک کلید قطع‌کننده‌ی سطح deployment.

> 💡 برای production بهتره `WRITE=false` بمونه و تو خود دیتابیس یه user بسازی که فقط `SELECT` grant داره. این بهت دفاع دولایه می‌ده.

---

## بخش پیشرفته

### مدل احراز هویت

هر کاربر یک نقش داره. **خواندن‌ها** برای هر توکن معتبر بدون محدودیت‌ه. **نوشتن** نیاز به *دو* گارد مستقل داره:

1. نقش کاربر `writer` یا `admin` باشه.
2. سورس writable علامت‌گذاری شده باشه (`<PREFIX>_<NAME>_WRITE=true`).

<div dir="ltr">

| Caller role | Source `_WRITE=true` | `db_execute_write` result                |
|-------------|----------------------|------------------------------------------|
| reader      | any                  | `forbidden: user X (role=reader)…`       |
| writer      | true                 | ✅ allowed                                |
| writer      | false                | `source X is read-only; set …WRITE=true` |
| admin       | true                 | ✅ allowed                                |
| admin       | false                | `source X is read-only; …`               |

</div>

هر فراخوانی audit-log می‌شه با فیلدهای `user`، `role`، `source`، `tool`، `duration_ms` و در صورت رد، فیلد `reason`. مقایسه‌ی توکن constant-time هست و توکن خام تو حافظه با sha256 hash می‌شه.

### چطور فقط-خواندنی بودن تضمین می‌شه

سه لایه‌ی مستقل روی هم نشسته‌ان:

1. **گارد سطح Statement** — `db_execute_query` هر دستوری غیر از `SELECT/WITH/EXPLAIN/SHOW/DESCRIBE` رو در زمان parse رد می‌کنه.
2. **read-only در سطح driver** — Postgres تراکنش read-only باز می‌کنه؛ SQLite `query_only` رو فعال می‌کنه؛ MySQL خواندن رو تو `READ ONLY` transaction می‌پیچه؛ ClickHouse با `readonly=2` کار می‌کنه.
3. **RBAC** — نوشتن علاوه بر بقیه نیاز به نقش `writer` یا بالاتر داره (بالا توضیح دادیم).

MongoDB / Redis / Elasticsearch ابزارهای اختصاصی خودشون رو دارن (`mongo_*`، `redis_*`، `es_*`) و از مسیر `db_execute_*` رد نمی‌شن. `mongo_find` و `mongo_aggregate` هم اپراتورهای `$out`, `$merge`, `$function`, `$accumulator`, `$where`, `$eval` رو رد می‌کنن.

### معماری

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

هر فایل `internal/adapters/<dbname>/<dbname>.go` خودکفاست: discovery، connection pool، schema introspection، اجرای query. اضافه کردن خانواده‌ی جدید یعنی یک فایل و یک blank import.

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

- **[docs/MCP_CLIENTS.md](./docs/MCP_CLIENTS.md)** — راهنمای کامل تنظیم برای هر کلاینت محبوب MCP
- **[CONTRIBUTING.md](./CONTRIBUTING.md)** — نحوه‌ی اضافه کردن adapter جدید (یک فایل، حدود ۱۵۰ خط کد)
- **[.env.example](./.env.example)** — مرجع کامل متغیرهای محیطی با توضیحات

می‌خوای **DuckDB / Snowflake / BigQuery / Cassandra / MSSQL** اضافه بشه؟ یه Issue باز کن یا PR بفرست — [CONTRIBUTING.md](./CONTRIBUTING.md) رو ببین.

---

## لایسنس

MIT — [LICENSE](./LICENSE). مشارکت‌ها استقبال می‌شوند.

---

## کلیدواژه‌ها

> MCP server · Model Context Protocol · LLM database tools · Claude MCP · Claude Code · Codex MCP · Gemini CLI MCP · Cursor MCP · Windsurf MCP · Continue MCP · PostgreSQL MCP · MySQL MCP · ClickHouse MCP · MongoDB MCP · Redis MCP · SQLite MCP · Elasticsearch MCP · self-hosted MCP · open source MCP · Docker MCP · AI database access · LLM SQL · agentic SQL · LLM data platform · TOON format

</div>
