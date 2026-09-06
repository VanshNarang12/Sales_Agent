---
name: techdoc
description: Use at the START of building any new section, feature, module, or roadmap stage of the Sales Copilot, AND whenever finishing one. Creates/updates a techdocs/<topic_name>_techdoc.md capturing what was built, why, and the folder structure, then registers it in techdocs/INDEX.md. Invoke this so any future agent can understand the codebase and automate new feature/scope development. Triggers: "start building <stage/feature>", "begin Stage N", "implement <feature>", "I'm starting work on …", or finishing a section.
---

# Techdoc skill — document every section as we build it

This skill keeps the codebase self-explaining. Every section we build gets a
technical document so that **any new agent (or human) can pick up the project, read
the relevant techdoc, and know what exists, why it was built that way, and how to
extend it** — without re-reading the whole codebase.

## When to run this

- **At the start** of any new section / feature / module / `ROADMAP.md` stage →
  create the techdoc (scaffold it from the template, fill the plan).
- **At the end** of that section → update the same techdoc with what actually got
  built, final folder structure, decisions, and the changelog entry.
- Whenever a decision changes during the work → update the techdoc's
  "Decisions & rationale" section so it never drifts from reality.

## The procedure

### Step 1 — Identify the topic and check for an existing techdoc
1. Determine the **topic name** from the section/feature (e.g. `audio_capture`,
   `realtime_gateway`, `rag_retrieval`, `coaching_engine`).
2. Read `techdocs/INDEX.md` and check whether a techdoc for this topic already
   exists. **If it exists, update it — do not create a duplicate.**

### Step 2 — Create the techdoc (if new)
1. Copy the template at `.claude/skills/techdoc/techdoc_template.md`.
2. Save it to `techdocs/<topic_name>_techdoc.md` using the **mandatory naming
   convention**: `topic_name_techdoc.md` — lowercase, words separated by `_`,
   always ending in `_techdoc.md`. Examples:
   - `audio_capture_techdoc.md`
   - `realtime_gateway_techdoc.md`
   - `stt_adapter_techdoc.md`
   - `coaching_engine_techdoc.md`
3. Fill every section of the template. Cross-reference the source docs by their IDs:
   `ROADMAP.md` stage, `FEATURES.md` feature IDs (e.g. `1.3`), and `ARCHITECTURE.md`
   section (e.g. §5, ADR-002). Never restate them — link to them.

### Step 3 — Register it in the index
Add a row to the registry table in `techdocs/INDEX.md` mapping the feature/section →
this techdoc, with its roadmap stage and status (`planned` / `in-progress` / `done`).
This index is how future agents find the right techdoc for a given feature.

### Step 4 — Follow the coding standards
Before writing code, read `techdocs/coding_standards_techdoc.md`. All code in this
project follows those conventions (Go-only backend, tenant-id propagation,
telemetry-first, testing, naming, gRPC/proto, commits). The techdoc you write must
note any place you deviated and why.

### Step 5 — Keep it true
When the section is complete, revisit the techdoc and:
- confirm the **folder structure** section matches what's actually on disk,
- record final **decisions & rationale** (including rejected alternatives),
- add a dated **changelog** entry,
- flip the index status to `done`.

## Writing style (mandatory — applies to techdocs AND chat explanations)

- **Simple words, full depth.** Keep every technical fact, decision, and number.
  Simplify the wording, not the content.
- Short sentences. No dramatic phrasing ("the missing half", "outsized payoff",
  "honest landscape"). Say it plainly.
- Don't explain basics the reader already knows (what a table is, what an API is).
  Do define product/domain terms on first use (battlecard, tenant, chunk).
- Prefer "X does Y because Z" over long clause chains.

## Rules

- **One topic per techdoc.** If a section spans two clear topics, write two techdocs.
- **Naming is strict:** `topic_name_techdoc.md` (snake_case + `_techdoc.md` suffix).
  The index is the only non-conforming file in `techdocs/` (it's `INDEX.md`).
- **Why over what.** Code already shows *what*. The techdoc's job is *why* — the
  decisions, trade-offs, and rejected options a future agent can't infer from code.
- **Always update the index** when adding or completing a techdoc. An unregistered
  techdoc is invisible to the next agent.
- **Link, don't duplicate.** Reference `FEATURES.md` / `ROADMAP.md` /
  `ARCHITECTURE.md` by ID instead of copying their content.

## Automating new feature / scope development

A future agent asked to build a new feature should:
1. Open `techdocs/INDEX.md` → find which techdoc(s) cover the relevant area.
2. Read those techdocs + `techdocs/coding_standards_techdoc.md`.
3. Run **this skill** to create/update the techdoc for the new work.
4. Build, following the standards, then close out the techdoc (Step 5).

This loop is what lets new sections and scope be developed consistently without a
human re-explaining the project each time.
