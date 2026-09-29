# Meetings APIs (org-wide call history) — Techdoc

| | |
| --- | --- |
| **Topic** | `meetings_api` |
| **Roadmap stage** | Stage 10 extension (feeds the web app's Meetings page) |
| **Feature IDs** | serves `9.12`–`9.13` data org-wide; groundwork for `13.1` usage stats |
| **Architecture** | `ARCHITECTURE.md` §4 (control plane REST) |
| **Plane** | Control |
| **Owner** | Vansh |
| **Status** | done (live smoke test with a real call still pending) |
| **Last updated** | 2026-09-23 |

## 1. Overview
The web app's Meetings page, dashboard tiles and profile stats need one thing the
backend never had: "every summarized customer call in my org, newest first, with
per-call numbers". Stage 10 stored summaries per customer; these APIs read them
org-wide, and the live call session now records the numbers each row needs.

## 2. APIs
- `GET /v1/meetings?limit=&cursor=` → `{meetings: [...], next_cursor?}` — list,
  newest first. Row: `meeting_id, client_name, started_at, duration_minutes,
  summary, suggestions_count, sources[]`. Action items/unanswered are stripped —
  they belong to the detail view.
- `GET /v1/meetings/{id}` → one meeting in full (adds `action_items`,
  `unanswered`). Invalid UUID or other-org id → 404 (RLS hides foreign rows).
Both behind the full-scope auth middleware.

## 3. Decisions & rationale
- **Stats are captured live, in the session, not derived later.** Duration, cards
  served, and cited documents cannot be reconstructed after the call — the
  transcript expires and suggestion traffic is never stored. So the WS session
  counts them as they happen and stamps them into the summary row at close.
- **`sources` = documents actually cited by served cards** (card citations), not
  every retrieval hit — the honest number for "what the copilot used".
- **Cursor = `created_at|id`** (keyset pagination on the 0007 index), not
  offset — stable under concurrent inserts and O(page) at any depth.
- **Old rows degrade gracefully:** pre-0007 rows read as duration 0, 0 cards,
  `[]` sources, `started_at` = `created_at`.

## 4. Folder & file structure
```
migrations/0007_meeting_stats.sql        # stats columns + org-wide list index
internal/gateway/ws.go                   # session: startedAt, recordCard(), stats()
internal/postcall/store.go               # Stats type; SaveSummary persists them;
                                         # Meeting, ListMeetings, GetMeeting
internal/gateway/meetings_handlers.go    # the two HTTP handlers
internal/gateway/server.go               # route registration
```

## 5. Data model
`call_summaries` gains `started_at TIMESTAMPTZ`, `duration_seconds INT`,
`suggestions_count INT`, `sources JSONB` (0007), plus index
`(org_id, created_at DESC, id DESC)` for the list read. RLS unchanged (0003
policy covers the new columns).

## 6. How to extend
- New per-call stat: add a field to `session` + `postcall.Stats`, record it where
  it happens in `ws.go`, add a column in a new migration, thread through
  `SaveSummary`/`meetingCols`/`scanMeeting`.
- Org-wide totals for the dashboard (exact counts beyond page 1): add a
  `SELECT count(*), sum(...)` store method + `/v1/meetings/stats` endpoint.

## 7. Testing & verification
Unit suites pass. Live check: apply 0007 → run a customer-tagged call with a few
Suggest clicks → `GET /v1/meetings` shows the row with real duration/cards/sources.

## 8. Changelog
- `2026-09-23` — Endpoints + live stats capture built; migration 0007 written
  (not applied). Frontend contract matches `Sales_Agent_Web/src/lib/api.ts`. — Claude
- `2026-09-27` — Migration 0007 applied to the dev DB and verified. Live smoke
  test (real call → row with duration/cards/sources) still pending. — Claude
