// Package mcp implements a minimal Model Context Protocol server.
//
// The protocol is JSON-RPC 2.0 over HTTP. Only three methods matter to us:
//
//	initialize           — handshake; we return our capabilities
//	tools/list           — list every tool the server can run
//	tools/call           — invoke one tool by name with arguments
//
// Plus a couple of notifications we acknowledge silently:
//
//	notifications/initialized
//	ping
//
// Each Tool ships its name, description, JSON-schema input contract, and a
// Handler function. Handlers return plain strings (typically TOON-encoded);
// they are wrapped into MCP `content: [{type:"text", text:...}]` blocks
// before being sent back.
package mcp

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// ToolHandler runs a single tool call. The returned string is sent to the
// client as a text content block.
type ToolHandler func(ctx context.Context, args map[string]any) (string, error)

// Tool is the user-facing description plus handler for a single MCP tool.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Handler     ToolHandler    `json:"-"`
}

// Server holds the registered tools. It is safe for concurrent use.
type Server struct {
	mu    sync.RWMutex
	tools map[string]Tool

	Name    string
	Version string
}

// New returns an empty Server.
func New(name, version string) *Server {
	return &Server{
		tools:   map[string]Tool{},
		Name:    name,
		Version: version,
	}
}

// RegisterTool adds a tool to the server. A duplicate name overrides the
// earlier registration; this lets users replace built-in tools by adding
// their own with the same name.
func (s *Server) RegisterTool(t Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[t.Name] = t
}

// ListTools returns every registered tool, sorted by name for stable output.
// The Handler field is zeroed so the slice can be JSON-encoded directly.
func (s *Server) ListTools() []Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		t.Handler = nil
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Handle dispatches a tools/call to the matching handler.
func (s *Server) Handle(ctx context.Context, name string, args map[string]any) (string, error) {
	s.mu.RLock()
	t, ok := s.tools[name]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	if t.Handler == nil {
		return "", fmt.Errorf("tool %s has no handler", name)
	}
	return t.Handler(ctx, args)
}
