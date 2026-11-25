package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/justscan"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) RunJustScanWorker(acquireCtx, executeCtx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-acquireCtx.Done():
			return
		case <-ticker.C:
		}
		if acquireCtx.Err() != nil {
			return
		}
		work, claimed, err := s.Store.ClaimJustScan(acquireCtx)
		if err != nil {
			if acquireCtx.Err() == nil {
				s.Logger.Error("could not claim JustScan scan", "error", err)
			}
			continue
		}
		if !claimed {
			continue
		}
		s.runJustScan(executeCtx, &work)
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(executeCtx), 5*time.Second)
		if err := s.Store.FinishJustScan(finishCtx, work); err != nil {
			s.Logger.Error("could not persist JustScan result", "planId", work.PlanID, "error", err)
		}
		cancel()
	}
}

func (s *Server) runJustScan(ctx context.Context, work *store.JustScanWork) {
	if s.Config.Production && !strings.HasPrefix(work.ServerURL, "https://") {
		work.Status = "error"
		return
	}
	token, err := security.Decrypt(s.EncryptionKey, work.TokenCipher, "justscan:"+work.WorkspaceID)
	if err != nil {
		work.Status = "error"
		return
	}
	client, err := justscan.NewClient(work.ServerURL, work.OrganizationID, string(token))
	if err != nil {
		work.Status = "error"
		return
	}

	if work.Status == "submitting" {
		scan, err := client.Submit(ctx, justscan.Target{Image: work.Image, ExternalRef: "justcd:plan:" + work.PlanID})
		if err != nil {
			work.Status = "error"
			s.Logger.Warn("JustScan submission failed", "planId", work.PlanID, "error", err)
			return
		}
		work.ScanID = scan.ID
		work.ReportURL = safeJustScanReportURL(scan.URL)
		work.Status = "pending"
		return
	}
	scan, err := client.Status(ctx, work.ScanID)
	if err != nil {
		s.Logger.Warn("JustScan status check failed", "planId", work.PlanID, "error", err)
		var response *justscan.HTTPError
		if errors.As(err, &response) && response.Status >= 400 && response.Status < 500 && response.Status != http.StatusTooManyRequests && response.Status != http.StatusRequestTimeout {
			work.Status = "error"
		}
		return
	}
	if report := safeJustScanReportURL(scan.URL); report != "" {
		work.ReportURL = report
	}
	work.Status = scan.Verdict
	switch scan.Verdict {
	case "pass", "fail":
		_, expected, _ := strings.Cut(work.Image, "@")
		if !strings.EqualFold(scan.ImageTag, expected) {
			work.Status = "error"
			return
		}
		work.CriticalCount = scan.CriticalCount
		work.HighCount = scan.HighCount
	case "pending", "error":
	default:
		work.Status = "error"
	}
}

func safeJustScanReportURL(raw string) string {
	if _, err := justscan.ParseServer(raw); err != nil {
		return ""
	}
	return raw
}
