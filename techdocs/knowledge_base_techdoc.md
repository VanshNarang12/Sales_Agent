# Knowledge Base (Stage 4) — Techdoc

| | |
| --- | --- |
| **Topic** | `knowledge_base` |
| **Roadmap stage** | `ROADMAP.md` Stage 4 — Knowledge Base, minimum |
| **Feature IDs** | `FEATURES.md` — `4.1` document upload, `4.5` chunking + embedding pipeline (`4.2` battlecards dropped 2026-08-30) |
| **Architecture** | `ARCHITECTURE.md` §7.3, §9 |
| **Plane** | Control (ingest); Stage 5 retrieval reads the data |
| **Owner** | Vansh |
| **Status** | in-progress |
| **Last updated** | 2026-08-30 |

## 1. Overview
Stage 4 stores the company's approved answers so Suggest has something to search.
One content type: **documents** — uploaded files (pricing docs, product docs,
security docs, competitor comparisons). There is no separate battlecard entity
(decision 2026-08-30): objection and competitor answers are whatever the uploaded
docs contain.

Flow after this stage: rep clicks Suggest → extraction produces the ask →
(Stage 5) search these tables → show the matching content with its source.

## 2. Scope
- **In:** TXT/MD upload first, then PDF/DOCX text extraction (`4.1`); chunking +
  embedding pipeline (`4.5`); storage in Neon Postgres with pgvector; `Embedder`
  provider layer.
- **Out (with pointers):** search itself (Stage 5); URL/Drive/Notion import,
  versioning, approval workflow (Stage 16).
- **Dropped (2026-08-30):** battlecard/objection structured entry (`4.2`) and its
  Stage-11 editors (11.2/11.3) — documents-only KB. Consequence, accepted: a Suggest
  click returns the best doc excerpt, not a hand-authored comeback; answer quality
  depends on what the uploaded docs contain.

## 3. Decisions & why

- **Chunking: split at structure if present, then recursively; ~450 tokens per
  chunk; ~60-token overlap; never mid-sentence.**
  Why: 2026 benchmarks — recursive ~512-token chunks scored 69% end-to-end vs 54%
  for semantic chunking; splitting at real section boundaries adds 5–10 points.
  Overlap repeats each chunk's tail in the next chunk, so an idea on the boundary
  stays whole in at least one chunk.
  (Sources: PremAI 2026 benchmark guide, Firecrawl 2026, Unstructured, Snowflake
  finance-RAG study.)
- **Assume unformatted input.** Users will not format files for us (decision
  2026-08-30). Headings are used only if they exist; otherwise the splitter works
  on paragraphs and sentences, which every document has. No format is required.
- **Breadcrumb prefix on every chunk.** Chunk text starts with where it came from:
  `"Pricing Guide > Enterprise tier — <text>"`, or just the document title if the
  file had no headings. This keeps a chunk meaningful (and citable) out of context.
- **Tables stay whole** when we can detect them (MD tables; PDF tables if the
  parser exposes them). In plain text they just go through the normal splitter.
- **nomic prefixes are mandatory.** The embedding model `nomic-embed-text-v1.5` is
  trained with task prefixes: store chunks as `search_document: <text>`, embed
  queries (Stage 5) as `search_query: <ask>`. Skipping them measurably hurts
  accuracy. The adapter adds them; callers can't forget.
- **Embeddings from Groq, behind a registry.** Same pattern as `internal/llm`
  (interface + adapters + `Register`): new provider = one file + one line; switching
  = env edit. Groq chosen because it's already our LLM vendor — one free key for
  both. Fallbacks: local Ollama running the same nomic model (vectors stay
  compatible), or Gemini/OpenAI (different model = full re-embed).
- **Chunk size counted approximately (chars ÷ 4 ≈ tokens).** Exact counting needs a
  tokenizer dependency; chunk targets don't need that precision.
- **Store: Neon Postgres + pgvector.** Already provisioned (pgvector 0.8.6 verified
  2026-08-30). One database for both normal data and vectors keeps ops simple.
  A dedicated vector DB (Qdrant) is a scale decision, not an MVP one (ARCH §9).
- **REJECTED, permanent — semantic chunking** (splitting by embedding similarity):
  54% vs 69% end-to-end in benchmarks and 3–5x ingest cost. Do not revisit.
- **Deferred items are in §13 with written triggers** — they are recorded there so
  they survive chat resets; nothing lives only in conversation.

## 4. Folder & file structure
```
internal/embed/
  ├── embed.go         # Embedder interface + Config + Register/New (same shape as internal/llm)
  ├── openai.go        # OpenAI-compatible /embeddings adapter (Groq); adds the nomic prefixes
  └── *_test.go        # registry tests + fake-server adapter tests
internal/kb/
  ├── chunker.go       # structure-if-any → recursive split → overlap → breadcrumb
  ├── chunker_test.go  # unformatted text, headings, overlap, no mid-sentence cuts, tables
  ├── store.go         # kb_documents/kb_chunks writes via db.WithTenantTx (RLS)
  ├── store_test.go
  ├── ingest.go        # pipeline: text → chunks → embed (batched) → store; atomic per doc
  └── extract_text.go  # per-filetype text extraction: txt/md now, pdf/docx next
migrations/0002_knowledge_base.sql   # tables + RLS + vector index
internal/gateway/kb_handlers.go      # POST /v1/documents
internal/platform/config/config.go   # EMBED_*, CHUNK_* knobs
```

## 5. Data flow
```
NOW (simple, synchronous — decision 2026-09-05):
POST /v1/documents (file ≤15 MB) ─► request-scoped buffer in RAM ─► extract text
  ─► chunker ─► embed (Groq, batched: ≤64 chunks per API call, sequential batches)
  ─► store: 1 kb_documents row + N kb_chunks rows via ONE bulk insert (pgx CopyFrom),
     one transaction — all-or-nothing, no half-indexed documents
  ─► respond {document_id, status, chunk_count}; buffer freed with the request

LATER (async, when the UI upload lands — see §13):
UI upload ─► S3/MinIO (original stored) ─► puller ─► queue ─► workers process one by
one. Point: 1000 simultaneous uploads land in object storage, not in RAM — memory
stays flat regardless of upload volume, and stored originals allow re-chunking /
re-embedding without asking users to re-upload.
```
Stage 5 later: ask → `search_query:` embed → nearest-neighbor search in kb_chunks →
return chunks + sources.

## 6. Data model
```sql
CREATE TABLE kb_documents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,            -- filename; shown in citations
    status      TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('processing','ready','failed')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE kb_chunks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
    position    INT  NOT NULL,            -- chunk order inside the document
    heading     TEXT,                     -- breadcrumb path; NULL if the doc had none
    chunk_text  TEXT NOT NULL,            -- breadcrumb-prefixed text that was embedded
    embedding   vector(768) NOT NULL,     -- nomic-embed-text-v1.5 output size
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- RLS on BOTH tables: same ENABLE + POLICY(org_id) + FORCE pattern as 0001.
-- Index: CREATE INDEX ON kb_chunks USING hnsw (embedding vector_cosine_ops);
-- HNSW over IVFFlat because it needs no training data — works from row one.
```
- `org_id` on every row; all access through `db.WithTenantTx`. Tenant A can never
  read tenant B's knowledge — enforced by the database, same as users/orgs.
- Retention: this is customer-approved material, not call data. It stays until
  deleted; delete-on-request arrives with `14.6` (Stage 22).

## 7. APIs
- `POST /v1/documents` — multipart file + title → `{document_id, status, chunk_count}`.
  Tenant comes from the session, never from the request body.
- Embed API down → 502, document marked `failed`, zero chunks written (one
  transaction). Nothing half-indexed, and live calls are never affected — ingest is
  control-plane only.

## 8. External dependencies
- **Groq `/openai/v1/embeddings`** with `nomic-embed-text-v1.5`. Listed in Groq docs
  (checked 2026-08-30); confirm once on the live console. If missing on free tier →
  point `EMBED_BASE_URL` at local Ollama with the same model; vectors stay
  compatible, nothing re-embeds.
- **Neon Postgres** (remote; pgvector 0.8.6) via `DATABASE_URL`.

## 9. Config & secrets
- `EMBED_PROVIDER` (default `openai_compatible`)
- `EMBED_MODEL` (default `nomic-embed-text-v1.5`)
- `EMBED_BASE_URL` (default `https://api.groq.com/openai/v1`)
- `EMBED_DIMS` (default `768`) — must match `vector(768)` in the migration. Changing
  the model later = new migration + re-embed everything. Known, accepted cost.
- `CHUNK_TARGET_TOKENS` (default `450`), `CHUNK_OVERLAP_TOKENS` (default `60`) —
  approximate (chars ÷ 4). Tune only with Stage 13 eval data.
- Secret: reuses `OPENAI_API_KEY` (the Groq key). No new secret.

## 10. How to extend
- **New file type:** add one `extractText<Type>()` in `extract_text.go`. Everything
  after that point works on plain text.
- **New embed provider:** one file in `internal/embed/` + one `Register` line.
  Keep the nomic prefixes inside the adapter.
- **Stage 5 search:** add a store method like `NearestChunks(ctx, tenant, vector, k)`.
  Never query kb_chunks outside `db.WithTenantTx`.
- **Re-chunking after knob changes:** chunks are derived data — wipe a document's
  chunks and re-ingest it.

## 11. Testing
- Chunker: wall-of-text splits at paragraphs/sentences; heading docs split at
  sections; overlap present; no mid-sentence cuts; MD table stays whole; breadcrumb
  falls back to title.
- Ingest with a fake Embedder (no network): embed error → zero rows + doc `failed`;
  prefix applied.
- Store: tenant A cannot read tenant B's chunks.
- End-to-end: upload a real pricing MD → count rows in kb_chunks on Neon.

## 12. Observability
- `kb_ingest_duration_ms`, `kb_chunks_created_total`, `kb_embed_errors_total`,
  `kb_documents_total{kind,status}`.
- Logs carry document_id + tenant, never chunk text (standards §7).

## 13. Deferred / open — WITH triggers (this table is the memory; do not lose it)
| Item | Where it lives | Trigger to act |
|---|---|---|
| Small-to-big retrieval (search small chunks, hand Stage 6 the parent section) | ROADMAP `15.3` | Stage-13 evals: right chunk found, but answers lack context |
| LLM context line per chunk at ingest (Groq, offline) | here + ROADMAP `15.x` | Stage-13 evals: precision misses on out-of-context chunks |
| PDF/DOCX parser library choice (`ledongthuc/pdf` / `pdfcpu`; `unioffice` for docx) | `extract_text.go` | picked while building those extractors, this stage |
| Per-tenant KB size caps | ingest endpoint | before any external pilot user can upload |
| **Async ingest pipeline: UI upload → S3/MinIO → puller → queue → one-by-one workers** (decision 2026-09-05: flat RAM under mass uploads + stored originals enable re-chunk/re-embed without re-upload; `status='processing'` column already supports it) | ROADMAP Stage 16 (`16.6` bulk import) + here | when the upload UI is built, OR ingest exceeds ~30 s, OR bulk import lands |
| Duplicate-upload detection (content hash) | decided AGAINST 2026-09-05 — re-uploading the same file creates a second document (duplicate chunks in search, double embed cost); accepted for now | revisit if duplicate results show up in Stage-13 evals or a pilot complains |
| Semantic chunking | — | **rejected permanently** (54% vs 69%, 3–5x cost) — do not revisit |

## 14. Changelog
- `2026-08-31` — **Battlecards dropped entirely (`4.2` + Stage-11 editors 11.2/11.3).**
  Documents-only KB: one content type, one upload endpoint; `kind` column removed from
  the schema; killer features 9.1/9.2 now retrieve doc excerpts. Accepted trade-off
  recorded in §2. Updated across ROADMAP/FEATURES/README/ARCHITECTURE.
- `2026-08-30` — Rewritten in plain language (style rule added to the techdoc skill);
  added battlecard definition + who/when/how it's inserted (admin, before calls, via
  `POST /v1/battlecards`; UI comes in Stage 11).
- `2026-08-30` — Created at Stage 4 start. Chunking decided from 2026 benchmarks
  (recursive + structure-aware + overlap + breadcrumbs; battlecards unchunked; nomic
  prefixes mandatory; semantic chunking rejected with numbers). Embeddings: Groq
  nomic-embed-text-v1.5 behind an Embedder registry. Store: Neon pgvector (verified),
  HNSW cosine index, RLS on both tables. Deferred items in §13 with triggers. —
  Vansh + Claude
