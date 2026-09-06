package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/VanshNarang12/sales-agent/internal/kb"
)

// DocumentIngester is what the gateway needs from Stage-4 ingest (kb.Ingester
// satisfies it; tests use a fake). nil ⇒ upload endpoint answers 503.
type DocumentIngester interface {
	IngestDocument(ctx context.Context, title, text string) (documentID string, chunkCount int, err error)
}

// handleDocumentUpload: POST /v1/documents — multipart `file` (+ optional `title`).
// Flow: size gate → read bytes → ExtractText → IngestDocument → 201.
func (s *Server) handleDocumentUpload(w http.ResponseWriter, r *http.Request) {
	if s.ingest == nil {
		http.Error(w, "document ingestion unavailable (embedding/DB not configured)", http.StatusServiceUnavailable)
		return
	}
	maxBytes := int64(s.cfg.MaxUploadMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		http.Error(w, "file too large or malformed multipart body", http.StatusRequestEntityTooLarge)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `multipart field "file" is required`, http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "reading upload failed", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	if title == "" {
		title = header.Filename
	}

	text, err := kb.ExtractText(header.Filename, data)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, kb.ErrUnsupportedType) {
			status = http.StatusUnsupportedMediaType
		}
		http.Error(w, err.Error(), status)
		return
	}

	docID, chunks, err := s.ingest.IngestDocument(r.Context(), title, text)
	if err != nil {
		s.log.Warn("ingest failed", "title", title, "err", err)
		http.Error(w, "ingestion failed", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"document_id": docID,
		"status":      "ready",
		"chunk_count": chunks,
	})
}
