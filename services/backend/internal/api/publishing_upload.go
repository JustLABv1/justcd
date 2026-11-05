package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"github.com/justlab/justcd/services/backend/internal/publishing"
)

const publishMetadataLimit = 64 << 10
const publishMultipartOverhead = 128 << 10

// Metadata must arrive first so workspace authorization happens before source is
// written to disk. Only bounded configuration/credential data is held in memory.
func readPublishingMetadata(r *http.Request) (publishInput, *multipart.Reader, error) {
	var input publishInput
	reader, err := r.MultipartReader()
	if err != nil {
		return input, nil, errors.New("publishing requires multipart/form-data with metadata followed by archive")
	}
	part, err := reader.NextPart()
	if err != nil {
		return input, nil, errors.New("publishing metadata is required")
	}
	if part.FormName() != "metadata" || part.FileName() != "" {
		return input, nil, errors.New("the first multipart field must be metadata JSON")
	}
	data, err := io.ReadAll(io.LimitReader(part, publishMetadataLimit+1))
	if err != nil {
		return input, nil, err
	}
	if len(data) > publishMetadataLimit {
		return input, nil, &publishing.LimitError{Message: "publishing metadata exceeds 64 KiB"}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		return input, nil, errors.New("invalid publishing metadata")
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return input, nil, errors.New("publishing metadata must contain one JSON object")
	}
	return input, reader, nil
}

func savePublishingArchive(r *http.Request, reader *multipart.Reader, dir string, limits publishing.Limits) (*os.File, error) {
	part, err := reader.NextPart()
	if err != nil {
		return nil, errors.New("application ZIP archive is required")
	}
	if part.FormName() != "archive" || part.FileName() == "" {
		return nil, errors.New("metadata must be followed by one archive file")
	}
	file, err := os.CreateTemp(dir, "upload-*.zip")
	if err != nil {
		return nil, errors.New("cannot allocate temporary upload storage")
	}
	ok := false
	defer func() {
		if !ok {
			file.Close()
		}
	}()
	n, err := io.Copy(file, io.LimitReader(publishContextReader{r: r, reader: part}, limits.ArchiveBytes+1))
	if n > limits.ArchiveBytes {
		return nil, &publishing.LimitError{Message: "ZIP archive exceeds " + publishing.SizeLabel(limits.ArchiveBytes)}
	}
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errors.New("upload a non-empty ZIP archive")
	}
	if _, err = reader.NextPart(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("only metadata and one archive file are allowed")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, errors.New("cannot read temporary upload storage")
	}
	ok = true
	return file, nil
}

type publishContextReader struct {
	r      *http.Request
	reader io.Reader
}

func (c publishContextReader) Read(p []byte) (int, error) {
	if err := c.r.Context().Err(); err != nil {
		return 0, err
	}
	return c.reader.Read(p)
}
func writePublishingUploadError(w http.ResponseWriter, err error) {
	var limit *publishing.LimitError
	var max *http.MaxBytesError
	if errors.As(err, &limit) {
		writeError(w, http.StatusRequestEntityTooLarge, limit.Error())
		return
	}
	if errors.As(err, &max) {
		writeError(w, http.StatusRequestEntityTooLarge, "publishing request exceeds the configured upload limit")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}
func (s *Server) getPublishingLimits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Config.PublishingLimits.WithDefaults())
}

// getPublishingTemplates lists the built-in templates. With a workspaceId the
// workspace's own templates follow them.
func (s *Server) getPublishingTemplates(w http.ResponseWriter, r *http.Request) {
	workspace := r.URL.Query().Get("workspaceId")
	if workspace == "" {
		writeJSON(w, http.StatusOK, map[string]any{"items": publishing.Templates()})
		return
	}
	if !s.requireWorkspaceRole(w, r, workspace, "viewer") {
		return
	}
	items, err := s.workspaceTemplates(r.Context(), workspace)
	if err != nil {
		writeStoreError(w, "could not load templates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// Validate an upload without creating repositories, pipelines, or applications.
func (s *Server) validatePublishingArchive(w http.ResponseWriter, r *http.Request) {
	limits := s.Config.PublishingLimits.WithDefaults()
	r.Body = http.MaxBytesReader(w, r.Body, limits.ArchiveBytes+publishMetadataLimit+publishMultipartOverhead)
	in, reader, err := readPublishingMetadata(r)
	if err != nil {
		writePublishingUploadError(w, err)
		return
	}
	if !s.requireWorkspaceRole(w, r, in.WorkspaceID, "deployer") {
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(5 * time.Minute))
	_ = controller.SetWriteDeadline(time.Now().Add(6 * time.Minute))
	dir, err := os.MkdirTemp(s.Config.PublishingTempDir, "justcd-validate-*")
	if err != nil {
		writeError(w, 500, "cannot allocate temporary upload storage")
		return
	}
	defer os.RemoveAll(dir)
	archive, err := savePublishingArchive(r, reader, dir, limits)
	if err != nil {
		writePublishingUploadError(w, err)
		return
	}
	defer archive.Close()
	stat, err := archive.Stat()
	if err != nil {
		writeError(w, 500, "cannot inspect archive")
		return
	}
	source, err := publishing.ExtractArchive(r.Context(), archive, stat.Size(), dir, limits)
	if err != nil {
		writePublishingUploadError(w, err)
		return
	}
	if err = s.attachTemplate(r.Context(), in.WorkspaceID, &in.Config); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	files, root, err := publishing.NormalizeSource(in.Config, source)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	message := "ZIP structure is valid for this template."
	if root != "" {
		message = "ZIP structure is valid. Enclosing folder " + root + "/ will be removed automatically."
	}
	if in.Config.Runtime == "python-static" {
		message += " The build command and generated output are checked during CI."
	}
	writeJSON(w, 200, map[string]any{"valid": true, "rootDirectory": root, "fileCount": len(files), "message": message})
}
