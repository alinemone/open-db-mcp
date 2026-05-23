// open-db-mcp — Model Context Protocol server over Streamable HTTP that
// exposes any database configured through environment variables.
//
// Adding a new database: see CONTRIBUTING.md.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	"github.com/open-db-mcp/open-db-mcp/internal/auth"
	"github.com/open-db-mcp/open-db-mcp/internal/config"
	"github.com/open-db-mcp/open-db-mcp/internal/mcp"
	"github.com/open-db-mcp/open-db-mcp/internal/search"
	"github.com/open-db-mcp/open-db-mcp/internal/tools"

	// Blank imports — each adapter file calls adapters.Register from its
	// init(). Add or remove a database family by editing this list.
	_ "github.com/open-db-mcp/open-db-mcp/internal/adapters/clickhouse"
	_ "github.com/open-db-mcp/open-db-mcp/internal/adapters/elasticsearch"
	_ "github.com/open-db-mcp/open-db-mcp/internal/adapters/mongodb"
	_ "github.com/open-db-mcp/open-db-mcp/internal/adapters/mysql"
	_ "github.com/open-db-mcp/open-db-mcp/internal/adapters/postgres"
	_ "github.com/open-db-mcp/open-db-mcp/internal/adapters/redis"
	_ "github.com/open-db-mcp/open-db-mcp/internal/adapters/sqlite"
)

const version = "0.1.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}

	// .env is optional — production deployments typically use real env vars.
	_ = godotenv.Load()
	env := config.LoadEnv()
	cfg := config.Load(env)

	setupLogging(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1. Discover every source from env.
	sources, err := adapters.DiscoverAll(env)
	if err != nil {
		slog.ErrorContext(ctx, "discovery failed", "err", err)
		os.Exit(1)
	}
	slog.InfoContext(ctx, "sources discovered", "count", len(sources))
	for _, sr := range sources {
		slog.InfoContext(ctx, "source",
			"kind", sr.Source.Kind, "name", sr.Source.Name, "host", sr.Source.Cfg["host"])
	}

	// 2. Wire the MCP server with tools.
	idx := search.New()
	srv := mcp.New("open-db-mcp", version)
	deps := &tools.Deps{Sources: sources, Search: idx}
	tools.RegisterDB(srv, deps)
	// Adapter-specific tools (mongo_*, redis_*, es_*, clog_*) register
	// themselves through their own Register* functions in the same package.
	tools.RegisterRedis(srv, deps)
	tools.RegisterMongo(srv, deps)
	tools.RegisterES(srv, deps)
	tools.RegisterCLOG(srv, deps, cfg.CLOG)

	// 3. Background-refresh the search index. The first build runs synchronously
	// inside the goroutine; subsequent builds happen every hour.
	go search.RefreshEvery(ctx, idx, sources, time.Hour)

	// 4. HTTP server.
	mw := auth.Middleware(cfg.Principals, "/health", "/version")
	handlerOpts := mcp.HandlerOptions{
		CORSOrigins:   cfg.CORSOrigins,
		VerboseErrors: cfg.LogLevel == "debug",
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", mw(mcp.HTTPHandler(srv, handlerOpts)))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"` + version + `"}`))
	})
	mux.Handle("/sources", mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"count":` + strconv.Itoa(len(sources)) + `}`))
	})))

	addr := fmt.Sprintf(":%d", cfg.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute, // matches the adapter-level query timeout
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		slog.InfoContext(ctx, "open-db-mcp listening",
			"addr", addr,
			"users", len(cfg.Principals),
			"cors_origins", len(cfg.CORSOrigins),
		)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.ErrorContext(ctx, "http server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received, draining connections")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	_ = adapters.CloseAll(shutdownCtx)
}

func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(h))
}
