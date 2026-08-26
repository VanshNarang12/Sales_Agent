# Sales Copilot — Real-Time AI Sales-Call Assistant

A real-time, **source-backed sales copilot** for B2B sales teams. It listens during
live Zoom / Google Meet / Microsoft Teams calls and transcribes the conversation. When
the rep clicks **Suggest**, it reads the last minute or two of the call, searches the
company's approved knowledge base, and surfaces a short, cited coaching suggestion **in
the moment** — all with consent and compliance built in. (The rep triggers help with
one click; there's no covert auto-listening deciding when to interrupt.)

> **Positioning:** _"Your sales playbook, battlecards, case studies, and product
> docs — live in the call, exactly when the buyer asks."_
>
> **Not:** a hidden/undisclosed "cheat" overlay. We win on **trust, accuracy,
> low latency, and document-grounding**, not on covertness.

## The problem

Sales calls are high-pressure and information-heavy. Even good reps can't recall
every case study, pricing rule, competitor detail, security answer, and objection
response on demand. Junior reps ramp slowly. Deals die when a rep fumbles a live
objection. This product gives every rep the company's best answer at the exact
moment of doubt.

## Core promise

> **"Never lose a deal because a rep forgot the right answer."**

Every suggestion is grounded in the team's approved docs, battlecards, pricing,
and case studies — with citations.

## Target wedge (ICP)

- B2B SaaS / technical-service companies, **$1M–$30M ARR**, **5–50 reps**
- Using **Zoom or Google Meet** + **HubSpot or Salesforce**
- Complex/consultative, high-ticket calls with demos, objections, and
  security/pricing questions
- New reps that take too long to ramp

## The three killer features

1. **Real-time objection-handling cards** — the rep clicks **Suggest** on an objection
   and gets the approved, cited rebuttal.
2. **Live competitor battlecards** — click **Suggest** after a competitor comes up and
   pull the battlecard for that competitor.
3. **"Do-not-say" guardrails** — every suggestion the copilot produces is vetted before
   it's shown, so it never tells a rep to over-promise on pricing, legal, security, or
   implementation. (A guardrail on *our own answers* — not a listener on the rep's
   speech. There is no auto-detection anywhere.)

Plus a **per-employee performance & coaching engine**: register every rep, and
after **every call** they get a scored "what you did well / what you did poorly"
review. Performance is tracked over time and each rep gets a personalized plan for
**how to improve** and **what to keep doing** — an always-on AI coach for the whole
team (transparent and consent-based, not covert monitoring). See §13A in
[`FEATURES.md`](./FEATURES.md).

## Documents

- [`FEATURES.md`](./FEATURES.md) — **the complete, prioritized feature analysis**
  (the main artifact: every feature required to build this product, grouped by
  capability area and release tier).
- [`ROADMAP.md`](./ROADMAP.md) — **the sequential build roadmap**: every feature
  arranged in build order across 30 dependency-ordered stages (Foundations →
  MVP → V1 → V2 → Future), each step referenced back to its `FEATURES.md` ID with
  checkboxes to track progress.
- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — **the technical architecture**: how we
  build it, the service decomposition (3 planes), the latency-critical real-time
  pipeline + budget, AI/ML subsystem, data architecture, the §13A coaching engine,
  security/compliance, deployment topology, full tech stack, and ADRs.
- [`techdocs/INDEX.md`](./techdocs/INDEX.md) — **start here when building.** The
  per-section technical docs, the coding standards, and the registry of which
  techdoc covers which feature. New sections are documented via the `techdoc` skill
  (`.claude/skills/techdoc/`).
- `Verdict.md` — strategic / market verdict (source input).
- `Market Research_ Real-Time AI Sales-Call Assistant ...md` — market research
  (source input).

## Release tiers used throughout `FEATURES.md`

| Tier | Meaning | Rough timeline (from source docs) |
| --- | --- | --- |
| **MVP** | Smallest thing that delivers the "magic moment" for a paid pilot | 0–3 months |
| **V1** | Product-led growth: self-serve, team plans, polish | 3–9 months |
| **V2** | Move upmarket: admin, analytics, security, enterprise | 9–18 months |
| **Future** | Defensibility / advanced bets | 18 months+ |

## Success metric (the bar the MVP must clear)

> When a prospect raises an objection and the rep clicks **Suggest**, can the tool show
> a **useful, accurate, short, source-backed** suggestion within **2–4 seconds**?
