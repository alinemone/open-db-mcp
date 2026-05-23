package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/open-db-mcp/open-db-mcp/internal/auth"
)

// rpcRequest mirrors a JSON-RPC 2.0 request frame.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// HandlerOptions controls how HTTPHandler renders responses without changing
// the call signature for every test.
type HandlerOptions struct {
	// CORSOrigins is the allow-list emitted in Access-Control-Allow-Origin.
	// Empty list → no CORS header at all. A single "*" element opens to all.
	CORSOrigins []string

	// VerboseErrors lets handler errors leak through to clients verbatim. Use
	// for local development only.
	VerboseErrors bool
}

// HTTPHandler returns an http.Handler that speaks the MCP Streamable HTTP
// dialect for the methods we care about. GET /mcp returns 405. POST responses
// honor the client's Accept header: clients that include text/event-stream
// (e.g. Claude Code) get a single SSE "message" event; everyone else gets
// plain application/json. One request still = one response either way.
func HTTPHandler(s *Server, opts HandlerOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORS(w, r, opts.CORSOrigins)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
		if err != nil {
			writeRPCError(w, r, nil, -32700, "parse error")
			return
		}

		var req rpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeRPCError(w, r, nil, -32700, "parse error")
			return
		}

		ctx := r.Context()

		switch req.Method {
		case "initialize":
			writeRPCResult(w, r, req.ID, map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    s.Name,
					"version": s.Version,
				},
			})

		case "tools/list":
			writeRPCResult(w, r, req.ID, map[string]any{
				"tools": s.ListTools(),
			})

		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				writeRPCError(w, r, req.ID, -32602, "invalid params")
				return
			}

			// Audit log: who called which tool on which source, with timing.
			// Picks `source` out of args when present (db_*, es_*, mongo_*,
			// redis_* all use this convention).
			started := time.Now()
			source, _ := p.Arguments["source"].(string)
			principal := auth.PrincipalOf(ctx)

			text, err := s.Handle(ctx, p.Name, p.Arguments)
			dur := time.Since(started)

			if err != nil {
				reason := classifyErr(err)
				slog.WarnContext(ctx, "tool.call",
					"user", principal.Name,
					"role", principal.Role.String(),
					"ip", clientIP(r),
					"tool", p.Name,
					"source", source,
					"duration_ms", dur.Milliseconds(),
					"status", "error",
					"reason", reason,
					"err", err.Error(),
				)
				writeRPCResult(w, r, req.ID, map[string]any{
					"content": []map[string]any{{
						"type": "text", "text": "Error: " + clientErrMsg(err, opts.VerboseErrors),
					}},
					"isError": true,
				})
				return
			}

			slog.InfoContext(ctx, "tool.call",
				"user", principal.Name,
				"role", principal.Role.String(),
				"ip", clientIP(r),
				"tool", p.Name,
				"source", source,
				"duration_ms", dur.Milliseconds(),
				"status", "ok",
				"bytes", len(text),
			)
			writeRPCResult(w, r, req.ID, map[string]any{
				"content": []map[string]any{{
					"type": "text", "text": text,
				}},
			})

		case "notifications/initialized", "notifications/cancelled", "ping":
			// No response for notifications; respond with empty result for ping.
			if req.Method == "ping" {
				writeRPCResult(w, r, req.ID, map[string]any{})
			} else {
				w.WriteHeader(http.StatusNoContent)
			}

		default:
			writeRPCError(w, r, req.ID, -32601, "method not found: "+req.Method)
		}
	})
}

// setCORS writes Access-Control-* headers only when CORSOrigins is non-empty.
// Single-element "*" opens to all; otherwise the Origin header is matched
// exactly against the allow-list.
func setCORS(w http.ResponseWriter, r *http.Request, origins []string) {
	if len(origins) == 0 {
		return
	}
	allow := ""
	if len(origins) == 1 && origins[0] == "*" {
		allow = "*"
	} else {
		o := r.Header.Get("Origin")
		for _, a := range origins {
			if a == o {
				allow = o
				break
			}
		}
	}
	if allow == "" {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", allow)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, X-Api-Key, Content-Type, Mcp-Session-Id")
	w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
}

func writeRPCResult(w http.ResponseWriter, r *http.Request, id json.RawMessage, result any) {
	writeRPC(w, r, rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

// clientIP returns the best guess for the originating IP, preferring
// X-Forwarded-For (set by reverse proxies) and falling back to RemoteAddr.
func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		if i := strings.IndexByte(h, ','); i >= 0 {
			return h[:i]
		}
		return h
	}
	if h := r.Header.Get("X-Real-Ip"); h != "" {
		return h
	}
	return r.RemoteAddr
}

func writeRPCError(w http.ResponseWriter, r *http.Request, id json.RawMessage, code int, msg string) {
	// JSON-RPC errors travel as HTTP 200 with an Error field; writeRPC writes
	// 200 implicitly via the chosen encoder.
	writeRPC(w, r, rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	})
}

// writeRPC renders one JSON-RPC response. Clients whose Accept header includes
// text/event-stream get a single SSE "message" event (Claude Code's MCP HTTP
// client requires this); others get plain application/json.
func writeRPC(w http.ResponseWriter, r *http.Request, resp rpcResponse) {
	if wantsSSE(r) {
		data, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// wantsSSE reports whether the client's Accept header opts in to SSE.
func wantsSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

// classifyErr labels an error for the audit log. The classification feeds
// alerting later (e.g. spike of forbidden_role => leaked reader token).
func classifyErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "forbidden"):
		return "forbidden"
	case strings.Contains(msg, "source not found"):
		return "not_found"
	case strings.Contains(msg, "read-only"):
		return "readonly"
	default:
		return "error"
	}
}

// clientErrMsg decides what to send back to the client. User-actionable errors
// (auth, RBAC, "source not found", AssertReadOnly rejections) pass through;
// anything else collapses to a generic message unless VerboseErrors is set.
func clientErrMsg(err error, verbose bool) string {
	if err == nil {
		return ""
	}
	if verbose {
		return err.Error()
	}
	msg := err.Error()
	if userVisible(msg) {
		return msg
	}
	return "internal error"
}

// userVisible reports whether an error message is safe to return verbatim.
func userVisible(msg string) bool {
	prefixes := []string{
		"forbidden",
		"source not found",
		"source ", // e.g. "source X is read-only..."
		"query is required",
		"key is required",
		"invalid filter",
		"invalid params",
		"pipeline stages must be",
		"unknown tool",
		"unsupported type",
		"query must start with",
		"empty query",
		"destructive keyword",
		"empty identifier",
		"identifier too long",
		"invalid character in identifier",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	// Allow wrapped errors that still expose the same prefixes via errors.Is/As
	// fallbacks. (Currently none; placeholder for future sentinel errors.)
	_ = errors.Is
	return false
}
