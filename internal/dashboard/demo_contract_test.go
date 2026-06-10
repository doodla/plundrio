package dashboard

import (
	"net/http"
	"testing"
	"time"

	"github.com/doodla/plundrio/internal/config"
	"github.com/doodla/plundrio/internal/contractprobe"
	"github.com/doodla/plundrio/internal/demo"
	"github.com/doodla/plundrio/internal/download"
)

// TestContractAgainstDemoServer is the contract-conformance gate: it boots the
// REAL dashboard server in demo mode on an ephemeral port — the same wiring
// main.go uses (demo put.io client → download.Manager → Dashboard) — and runs
// the frozen endpoint prober against it. ProbeContract asserts every REST + SSE
// shape and, critically, that the OAuth token sentinel appears in NO payload.
//
// The token leak needle: PLDR_TOKEN is seeded with contractprobe.DemoTokenSentinel
// and cfg.OAuthToken carries it too, so the daemon HAS a token to potentially
// leak. The demo client never serves it and the settings API reports token.value
// as null, so finding the sentinel anywhere is a real contract violation.
func TestContractAgainstDemoServer(t *testing.T) {
	clearPLDREnv(t)
	// Seed the sentinel as the (env-pinned) token so the security scan has a
	// concrete needle and the token entry reports locked + is_set:true.
	t.Setenv("PLDR_TOKEN", contractprobe.DemoTokenSentinel)

	targetDir := t.TempDir()
	const demoFolderID int64 = 99_000

	demoClient := demo.NewClient(demoFolderID)
	defer demoClient.Stop()

	cfg := &config.Config{
		TargetDir:     targetDir,
		PutioFolder:   "demo",
		FolderID:      demoFolderID,
		OAuthToken:    contractprobe.DemoTokenSentinel,
		ListenAddr:    ":9091",
		DashboardAddr: "127.0.0.1:0", // ephemeral
		WorkerCount:   2,
	}

	mgr := download.New(cfg, demoClient)
	mgr.Start()
	defer mgr.Stop()

	// overridesPath in targetDir so the settings GET's LoadOverrides is a no-op
	// (file absent) rather than reaching for a real path.
	dash := New(cfg, demoClient, mgr, targetDir+"/.plundrio-overrides.json")
	go func() { _ = dash.Start() }()
	defer func() { _ = dash.Stop() }()

	base := waitForAddr(t, dash)

	// Drive the demo virtual clock forward a touch so some transfers advance
	// past IN_QUEUE; not required for the contract (shapes hold for any state),
	// but gives the prober non-trivial data to validate.
	demoClient.AdvanceForTest(20 * time.Second)

	contractprobe.ProbeContract(t, base)
}

// waitForAddr blocks until the dashboard's listener is bound, then returns the
// http base URL. Fails the test if the bind doesn't happen promptly.
func waitForAddr(t *testing.T, d *Dashboard) string {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if addr := d.Addr(); addr != "" {
			// Confirm the listener actually answers before handing the URL to
			// the prober, so a probe doesn't race the Serve goroutine.
			base := "http://" + addr
			if pingOK(base) {
				return base
			}
		}
		select {
		case <-deadline:
			t.Fatal("dashboard listener did not bind/answer within 3s")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func pingOK(base string) bool {
	ctxClient := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := ctxClient.Get(base + "/api/transfers")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
