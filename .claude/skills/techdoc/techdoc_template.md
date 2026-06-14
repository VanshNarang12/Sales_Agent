<!--
TEMPLATE — copy to techdocs/<topic_name>_techdoc.md and fill in.
Naming: lowercase snake_case + `_techdoc.md`  e.g. audio_capture_techdoc.md
Delete this comment block after copying.
-->

# <Topic Name> — Techdoc

| | |
| --- | --- |
| **Topic** | `<topic_name>` |
| **Roadmap stage** | `ROADMAP.md` Stage N — <stage title> |
| **Feature IDs** | `FEATURES.md` — e.g. `1.1`, `1.3`, `2.4` |
| **Architecture** | `ARCHITECTURE.md` — e.g. §5, §10, ADR-002 |
| **Plane** | Real-Time / Control / Async-Data |
| **Owner** | <name / team> |
| **Status** | planned · in-progress · done |
| **Last updated** | <YYYY-MM-DD> |

## 1. Overview
One paragraph: what this section is and the value it delivers. What can a user/rep
do once it's built that they couldn't before?

## 2. Scope
- **In scope:** the features (by ID) this section delivers.
- **Out of scope / deferred:** what is intentionally not here yet, and where it lives.

## 3. Decisions & rationale (the "why")
The most important section. For each non-obvious choice:
- **Decision:** what we chose.
- **Why:** the reasoning and the constraint it serves (latency? compliance? cost?).
- **Alternatives rejected:** what we didn't do and why.
Link to any relevant ADR in `ARCHITECTURE.md`.

## 4. Folder & file structure
What directories/files this section adds, and the responsibility of each. Keep this
matching what's actually on disk.
```
path/to/module/
  ├── ...        # what it does
  └── ...        # what it does
```

## 5. Architecture & data flow
How this fits the system. A short diagram or step list of the flow in/out of this
component. Note services it calls and is called by (gRPC/WS/events).

## 6. Data model & storage
Tables/collections/keys touched or added (with `tenant_id` note), what is stored
where (Postgres / Redis / S3 / vector / ClickHouse), and retention/consent handling
if it touches customer or personal data.

## 7. APIs / events
- **Inbound:** endpoints / gRPC methods / events consumed.
- **Outbound:** events emitted, downstream calls made.
- Include payload shapes or links to proto/schema.

## 8. External dependencies
Third-party APIs (STT/LLM/CRM), libraries, infra. Note failover/degradation.

## 9. Configuration & secrets
Config flags, env, feature flags, secrets used (named, not valued).

## 10. How to extend (for the next agent)
Concrete: "to add X, edit Y and Z, follow pattern in <file>." The fast path for
future automated feature work.

## 11. Testing & verification
How this is tested (unit/integration/eval), how to run it locally, and what "working"
looks like (tie to the section's exit expectation in `ROADMAP.md`).

## 12. Observability
Metrics/traces/logs this section emits; latency-budget contribution if on the hot path.

## 13. Open questions / TODO
Known gaps, risks, follow-ups.

## 14. Changelog
- `<YYYY-MM-DD>` — <what changed> — <author>
