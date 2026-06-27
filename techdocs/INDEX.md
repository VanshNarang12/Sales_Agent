# Techdocs Index — start here

**This is the entry point for any agent or developer working on the Sales Copilot.**
It tells you (1) how we document and build, (2) the coding methods we follow, and
(3) **which techdoc to read for which feature**. Read this file first.

> Companion product docs (the "what" and "why-build"): [`README.md`](../README.md),
> [`FEATURES.md`](../FEATURES.md), [`ROADMAP.md`](../ROADMAP.md),
> [`ARCHITECTURE.md`](../ARCHITECTURE.md).

---

## 1. How we work (the loop)

Every section/feature of this project is documented as it's built. The workflow is
enforced by the **`techdoc` skill** (`.claude/skills/techdoc/`):

1. **Starting a new section/feature/stage?** → run the **`techdoc` skill**. It
   scaffolds `techdocs/<topic_name>_techdoc.md` from the template and registers it
   below.
2. **Read first:** this index → the relevant techdoc(s) → `coding_standards_techdoc.md`.
3. **Build** following the coding standards.
4. **Finishing the section?** → run the `techdoc` skill again to close out the
   techdoc (final folder structure, decisions, changelog) and flip its status to
   `done` here.

**Naming convention (mandatory):** every techdoc is `topic_name_techdoc.md` —
lowercase, `snake_case`, ending in `_techdoc.md`. This file (`INDEX.md`) is the only
exception.

---

## 2. Coding methods (summary)

Full detail in **[`coding_standards_techdoc.md`](./coding_standards_techdoc.md)**.
The essentials every agent must follow:

- **Go-only backend**, all three planes (ARCH ADR-002). No Python unless we self-host
  a model, and then async-plane only.
- **`tenant_id` is propagated through every request, row, event, cache key.** Tenant
  isolation is never optional.
- **Telemetry-first:** every hot-path component emits OpenTelemetry traces + the
  per-stage latency metric *before* it's optimized.
- **Hot path = gRPC + streaming**, co-located, no extra hops. Control plane = REST.
- **Grounded-only AI:** retrieval returns citations or nothing; the guardrail gate
  runs before any card is shown.
- **Consent/compliance enforced in code**, not docs (consent gate before STT;
  ephemeral audio by default).
- **Tests + eval gates** for anything on the live-critical path.
- **Standard project layout, error handling, naming, and commit format** per the
  standards doc.

---

## 3. Techdoc registry (which techdoc for which feature)

When building or changing a feature, find its area below and read that techdoc.
Status: `planned` · `in-progress` · `done`.

| Topic / techdoc | Roadmap stage | Feature IDs | Architecture | Status |
| --- | --- | --- | --- | --- |
| [`coding_standards_techdoc.md`](./coding_standards_techdoc.md) | all | — | §19, ADR-002 | done |
| [`project_foundations_techdoc.md`](./project_foundations_techdoc.md) | Stage 0 | 0.1–0.9, `20.8`,`20.9`,`20.1`,`16.1`,`15.6`,`15.1`,`15.9`,`18.1` | §9,§13,§17 | in-progress |
| [`audio_capture_techdoc.md`](./audio_capture_techdoc.md) | Stage 1 | `1.1`–`1.6`,`1.8`,`1.12` | §10 | in-progress |
| [`transcription_techdoc.md`](./transcription_techdoc.md) | Stage 2 | `2.1`–`2.6`,`2.10` | §7.1 | in-progress |
| _detection_techdoc.md_ | Stage 3 | `3.1`–`3.4`,`3.11` | §7.2 | planned |
| _knowledge_base_techdoc.md_ | Stage 4 / 16 | `4.1`,`4.2`,`4.5` + ingestion | §7.3,§9 | planned |
| _rag_retrieval_techdoc.md_ | Stage 5 | `5.1`,`5.3`–`5.6` | §7.3 | planned |
| _suggestion_generation_techdoc.md_ | Stage 6 | `6.1`–`6.4`,`6.8`,`6.12`,`18.6`,`18.9` | §7.4 | planned |
| _overlay_ui_techdoc.md_ | Stage 7 | `7.1`–`7.5`,`7.7`,`7.7a`,`7.9`,`20.5` | §10 | planned |
| _consent_compliance_techdoc.md_ | Stage 8 / 22 | `14.1`–`14.7`,`14.12` | §15 | planned |
| _sales_guidance_techdoc.md_ | Stage 9 | `8.1`–`8.6`,`6.5`,`6.7`,`3.6` | §7.4 | planned |
| _post_call_techdoc.md_ | Stage 10 | `9.1`,`9.2`,`9.9` | §6 | planned |
| _admin_playbook_techdoc.md_ | Stage 11 | `12.1`–`12.4` | §4 | planned |
| _billing_onboarding_techdoc.md_ | Stage 12 | `16.2`,`16.3`,`16.7`,`16.9`,`17.1`,`17.2` | §4 | planned |
| [`realtime_gateway_techdoc.md`](./realtime_gateway_techdoc.md) | Stage 0/1 | gateway, WS, session, frame parsing | §4,§5 | in-progress |
| _orchestrator_techdoc.md_ | Stage 1–7 | call session orchestration | §4,§5 | planned |
| _crm_integration_techdoc.md_ | Stage 19 | `10.1`–`10.5`,`9.3`,`9.4` | §4 | planned |
| _coaching_engine_techdoc.md_ | Stage 21 / 26 | `13A.1`–`13A.12`,`13A.19`,`13A.20` | §8 | planned |
| _analytics_techdoc.md_ | Stage 20 / 27 | `13.1`–`13.6` | §4,§16 | planned |

> Add a row whenever you create a new techdoc. Italic rows are not yet created —
> they show the intended topic + naming so the next agent knows what to make.

---

## 4. Conventions for this index
- Keep the registry sorted roughly by build order (roadmap stage).
- One row per techdoc; update **Status** as work progresses.
- If a feature doesn't map to any techdoc, that's a signal to create one (run the
  skill) before building it.
