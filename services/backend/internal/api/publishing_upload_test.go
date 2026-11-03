package api

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/publishing"
)

func uploadRequest(t *testing.T, metadata string, archive []byte, extra bool) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("metadata", metadata); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("archive", "app.zip")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(archive)
	if extra {
		w.WriteField("unexpected", "value")
	}
	w.Close()
	request := httptest.NewRequest("POST", "/api/v1/publishing", &body)
	request.Header.Set("Content-Type", w.FormDataContentType())
	return request
}
func TestPublishingUploadBounds(t *testing.T) {
	for _, tc := range []struct {
		name      string
		size      int
		extra     bool
		wantError bool
	}{
		{"at boundary", 32, false, false}, {"too large", 33, false, true}, {"empty", 0, false, true}, {"extra field", 32, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := uploadRequest(t, `{"workspaceId":"workspace"}`, bytes.Repeat([]byte{'x'}, tc.size), tc.extra)
			in, reader, err := readPublishingMetadata(request)
			if err != nil || in.WorkspaceID != "workspace" {
				t.Fatal(err)
			}
			dir := t.TempDir()
			file, err := savePublishingArchive(request, reader, dir, publishing.Limits{ArchiveBytes: 32, ExpandedBytes: 64})
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected upload result: %v", err)
			}
			if file != nil {
				defer file.Close()
				stat, _ := file.Stat()
				if stat.Size() != int64(tc.size) {
					t.Fatal("archive size changed")
				}
				data, _ := io.ReadAll(file)
				if len(data) != tc.size {
					t.Fatal("archive not rewound")
				}
			}
			if tc.name == "too large" {
				var limit *publishing.LimitError
				if !errors.As(err, &limit) {
					t.Fatal("oversized upload not distinguished")
				}
				rec := httptest.NewRecorder()
				writePublishingUploadError(rec, err)
				if rec.Code != 413 {
					t.Fatal(rec.Code)
				}
			}
		})
	}
}
func TestPublishingMetadataBoundsAndUnknownFields(t *testing.T) {
	for _, metadata := range []string{`{"unexpected":true}`, `{} {}`, strings.Repeat(" ", publishMetadataLimit+1)} {
		_, _, err := readPublishingMetadata(uploadRequest(t, metadata, []byte("zip"), false))
		if err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	request := httptest.NewRequest("POST", "/api/v1/publishing", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	if _, _, err := readPublishingMetadata(request); err == nil {
		t.Fatal("legacy base64 JSON accepted")
	}
}
func TestPublishingTemporaryUploadIsPrivate(t *testing.T) {
	request := uploadRequest(t, `{}`, []byte("contents"), false)
	_, reader, err := readPublishingMetadata(request)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file, err := savePublishingArchive(request, reader, dir, publishing.Limits{}.WithDefaults())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	stat, err := os.Stat(file.Name())
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("upload permissions are not private")
	}
}
