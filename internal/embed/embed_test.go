package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistryResolvesAndRejectsUnknown(t *testing.T) {
	if _, err := New(Config{Provider: "nope", Model: "m", Dims: 8}); err == nil {
		t.Fatal("want error for unknown provider")
	}
	found := false
	for _, p := range Providers() {
		if p == "openai_compatible" {
			found = true
		}
	}
	if !found {
		t.Fatal("openai_compatible not self-registered")
	}
}

// fakeEmbedServer records requests and returns dims-sized vectors, optionally out of order.
func fakeEmbedServer(t *testing.T, dims int, reverse bool, calls *[][]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("path = %s, want /embeddings", r.URL.Path)
		}
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		*calls = append(*calls, req.Input)

		type item struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		items := make([]item, len(req.Input))
		for i := range req.Input {
			vec := make([]float32, dims)
			vec[0] = float32(i) // marker: first dim = position in this batch
			items[i] = item{Index: i, Embedding: vec}
		}
		if reverse {
			for l, r := 0, len(items)-1; l < r; l, r = l+1, r-1 {
				items[l], items[r] = items[r], items[l]
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": items})
	}))
}

func newTestEmbedder(t *testing.T, url string, dims, batch int) Embedder {
	t.Helper()
	e, err := New(Config{
		Provider: "openai_compatible", Model: "nomic-embed-text-v1.5",
		BaseURL: url, Dims: dims, BatchSize: batch,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return e
}

func TestEmbedAppliesPrefixAndBatches(t *testing.T) {
	var calls [][]string
	srv := fakeEmbedServer(t, 8, false, &calls)
	defer srv.Close()

	texts := make([]string, 150)
	for i := range texts {
		texts[i] = "chunk"
	}
	vecs, err := newTestEmbedder(t, srv.URL, 8, 64).Embed(context.Background(), PrefixDocument, texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 150 {
		t.Fatalf("want 150 vectors, got %d", len(vecs))
	}
	if len(calls) != 3 { // 64 + 64 + 22
		t.Fatalf("want 3 API calls for 150 texts at batch 64, got %d", len(calls))
	}
	if got := calls[0][0]; !strings.HasPrefix(got, "search_document: ") {
		t.Errorf("prefix not applied: %q", got)
	}
}

func TestEmbedReassemblesOutOfOrderResponse(t *testing.T) {
	var calls [][]string
	srv := fakeEmbedServer(t, 4, true, &calls) // server returns items reversed
	defer srv.Close()

	vecs, err := newTestEmbedder(t, srv.URL, 4, 64).
		Embed(context.Background(), PrefixQuery, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	for i, v := range vecs {
		if int(v[0]) != i { // marker must match original position
			t.Fatalf("vector %d has marker %v — order not restored", i, v[0])
		}
	}
}

func TestEmbedWrongDimsFails(t *testing.T) {
	var calls [][]string
	srv := fakeEmbedServer(t, 4, false, &calls) // server returns 4 dims
	defer srv.Close()

	_, err := newTestEmbedder(t, srv.URL, 768, 64). // config expects 768
							Embed(context.Background(), PrefixDocument, []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "dims") {
		t.Fatalf("want dims mismatch error, got %v", err)
	}
}

func TestEmbedAPIErrorSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "rate limited"}})
	}))
	defer srv.Close()

	_, err := newTestEmbedder(t, srv.URL, 8, 64).
		Embed(context.Background(), PrefixDocument, []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("want API error surfaced, got %v", err)
	}
}

func TestEmbedEmptyInput(t *testing.T) {
	var calls [][]string
	srv := fakeEmbedServer(t, 8, false, &calls)
	defer srv.Close()

	vecs, err := newTestEmbedder(t, srv.URL, 8, 64).Embed(context.Background(), PrefixDocument, nil)
	if err != nil || vecs != nil {
		t.Fatalf("want (nil, nil) for empty input, got (%v, %v)", vecs, err)
	}
	if len(calls) != 0 {
		t.Fatalf("no API call expected for empty input, got %d", len(calls))
	}
}
