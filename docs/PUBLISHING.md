# Publishing & promotion playbook

Practical checklist when pushing `open-db-mcp` to a public GitHub repo. Aim:
make it findable by people Googling "MCP server postgres", "Claude Desktop
database", "self-hosted MCP", etc.

---

## 1. GitHub repo metadata

### About (the description box on the right of the repo page)

Pick one — both are tuned for SEO and 350-char limit on GitHub:

**Option A (short, action-led):**

> One self-hosted MCP server that exposes PostgreSQL, MySQL, ClickHouse, MongoDB, Redis, SQLite, and Elasticsearch to Claude, Codex, Gemini, Cursor, and any MCP client. Add a database via .env, no code change.

**Option B (longer, keyword-dense):**

> Self-hosted Model Context Protocol (MCP) server in Go. Lets Claude / GPT / Gemini / Cursor query PostgreSQL, MySQL, ClickHouse, MongoDB, Redis, SQLite, Elasticsearch through one Docker container. Env-driven discovery, read-only by default, plugin per database.

### Topics (under About → Topics)

Add these — GitHub indexes Topics directly and many MCP discovery sites scrape them:

```
mcp
mcp-server
model-context-protocol
claude
claude-desktop
claude-code
codex
gemini
cursor
windsurf
llm
ai
agentic
postgresql
mysql
clickhouse
mongodb
redis
sqlite
elasticsearch
golang
docker
self-hosted
database
data-platform
sql
read-only
```

### Website link

If you have one, point it at the docs (e.g. a GitHub Pages site for `docs/`).
Otherwise leave it pointing back at the repo.

### Repository settings

- ✅ **Issues**: on
- ✅ **Discussions**: on (creates a friendly Q&A surface)
- ✅ **Sponsors**: optional, but helps signal seriousness
- ❌ **Wiki**: off (everything in docs/ instead — keeps PR review honest)
- ✅ **Preserve this repository** (Zenodo / Software Heritage) — helps academic citations

---

## 2. Files that boost discoverability

The repo already ships:

- `README.md` — keyword-dense, with comparison table
- `LICENSE` — MIT (most permissive, highest adoption ceiling)
- `CONTRIBUTING.md` — onboarding path for new adapters
- `docs/MCP_CLIENTS.md` — per-client setup, indexed by search engines
- `.env.example` — fully commented; people grep their database name and find you

Optional add-ons (high-leverage):

- `SECURITY.md` — short policy (one paragraph + email) → unlocks GitHub's security advisory features
- `CODE_OF_CONDUCT.md` — Contributor Covenant v2.1, paste verbatim
- `.github/ISSUE_TEMPLATE/` — bug-report and feature-request templates → fewer junk issues
- `.github/PULL_REQUEST_TEMPLATE.md` — short PR checklist
- `.github/workflows/ci.yml` — `go test ./...` + `go vet ./...` + `docker build`. Green CI badge converts much better than no badge.
- `CHANGELOG.md` — Keep-a-Changelog format, one entry per release

---

## 3. Tagged releases

GitHub gives big SEO weight to **Releases**. After your first stable cut:

```bash
git tag v0.1.0
git push origin v0.1.0
# Then go to GitHub → Releases → Draft a new release → pick v0.1.0
# Title: "v0.1.0 — initial public release"
# Body:  the CHANGELOG entry for v0.1.0
```

Why it matters:

- Tagged releases appear in the GitHub sidebar
- Show up in `release-please`/`renovate`-style scrapers
- Make it possible for users to pin: `image: ghcr.io/you/open-db-mcp:v0.1.0`

---

## 4. Where to announce (in order of payoff)

| Where                                                  | Effort | Why it works                                  |
|--------------------------------------------------------|--------|-----------------------------------------------|
| **Anthropic Discord** → `#mcp` channel                 | 5 min  | Highest-density audience of MCP users         |
| **r/ClaudeAI** subreddit (link post + 2-line summary)  | 10 min | Active community searching for MCP tools      |
| **r/LocalLLaMA**                                       | 10 min | Self-hosters love single-binary Go tools      |
| **Hacker News** as "Show HN: open-db-mcp"              | 20 min | Time it for ~9am ET on a weekday; one shot only |
| **Twitter/X** with a 1-line pitch and a screenshot     | 5 min  | Tag `@anthropic`, `@modelcontextprotocol`     |
| **LinkedIn** post (if you have an audience there)      | 10 min | B2B reach; pitches the "read-only, prod-safe" angle |
| **MCP server directories**                             | 15 min | See list below                                |
| **dev.to** / **Medium** with a 600-word writeup        | 60 min | Indexed by Google fast; brings long-tail traffic |
| **Lobsters** ("show" tag)                              | 5 min  | Smaller but high signal-to-noise              |

### MCP directories to submit to

- https://github.com/modelcontextprotocol/servers (Anthropic's official list — PR to add yourself)
- https://mcp.so
- https://glama.ai/mcp/servers
- https://www.pulsemcp.com
- https://mcpdirectory.com
- https://www.mcphub.tools

---

## 5. The 1-line pitch (reuse everywhere)

> **open-db-mcp** — one MCP server for every database. Self-hosted, read-only by default. PostgreSQL, MySQL, ClickHouse, MongoDB, Redis, SQLite, Elasticsearch — drop a `.env` and Claude can query all of them.

Variants:

- **Twitter / X (260 chars):**
  > Just shipped open-db-mcp 🚀
  > One self-hosted MCP server that gives Claude / Codex / Gemini / Cursor read-only access to PostgreSQL, MySQL, ClickHouse, MongoDB, Redis, SQLite, Elasticsearch. Add a DB with one .env line, no code change.
  > 🔗 github.com/alinemone/open-db-mcp

- **Reddit title:**
  > Show: open-db-mcp — point Claude at every database you own with one .env file

- **HN title:**
  > Show HN: open-db-mcp – self-hosted MCP server for Postgres/MySQL/ClickHouse/Mongo/Redis/ES

---

## 6. Screenshots that convert

People skim. One image worth shipping:

- A screenshot of Claude Desktop showing the tool list (`db_list_sources`, `db_list_tables`, …) and a sample answer (`db_execute_query` returning a TOON table). Put it near the top of the README, right under the badges.

Capture it on a clean macOS / Windows session, dark theme, no work data.

---

## 7. Long-term SEO

- Write **one blog post every 2–3 weeks** about a use case: "Letting Claude debug your Postgres slow queries", "Using MCP to triage Kubernetes logs through ClickHouse", etc. Each post links back to the repo with a CTA.
- Reply on relevant GitHub issues in Claude/Cursor/Continue repos where someone asks "how do I let Claude talk to my database?". One sentence + a link.
- Maintain CHANGELOG.md religiously — `release-please` and dependency bots prefer projects that look maintained.

---

## 8. Anti-patterns (don't do)

- ❌ Don't beg for stars in the README. It triggers GitHub's bot filters and looks desperate.
- ❌ Don't post the same announcement to 12 subreddits in one hour — gets you banned.
- ❌ Don't claim "fastest" / "best" without benchmarks.
- ❌ Don't strip the MIT license to add a "Commons Clause" or "BUSL". It will hurt adoption.
- ❌ Don't leave placeholders like `your-org` in the docs — replace with your actual GitHub user/org before publishing.

---

## 9. Pre-publish checklist

- [ ] Confirm every README / docs reference uses your real GitHub user/org (currently `alinemone`)
- [ ] Rotate the API tokens that ship in `.env.example` (mark them as obvious dummies)
- [ ] `git log` — confirm no secrets leaked in history (run `gitleaks detect` if unsure)
- [ ] Tag `v0.1.0` and draft the release notes
- [ ] Add the screenshot to the README
- [ ] Submit the PR to `modelcontextprotocol/servers`
- [ ] Cross-post the announcement to Anthropic Discord + r/ClaudeAI in the same hour
