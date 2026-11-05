package api

import (
	"testing"

	"github.com/justlab/justcd/services/backend/internal/config"
	"github.com/justlab/justcd/services/backend/internal/publishing"
)

func TestRequestBodyLimitFollowsArchiveEndpoints(t *testing.T) {
	s := &Server{Config: config.Config{PublishingLimits: publishing.Limits{ArchiveBytes: 100 << 20}}}
	archive := int64(100<<20) + publishMultipartOverhead
	for _, path := range []string{"/api/v1/publishing", "/api/v1/publishing/validate-archive"} {
		if got := s.requestBodyLimit(path); got != archive {
			t.Errorf("%s limit = %d, want the archive limit %d", path, got, archive)
		}
	}
	for _, path := range []string{"/api/v1/publishing/repository/inspect", "/api/v1/workspaces/w/publishing-profiles"} {
		if got := s.requestBodyLimit(path); got != 2<<20 {
			t.Errorf("%s limit = %d, want 2 MiB", path, got)
		}
	}
	if got := s.requestBodyLimit("/api/v1/agents/tasks/t1/result"); got != 12<<20 {
		t.Errorf("agent result limit = %d", got)
	}
}
