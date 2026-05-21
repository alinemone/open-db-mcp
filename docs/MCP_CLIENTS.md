# Connecting MCP clients to open-db-mcp

`open-db-mcp` speaks the **Streamable HTTP** dialect of MCP at:

```
http://<host>:<port>/mcp
```

Every request must carry an API key — pick whichever style your client prefers:

- **URL query**: `?api_key=<token>` (works with every client, easiest)
- **Header**: `X-Api-Key: <token>`
- **Bearer**: `Authorization: Bearer <token>`

Below assume the server is on `http://localhost:3001` and a token is `my-secret-key`. Replace with your own values.

---

## Claude Desktop

Edit `claude_desktop_config.json`:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "open-db": {
      "url": "http://localhost:3001/mcp?api_key=my-secret-key"
    }
  }
}
```

Restart Claude Desktop. The tools (`db_list_sources`, `db_execute_query`, …) appear in the tool picker.

---

## Claude Code (CLI)

Either edit `~/.claude.json` (look for `mcpServers`) or use the CLI:

```bash
claude mcp add open-db \
  --transport http \
  --url "http://localhost:3001/mcp?api_key=my-secret-key"
```

Verify with `claude mcp list`. The server appears next to any other MCPs you’ve registered.

---

## Codex CLI

Edit `~/.codex/config.toml` (create if missing):

```toml
[mcp_servers.open-db]
url = "http://localhost:3001/mcp?api_key=my-secret-key"
```

Restart `codex`. The tools become available to the model.

---

## Gemini CLI

Edit `~/.gemini/settings.json`:

```json
{
  "mcpServers": {
    "open-db": {
      "httpUrl": "http://localhost:3001/mcp?api_key=my-secret-key"
    }
  }
}
```

Use `gemini` as usual — tools appear in the picker.

---

## Cursor

Open *Settings → Cursor Settings → MCP → Add new MCP Server*, then paste:

```json
{
  "open-db": {
    "url": "http://localhost:3001/mcp?api_key=my-secret-key"
  }
}
```

Or edit `~/.cursor/mcp.json` directly with the same content under `mcpServers`.

---

## VS Code (Continue.dev, Cline, Roo Code, …)

For **Continue**, edit `~/.continue/config.json`:

```json
{
  "experimental": {
    "modelContextProtocolServers": [
      {
        "transport": {
          "type": "http",
          "url": "http://localhost:3001/mcp?api_key=my-secret-key"
        }
      }
    ]
  }
}
```

For **Cline** / **Roo Code**, open the extension settings and add the URL the same way.

---

## Windsurf / Zed

**Windsurf**: edit `~/.codeium/windsurf/mcp_config.json`:

```json
{
  "mcpServers": {
    "open-db": {
      "serverUrl": "http://localhost:3001/mcp?api_key=my-secret-key"
    }
  }
}
```

**Zed**: open the AI panel → *Settings → MCP* → add server URL above.

---

## Multiple users / multiple tokens

If you defined several keys in `.env`:

```env
MCP_USER_ADMIN=admin-token
MCP_USER_ALI=ali-token
MCP_USER_SARA=sara-token
```

Each user simply uses their own token in the URL. The role appears in server logs so you can tell who ran what.

---

## Sanity check from the shell

Once you’ve added the server in your client of choice, you can also verify directly:

```bash
# Health (no auth)
curl http://localhost:3001/health

# tools/list
curl -X POST http://localhost:3001/mcp \
  -H "X-Api-Key: my-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | jq

# tools/call db_list_sources
curl -X POST http://localhost:3001/mcp \
  -H "X-Api-Key: my-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"db_list_sources","arguments":{}}}' | jq
```

If the server lists your sources, you’re good — every client that speaks MCP HTTP will work.

---

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `401 Unauthorized` | Wrong / missing API key. Check `.env` and the URL’s `api_key=` value. |
| `connect ECONNREFUSED` | Container isn’t up. `docker compose ps` to confirm. |
| Tools appear but every call fails | Database hosts unreachable from inside the container. If you use `kubectl port-forward`, the host must be `host.docker.internal`, not `localhost`. |
| `403 Forbidden` from ES (CLOG) | Credentials don’t have read access on that ES cluster. |
| Tool list is empty | No tools registered — confirm the server logs say `"sources discovered" count > 0` at startup. |

Open an issue on the repo if a client isn’t listed here; PRs welcome.
