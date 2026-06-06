// Package dashboard is the daemon's second, read-mostly HTTP face: a static
// Svelte SPA plus a small REST API and an SSE stream, served on its own listener
// (cfg.DashboardListen) separate from the RPC :9091. It reads the same Manager
// and put.io client the RPC server already holds — no second put.io client, no
// duplicate state — tees the existing zerolog stream to SSE subscribers, and
// writes a small JSON overrides file layered on top of viper. The RPC path, the
// OAuth token, and the download engine's invariants are untouched.
//
// Default-off: main.go constructs a Dashboard only when DashboardListen != "".
// A ListenAndServe error is logged and returned (never log.Fatal) so a failed
// dashboard never takes down the load-bearing RPC path.
package dashboard

import (
	"context"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/config"
	"github.com/doodla/plundrio/internal/download"
	"github.com/doodla/plundrio/internal/log"
	"github.com/doodla/plundrio/internal/web"
)

// Service is the narrow view of the download Manager the dashboard reads, in the
// segregated-interface style of server.DownloadService. *download.Manager
// satisfies it; tests pass a fake.
type Service interface {
	GetTransfers() []*putio.Transfer
	GetTransferContext(transferID int64) (*download.TransferContext, bool)
	GetCategory(hash string) string
	// SetWorkerCount / GetWorkerCount back the live `workers` setting.
	SetWorkerCount(n int)
	GetWorkerCount() int
}

// AccountClient is the slice of the put.io client the dashboard needs: just the
// account/quota lookup. Both *api.Client and *demo.Client satisfy it.
type AccountClient interface {
	GetAccountInfo(ctx context.Context) (*putio.AccountInfo, error)
}

// Dashboard owns the dashboard HTTP listener, the SSE hub, and the settings
// state. Construct with New; drive with Start (blocking) / Stop.
type Dashboard struct {
	cfg           *config.Config
	svc           Service
	account       AccountClient
	overridesPath string

	srv *http.Server
	hub *hub

	mu   sync.Mutex
	addr string // bound listener address, set by Start (read via Addr)

	ctx    context.Context
	cancel context.CancelFunc
}

// New constructs a Dashboard. It installs the SSE hub as the log fan-out sink
// (log.SetHubWriter) so log lines flow to subscribers, and wires the HTTP mux.
// It does NOT bind the listener — Start does that.
func New(cfg *config.Config, account AccountClient, svc Service, overridesPath string) *Dashboard {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Dashboard{
		cfg:           cfg,
		svc:           svc,
		account:       account,
		overridesPath: overridesPath,
		ctx:           ctx,
		cancel:        cancel,
	}
	d.hub = newHub(ctx, d)

	// Tee the zerolog stream into the hub. The hub's writer is non-blocking, so
	// a slow SSE subscriber can never back-pressure the logger.
	log.SetHubWriter(d.hub.logWriter())

	mux := http.NewServeMux()
	d.registerRoutes(mux)

	d.srv = &http.Server{
		Addr:    cfg.DashboardListen,
		Handler: mux,
		// This is a NEW bind, so the CLAUDE.md slowloris note applies — set the
		// header timeout here even though :9091 deliberately omits it. We are
		// not changing :9091.
		ReadHeaderTimeout: 10 * time.Second,
	}
	return d
}

// registerRoutes wires the REST + SSE handlers, the /api catch-all, and the
// static SPA handler. ServeMux longest-prefix matching gives the precedence we
// need: the specific /api/* routes win over the /api/ catch-all, which wins over
// "/". So an unknown /api/* path hits the JSON 404 handler (not the SPA), while
// every NON-/api path falls through to the embedded SPA with index.html fallback
// for client-side deep links.
func (d *Dashboard) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/transfers", d.handleTransfers)
	mux.HandleFunc("/api/account", d.handleAccount)
	mux.HandleFunc("/api/settings", d.handleSettings)
	mux.HandleFunc("/api/events", d.hub.handleSSE)
	// Catch-all for unknown API routes: the contract requires a JSON 404, not
	// the SPA HTML the "/" handler would otherwise serve for these paths.
	mux.HandleFunc("/api/", d.handleAPINotFound)
	mux.Handle("/", d.staticHandler())
}

// handleAPINotFound returns the contract JSON 404 for any /api/ path that no
// specific handler claimed.
func (d *Dashboard) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, codeNotFound, "unknown API route: "+r.URL.Path, "")
}

// staticHandler serves the embedded dist/ tree with index.html SPA fallback:
// any path that doesn't resolve to a real embedded file returns index.html so
// client-side deep links work. Vite emits hashed assets under assets/ with
// plain names, so no special handling is needed beyond the fallback.
func (d *Dashboard) staticHandler() http.Handler {
	sub, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		// Unreachable: the embed directive roots at dist/. Panicking here would
		// be honest, but returning a 500 handler keeps a packaging mistake from
		// killing the (already-running) daemon process.
		log.Error("dashboard").Err(err).Msg("embedded dist/ subtree missing")
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "dashboard assets unavailable", http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	indexFallback := newSPAFallback(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only GET/HEAD for static assets.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, "method not allowed", "")
			return
		}
		if indexFallback.shouldFallback(r.URL.Path, sub) {
			indexFallback.serve(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// Start binds the listener on cfg.DashboardListen and serves until Stop. It
// binds explicitly (net.Listen + srv.Serve, not ListenAndServe) so the resolved
// address is observable via Addr — necessary for an ephemeral ":0" bind in
// tests. A clean Stop's http.ErrServerClosed is normalized to nil; main.go logs
// any other error as Error (never Fatal). Blocking.
func (d *Dashboard) Start() error {
	d.hub.start()

	ln, err := net.Listen("tcp", d.srv.Addr)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.addr = ln.Addr().String()
	d.mu.Unlock()

	err = d.srv.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Addr returns the bound listener address once Start has bound it (useful for an
// ephemeral ":0" bind). Empty before Start binds.
func (d *Dashboard) Addr() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.addr
}

// Stop shuts the listener down, cancels the hub context (closing all subscriber
// streams), and detaches the log sink so a torn-down dashboard stops receiving
// log events. Idempotent enough for the single shutdown call site.
func (d *Dashboard) Stop() error {
	// Detach the log sink first so no further log events are routed to a hub
	// that's about to close.
	log.SetHubWriter(nil)
	d.cancel()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return d.srv.Shutdown(shutdownCtx)
}
