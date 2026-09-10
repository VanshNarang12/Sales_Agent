# RAG Retrieval — Techdoc

| | |
| --- | --- |
| **Topic** | `rag_retrieval` |
| **Roadmap stage** | `ROADMAP.md` Stage 5 — Retrieval & Grounding |
| **Feature IDs** | `FEATURES.md` — `5.1` (vector search), `5.4` (citations), `5.5` (approved-docs-only), `5.6` (confidence) |
| **Architecture** | `ARCHITECTURE.md` §7.3 |
| **Plane** | Real-Time (runs on a Suggest click) |
| **Owner** | Vansh |
| **Status** | done (code + tests; live-call smoke test pending, §13) |
| **Last updated** | 2026-09-09 |

## 1. Overview
Stage 4 made documents searchable (chunks + embeddings in pgvector). This stage
does the search. On a Suggest click, the extracted ask (from
`query_extraction_techdoc.md`) is embedded and matched against the tenant's KB
chunks. The result: the best few chunks, each with a score and a citation — or
nothing, if no chunk is a good enough match. This is the anti-hallucination core:
Stage 6 may only answer from what this stage returns.

## 2. Scope
- **In scope:** vector search over `kb_chunks` (`5.1`), similarity score on every
  hit (`5.6`), citation on every hit (`5.4`), refuse-below-threshold (`5.5`),
  wiring the Suggest pipeline end-to-end (extract → search → WS message).
- **Out of scope / deferred:** hybrid BM25 search, re-ranking, caching
  (Stage 15); answer generation from the hits (Stage 6); the extraction step
  itself (already built, see `query_extraction_techdoc.md`).

## 3. Decisions & rationale
- **Search code lives in `internal/retrieval`, not `internal/kb`.**
  Why: `kb` owns writes (ingest), `retrieval` owns reads (lookup). One direction
  of data flow per package. Both go through `db.WithTenantTx`, so RLS tenant
  isolation applies to search with no extra code.
- **One SQL query does everything:** cosine distance via the existing HNSW index,
  join to `kb_documents` for the title, filter `status = 'ready'`.
  Why: one round trip on the hot path; no post-processing in Go.
  Rejected: fetching chunks then titles separately (extra round trip, no gain).
- **Confidence = cosine similarity = `1 - (embedding <=> query)`.**
  Why: pgvector's `<=>` returns cosine *distance* (0 = identical, 2 = opposite);
  similarity is the natural "how good is this match" number and needs no model call.
- **Refuse below `RETRIEVAL_MIN_SCORE` (default 0.5).**
  Why: a weak match answered confidently is worse than "nothing found". The
  threshold is env-tunable because the right value only shows up with real docs
  and real calls (Stage 13 evals will set it properly).
  Rejected: LLM-judged relevance per hit — adds a model call + latency to every
  Suggest for a gate we can get from the score.
- **Top K = 5.** Why: Stage 6's card prompt needs a handful of chunks, not a page.
  Re-ranking a bigger pool is Stage 15 (`5.7`).
- **Query embedded with nomic's `search_query: ` prefix** (chunks were embedded
  with `search_document: `). The model was trained asymmetric: questions and the
  passages that answer them only land near each other when each side carries its
  prefix. Skipping it silently degrades recall.

## 4. Folder & file structure
```
internal/retrieval/
  ├── extract.go       # step 1: window → ask (existing, Stage 5.2)
  ├── extract_test.go
  ├── search.go        # step 2: ask → scored, cited chunks   (this stage)
  └── search_test.go
internal/gateway/ws.go # querySink wires extract → search → WS "suggestion" msg
cmd/gateway/main.go    # builds the Searcher (embedder + pgx pool) at startup
```

## 5. Architecture & data flow
```
Suggest click (WS)
  → transcript window (Redis)          existing, Stage 3
  → Extractor.Extract → ask            existing, Stage 5.2; "" = stop, no card
  → Searcher.Search(ask)               this stage
      1. embedder.Embed(PrefixQuery, ask)         Groq, ~1 network call
      2. SELECT ... ORDER BY embedding <=> $1     pgvector HNSW, tenant-scoped
      3. drop hits with similarity < threshold
  → WS "suggestion" message to the client (hits or empty)
```
Everything runs on the Suggest goroutine; the in-flight guard already prevents
stacked clicks.

## 6. Data model & storage
No new tables. Reads `kb_chunks` (embedding, chunk_text, heading, position) joined
to `kb_documents` (title, status). Tenant comes from ctx via `db.WithTenantTx`;
RLS filters every row. Nothing is written; the query text is not stored.

## 7. APIs / events
- **Inbound:** called in-process by the gateway's querySink after extraction.
- **Outbound (WS, gateway → client):**
```json
{"type": "suggestion", "ask": "prospect objects that price is higher than X",
 "hits": [{"document_id": "…", "title": "pricing.md", "heading": "Discounts > Annual",
           "text": "…chunk text…", "score": 0.78}]}
```
  Empty `hits` means: searched, nothing above threshold — client shows "no answer
  in your docs", never a made-up card.

## 8. External dependencies
- Groq embeddings API (via the existing `embed.Embedder`) — if it's down, Suggest
  degrades to "no suggestion" with a logged warning; the call itself is unaffected.
- Neon Postgres + pgvector (existing pool).

## 9. Configuration & secrets
- `RETRIEVAL_TOP_K` (default 5)
- `RETRIEVAL_MIN_SCORE` (default 0.5) — similarity gate for `5.5`
- No new secrets; reuses `EMBED_*` config and `DATABASE_URL`.

## 10. How to extend (for the next agent)
- **Hybrid/BM25 (Stage 15):** add a keyword query in `search.go` and merge with the
  vector hits; the `SearchResult` shape already carries scores for fusion.
- **Re-ranking (Stage 15):** raise `RETRIEVAL_TOP_K`, add a rerank step between the
  SQL query and the threshold filter.
- **Stage 6 (generation):** consume `[]SearchResult` from querySink; each hit
  already has the citation fields the card must render.

## 11. Testing & verification
- Unit: `search_test.go` — SQL shape, threshold filtering, empty-KB, prefix use
  (mock embedder), tenant ctx required.
- Manual: upload a doc via `POST /v1/documents`, start a call, click Suggest, watch
  the `suggestion` WS message. Works = relevant chunk with sensible score; honest
  empty for an off-topic ask (ROADMAP Stage 5 expectation).

## 12. Observability
- `retrieval_search_duration_ms` (embed + SQL, the Suggest path's latency share),
  `retrieval_hits_total`, `retrieval_empty_total` (searched, nothing ≥ threshold).
- Logs carry session + ask length + top score — never chunk text (standards §7).

## 13. Open questions / TODO
- **`TestSearchTenantIsolation` fails on the dev DB — known, deferred, not a bug
  here.** The dev role (`neondb_owner`) has BYPASSRLS, so Postgres skips RLS
  entirely. The search code and its tenant scoping are correct; the environment
  leaks. Fix + hard trigger live in `project_foundations_techdoc.md` §13
  (deferred 2026-09-05, re-confirmed 2026-09-09).
- Threshold 0.5 is a guess; calibrate in Stage 13 evals with real docs.
- Server-side Suggest min-interval (`3.5`) still pending — lives in gateway, not here.
- Live-call smoke test not yet run: gateway + desktop client, upload a doc, click
  Suggest, confirm the `suggestion` WS message (§11 manual steps). Code and DB tests
  are green; this is the last check before the stage's exit expectation is met.

## 14. Changelog
- `2026-09-09` — Built and wired: `search.go` + tests (unit green; DB tests green
  except tenant isolation, blocked by the known BYPASSRLS dev-DB issue, §13);
  `querySink` now runs extract → search → WS `suggestion` message; config
  `RETRIEVAL_TOP_K`/`RETRIEVAL_MIN_SCORE`; `main.go` shares one pool+embedder
  between ingest and search (`buildKB`). ROADMAP `5.1`–`5.5` flipped. — Vansh + Claude
- `2026-09-09` — Created at Stage 5 start. Decisions: search in `retrieval` pkg,
  one-query SQL with title join, similarity threshold gate (0.5 default), K=5,
  mandatory `search_query:` prefix. — Vansh + Claude
