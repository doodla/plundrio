package dashboard

import (
	"context"
	"net/http"
	"time"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/download"
)

// overQuotaThreshold matches server/utils.go: usage at or above 95% is "over
// quota." Kept identical so the dashboard and the RPC server's quota warning
// agree.
const overQuotaThreshold = 95.0

// AccountDTO is the frozen Account object (contract §GET /api/account), shared by
// the REST endpoint and the SSE `account` event.
type AccountDTO struct {
	Username               string  `json:"username"`
	Disk                   DiskDTO `json:"disk"`
	UsedPercent            float64 `json:"used_percent"`
	OverQuota              bool    `json:"over_quota"`
	DaysUntilFilesDeletion int     `json:"days_until_files_deletion"`
}

// DiskDTO is bytes (do NOT carry server.go's /1024/1024 log-line division into
// the API — those put.io fields are int64 bytes).
type DiskDTO struct {
	Size  int64 `json:"size"`
	Used  int64 `json:"used"`
	Avail int64 `json:"avail"`
}

// renderAccount maps a put.io AccountInfo to the contract DTO.
func renderAccount(ai *putio.AccountInfo) AccountDTO {
	var usedPercent float64
	if ai.Disk.Size > 0 {
		usedPercent = float64(ai.Disk.Used) / float64(ai.Disk.Size) * 100
	}
	return AccountDTO{
		Username: ai.Username,
		Disk: DiskDTO{
			Size:  ai.Disk.Size,
			Used:  ai.Disk.Used,
			Avail: ai.Disk.Avail,
		},
		UsedPercent:            usedPercent,
		OverQuota:              usedPercent >= overQuotaThreshold,
		DaysUntilFilesDeletion: ai.DaysUntilFilesDeletion,
	}
}

// renderTransfers builds the full transfer list DTO. Single source for the REST
// endpoint, the SSE `transfers` event, and the snapshot's transfers — so all
// three are byte-identical.
func (d *Dashboard) renderTransfers() []TransferDTO {
	transfers := d.svc.GetTransfers()
	out := make([]TransferDTO, 0, len(transfers))
	for _, t := range transfers {
		var ctx *download.TransferContext
		if c, ok := d.svc.GetTransferContext(t.ID); ok {
			ctx = c
		}
		out = append(out, renderTransfer(t, ctx, d.svc.GetCategory(t.Hash)))
	}
	return out
}

// fetchAccount fetches and renders the account, with a bounded timeout so a hung
// put.io call fails the request instead of hanging the handler.
func (d *Dashboard) fetchAccount() (AccountDTO, error) {
	ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
	defer cancel()
	ai, err := d.account.GetAccountInfo(ctx)
	if err != nil {
		return AccountDTO{}, err
	}
	return renderAccount(ai), nil
}

// handleTransfers serves GET /api/transfers.
func (d *Dashboard) handleTransfers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, "method not allowed", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transfers": d.renderTransfers()})
}

// handleAccount serves GET /api/account.
func (d *Dashboard) handleAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, "method not allowed", "")
		return
	}
	acc, err := d.fetchAccount()
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeUpstreamError, "failed to fetch account info", "")
		return
	}
	writeJSON(w, http.StatusOK, acc)
}
