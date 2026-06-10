package dashboard

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/config"
	"github.com/doodla/plundrio/internal/download"
)

// fakeService is an in-memory Service for the dashboard unit tests. It records
// SetWorkerCount calls so the live-apply assertions can observe the hook firing.
type fakeService struct {
	mu          sync.Mutex
	transfers   []*putio.Transfer
	contexts    map[int64]*download.TransferContext
	categories  map[string]string
	workerCount int32
}

func newFakeService() *fakeService {
	return &fakeService{
		contexts:    map[int64]*download.TransferContext{},
		categories:  map[string]string{},
		workerCount: 4,
	}
}

func (f *fakeService) GetTransfers() []*putio.Transfer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.transfers
}

func (f *fakeService) GetTransferContext(id int64) (*download.TransferContext, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.contexts[id]
	return c, ok
}

func (f *fakeService) GetCategory(hash string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.categories[hash]
}

func (f *fakeService) SetWorkerCount(n int) { atomic.StoreInt32(&f.workerCount, int32(n)) }
func (f *fakeService) GetWorkerCount() int  { return int(atomic.LoadInt32(&f.workerCount)) }

// fakeAccountClient returns a fixed account, or an error when errOnAccount is
// set (to exercise the upstream-error path).
type fakeAccountClient struct {
	errOnAccount error
}

func (f *fakeAccountClient) GetAccountInfo(ctx context.Context) (*putio.AccountInfo, error) {
	if f.errOnAccount != nil {
		return nil, f.errOnAccount
	}
	ai := &putio.AccountInfo{Username: "tester", DaysUntilFilesDeletion: 3}
	ai.Disk.Size = 1000
	ai.Disk.Used = 620
	ai.Disk.Avail = 380
	return ai, nil
}

// newTestDashboard builds a Dashboard against fakes with the given overrides
// path. It does NOT bind a listener; tests drive the handlers / hub directly or
// via httptest.
func newTestDashboard(svc Service, acc AccountClient, overridesPath string) *Dashboard {
	cfg := &config.Config{
		TargetDir:     "/tmp/plundrio-dash-test",
		PutioFolder:   "plundrio",
		ListenAddr:    ":9091",
		DashboardAddr: ":0",
		WorkerCount:   4,
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &Dashboard{
		cfg:           cfg,
		svc:           svc,
		account:       acc,
		overridesPath: overridesPath,
		ctx:           ctx,
		cancel:        cancel,
	}
	d.hub = newHub(ctx, d)
	return d
}
