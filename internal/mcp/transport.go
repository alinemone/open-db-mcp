package mcp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
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

// HTTPHandler returns an http.Handler that speaks the MCP Streamable HTTP
// dialect for the methods we care about. GET /mcp returns 405 — we don't
// upgrade to SSE; one POST = one JSON-RPC response is enough for tools/list
// and tools/call.
func HTTPHandler(s *Server) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
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
			writeRPCError(w, nil, -32700, "parse error")
			return
		}

		var req rpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeRPCError(w, nil, -32700, "parse error")
			return
		}

		ctx := r.Context()

		switch req.Method {
		case "initialize":
			writeRPCResult(w, req.ID, map[string]any{
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
			writeRPCResult(w, req.ID, map[string]any{
				"tools": s.ListTools(),
			})

		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				writeRPCError(w, req.ID, -32602, "invalid params")
				return
			}
			text, err := s.Handle(ctx, p.Name, p.Arguments)
			if err != nil {
				// Tool errors are returned as a content block with isError=true
				// so the LLM can see them, rather than a JSON-RPC error.
				slog.WarnContext(ctx, "tool error", "tool", p.Name, "err", err)
				writeRPCResult(w, req.ID, map[string]any{
					"content": []map[string]any{{
						"type": "text", "text": "Error: " + err.Error(),
					}},
					"isError": true,
				})
				return
			}
			writeRPCResult(w, req.ID, map[string]any{
				"content": []map[string]any{{
					"type": "text", "text": text,
				}},
			})

		case "notifications/initialized", "notifications/cancelled", "ping":
			// No response for notifications; respond with empty result for ping.
			if req.Method == "ping" {
				writeRPCResult(w, req.ID, map[string]any{})
			} else {
				w.WriteHeader(http.StatusNoContent)
			}

		default:
			writeRPCError(w, req.ID, -32601, "method not found: "+req.Method)
		}
	})
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, X-Api-Key, Content-Type, Mcp-Session-Id")
	w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors travel as 200 with an Error field
	_ = json.NewEncoder(w).Encode(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	})
}
