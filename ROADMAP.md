# Product Roadmap — Real-Time AI Sales-Call Copilot

**A sequential, feature-by-feature build order.** This roadmap takes every feature
from [`FEATURES.md`](./FEATURES.md) and arranges it in the order it should be built,
so engineering can move top-to-bottom. Each **Stage** depends on the ones before it;
**within a stage, steps are also ordered**. Every feature is referenced by its
`FEATURES.md` ID so nothing is lost.

**How to use it:** build the stages in order. Inside a stage, build the steps in
order. A feature never appears before the feature it depends on. Stages 1–13 = the
shippable MVP; Stages 14–22 = growth/V1; Stages 23–29 = upmarket/V2; Stage 30 =
future bets.

**Status key:** `[ ]` not started · `[~]` in progress · `[x]` done.

---

## Stage 0 — Project Foundations
*Why first: nothing can be built or tested without the skeleton.*

- [ ] **0.1** Repo, CI/CD, environments (dev/staging/prod)
- [ ] **0.2** Multi-tenant data architecture — `20.8`
- [ ] **0.3** Vector store + relational store + object store — `20.9`
- [ ] **0.4** Real-time streaming backend (WebSocket/gRPC pipeline) — `20.1`
- [ ] **0.5** Sign-up / auth (email + Google/Microsoft OAuth) — `16.1`
- [ ] **0.6** Tenant data isolation — `15.6`
- [ ] **0.7** Encryption in transit + at rest — `15.1`
- [ ] **0.8** Secrets / key management & rotation — `15.9`
- [ ] **0.9** Latency/telemetry scaffolding (instrument before optimizing) — `18.1`

**Expectation:** a deployable, multi-tenant shell with auth and a streaming backbone.
No user-facing value yet — this is the launchpad.

---

## Stage 1 — Audio Capture (the input)
*Why now: everything downstream needs clean two-sided audio.*

- [~] **1.1** Desktop app shell (macOS + Windows) — `1.1` (macOS built; Windows not yet)
- [~] **1.2** OS permission handling (mic, system-audio) — `1.5` (mic + macOS tap TCC prompt; not device-verified)
- [x] **1.3** Microphone capture (rep) — `1.2`
- [~] **1.4** System/loopback audio capture (prospect) — `1.3` (macOS CoreAudio tap built + wired; Windows WASAPI pending)
- [x] **1.5** Two-stream separation at capture (mic vs. system) — `1.4`
- [x] **1.6** Local-only / "no audio storage" capture mode — `1.12`
- [x] **1.7** Instant pause / mute-listening control — `1.8`
- [~] **1.8** Audio device selection / switching — `1.6` (mic picker done; mid-call hot-swap pending)

**Expectation:** the app reliably captures rep + prospect on separate channels, with
a hard "stop listening" control and no audio persisted. Test on real Zoom/Meet calls.

---

## Stage 2 — Real-Time Transcription (the substrate)
*Why now: text is what every detector and retriever reads.*

- [ ] **2.1** Pluggable STT provider abstraction — `2.10`
- [ ] **2.2** Streaming STT, partial + final transcripts — `2.1`
- [ ] **2.3** Speaker labeling (rep vs. prospect, from the two-stream split) — `2.3`
- [ ] **2.4** End-of-turn / end-of-speech detection — `2.4`
- [ ] **2.5** Punctuation, casing & formatting — `2.5`
- [ ] **2.6** Hit the <400 ms latency target — `2.2`

**Expectation:** a live, speaker-labeled transcript streaming in under 400 ms. This
is the first measurable quality gate.

---

## Stage 3 — Suggestion Trigger (the "Suggest" button)
*Why now: the rep decides WHEN to ask; depends on transcript. Automatic detection removed.*

- [ ] **3.1** Suggest-button trigger (inbound WS click; the only trigger) — `3.1`
- [ ] **3.2** Last-N-minutes transcript window capture (configurable) — `3.2`
- [ ] **3.3** Light-LLM query builder (window → clean search query) — `3.3`
- [ ] **3.4** `SuggestRequest → BuiltQuery` contract handed to retrieval — `3.4`
- [ ] **3.5** Button rate-limit / in-flight guard — `3.11`

**Expectation:** a Suggest click reliably produces a clean, relevant query from the
recent transcript, with double-clicks guarded. There is no auto-detection to tune.
Measure query quality on recorded calls. (Retrieval = Stage 5, the answer = Stage 6.)

---

## Stage 4 — Knowledge Base, minimum (the grounding source)
*Why now: retrieval and generation need approved content to cite.*

- [ ] **4.1** Document upload (PDF/DOCX/PPTX/TXT/MD) — `4.1`
- [ ] **4.2** Manual battlecard / objection-handler entry (structured) — `4.2`
- [ ] **4.3** Automatic chunking + embedding pipeline — `4.5`

**Expectation:** a customer can load their docs + battlecards and they're indexed for
search.

---

## Stage 5 — Retrieval & Grounding (the lookup)
*Why now: turns the Suggest-built query + KB into the right snippet.*

- [ ] **5.1** Vector / semantic search over the KB — `5.1`
- [ ] **5.2** Query refinement / expansion (query already built by Stage 3) — `5.3`
- [ ] **5.3** Retrieval confidence scoring — `5.6`
- [ ] **5.4** Mandatory source citation on every answer — `5.4`
- [ ] **5.5** "Answer only from approved docs" mode (refuse if no source) — `5.5`

**Expectation:** given the Suggest-built query, the system returns the best approved
snippet with a citation — or honestly returns nothing. This is the anti-hallucination core.

---

## Stage 6 — Suggestion Generation (the answer)
*Why now: turns retrieved content into a glanceable card.*

- [ ] **6.1** Pluggable LLM provider abstraction — `18.6`
- [ ] **6.2** Short suggestion card, 1–3 bullets — `6.1`
- [ ] **6.3** Suggested verbatim response / talk-track — `6.2`
- [ ] **6.4** Source citation rendered on the card — `6.3`
- [ ] **6.5** Confidence indicator on the card — `6.4`
- [ ] **6.6** Confidence-gated display (suppress low-confidence cards) — `18.9`
- [ ] **6.7** Hallucination guardrail: refuse / hedge when unsupported — `6.12`
- [ ] **6.8** End-to-end 2–4 s suggestion latency — `6.8`

**Expectation:** a Suggest click produces a short, cited, trustworthy card in
2–4 seconds. **This is the "magic moment."**

---

## Stage 7 — In-Call Overlay (the face)
*Why now: the card must reach the rep without breaking the call.*

- [ ] **7.1** Always-on-top floating overlay widget — `7.1`
- [ ] **7.2** Glanceable, peripheral design — `7.2`
- [ ] **7.3** Draggable / resizable / repositionable window — `7.3`
- [ ] **7.4** Screen-share privacy (overlay not captured) — `7.7`
- [ ] **7.5** Transparent disclosure that AI assistance is in use — `7.7a`
- [ ] **7.6** Keyboard shortcuts / hotkeys — `7.4`
- [ ] **7.7** Manual "Ask" box — `7.5`
- [ ] **7.8** In-call per-card feedback (helpful/not/wrong/too late) — `7.9`
- [ ] **7.9** Graceful degradation if STT/LLM down — `20.5`

**Expectation:** a rep sees private, glanceable cards over any meeting window, can
pull answers manually, and rates each card. The full live loop now works end-to-end.

---

## Stage 8 — Compliance Core (the gate to real calls)
*Why now: HARD GATE — no real prospect call until this exists.*

- [ ] **8.1** In-call consent / disclosure prompt — `14.1`
- [ ] **8.2** Admin toggle to require explicit consent — `14.2`
- [ ] **8.3** "No-recording" mode (process live, store nothing) — `14.3`
- [ ] **8.4** Audio-off-by-default storage setting — `14.4`
- [ ] **8.5** "No training on customer data" default — `14.12`

**Expectation:** the tool can lawfully run on real, consented prospect calls. Do not
skip or reorder this ahead of live customer use.

---

## Stage 9 — Sales-Specific Guidance (the differentiators)
*Why now: turns a generic copilot into a sales copilot — the killer features.*

- [ ] **9.1** Real-time objection-handling cards (killer #1) — `8.1`
- [ ] **9.2** Competitor battlecards — pulled via Suggest (killer #2) — `8.2`
- [ ] **9.3** "Do-not-say" guardrail on generated cards (killer #3) — `6.5` / `8.3`
- [ ] **9.4** Do-not-say rule match on the copilot's output (Guardrail Service) — `3.6`
- [ ] **9.5** Product Q&A answers (cited) — `8.4`
- [ ] **9.6** Pricing / packaging guidance — `8.5`
- [ ] **9.7** Discovery-question prompts — `8.6`
- [ ] **9.8** Stall-line suggestions — `6.7`

**Expectation:** the three headline features work: objection cards and competitor
battlecards pulled via the Suggest button, and the do-not-say guardrail vetting every
generated card — all cited.

---

## Stage 10 — Post-Call Output (value after the call)
*Why now: immediate value even when live cards miss; feeds CRM later.*

- [ ] **10.1** Post-call summary — `9.1`
- [ ] **10.2** Action items / next-steps extraction — `9.2`
- [ ] **10.3** "Unanswered question" capture (content-gap signal) — `9.9`

**Expectation:** every call ends with a usable summary, next steps, and a list of
what the KB couldn't answer.

---

## Stage 11 — Admin & Playbook Authoring (content control)
*Why now: pilots need a way to create/govern the guidance.*

- [ ] **11.1** Admin playbook builder — `12.1`
- [ ] **11.2** Objection → response mapping editor — `12.2`
- [ ] **11.3** Battlecard editor (per competitor) — `12.3`
- [ ] **11.4** "Do-not-say" rules editor — `12.4`

**Expectation:** a sales leader can author and control exactly what reps see live.

---

## Stage 12 — Accounts, Billing & Onboarding (collect revenue)
*Why now: enables the paid pilot — the first real validation.*

- [ ] **12.1** Usage metering (minutes/hours) — `16.7`
- [ ] **12.2** Billing / payments (Stripe) — `16.9`
- [ ] **12.3** Free trial (limited calls/minutes) — `16.2`
- [ ] **12.4** Solo plan ($19–$29/mo) — `16.3`
- [ ] **12.5** <10-minute setup (install → upload → first call) — `17.1`
- [ ] **12.6** Guided doc-upload wizard — `17.2`

**Expectation:** an individual can sign up, onboard in <10 min, and pay.

---

## Stage 13 — MVP Quality Hardening (prove the bar)
*Why now: lock the metrics before adding breadth.*

- [ ] **13.1** Hallucination / groundedness evaluation harness — `18.3`
- [ ] **13.2** Suggestion feedback capture pipeline — `18.2`
- [ ] **13.3** Cost-per-call monitoring & guardrails — `18.8`

> **✅ MVP COMPLETE.** Bar to clear: useful-suggestion ≥50%, wrong-answer <5%, latency
> 2–4 s, adoption ≥60% of eligible calls, 3–5 teams paying $49–$79/seat. Do not start
> Stage 14 until this holds.

---

## Stage 14 — Transcription Depth
*Why now: real-world calls expose accuracy gaps to fix before scaling.*

- [ ] **14.1** Custom vocabulary / keyterm prompting — `2.6`
- [ ] **14.2** Multi-accent / noisy-line robustness — `2.7`
- [ ] **14.3** Noise suppression / echo handling on capture — `1.7`
- [ ] **14.4** Audio buffering & reconnection — `1.11`
- [ ] **14.5** Buying-signal surfacing — **post-call only** (coaching/CRM, Stage 21) — `3.5`
- [ ] **14.6** Discovery-gap surfacing — **post-call only** (coaching, Stage 21) — `3.7`
- [ ] **14.8** Live scrolling transcript view (optional panel) — `2.11`

> **No auto-detection, ever.** The Suggest button (Stage 3) is the only live trigger.
> Signals that once implied live detection (buying-signal, discovery-gap, sentiment,
> talk-ratio) are **post-call analysis** — never a live listener. `3.10` configurable
> trigger phrases is dropped (it only existed to auto-fire).

---

## Stage 15 — Retrieval & Generation Quality
- [ ] **15.1** Hybrid search (semantic + keyword/BM25) — `5.2`
- [ ] **15.2** Re-ranking of retrieved chunks — `5.7`
- [ ] **15.3** Context-overload handling — `5.8`
- [ ] **15.4** Caching of common Q&A / objections — `5.9`
- [ ] **15.5** Streaming card rendering — `6.9`
- [ ] **15.6** Multi-suggestion ranking (best first, alternatives on demand) — `6.11`
- [ ] **15.7** Clarifying / discovery-question suggestions — `6.6`

---

## Stage 16 — Knowledge Ingestion at Scale
*Why now: fast onboarding kills the #1 churn risk.*

- [ ] **16.1** Website / URL ingestion — `4.3`
- [ ] **16.2** Google Drive / Notion connectors — `4.4`
- [ ] **16.3** Document versioning — `4.6`
- [ ] **16.4** Admin approval / publish workflow for content — `4.7`
- [ ] **16.5** Source tagging / categorization — `4.8`
- [ ] **16.6** Bulk import & de-duplication — `4.12`
- [ ] **16.7** Freshness / version-aware retrieval — `5.10`

---

## Stage 17 — Overlay & Guidance Polish
- [ ] **17.1** Card dismissal / snooze / pin — `7.6`
- [ ] **17.2** Minimal "focus mode" (one card) — `7.11`
- [ ] **17.3** Dark/light theme & font-size controls — `7.8`
- [ ] **17.4** Live objection/competitor "ticker" — `7.10`
- [ ] **17.5** Mock-call / practice mode — `7.12`
- [ ] **17.6** Case-study / proof-point surfacing — `8.7`
- [ ] **17.7** Methodology overlays (MEDDICC/BANT/SPICED/SPIN) — `8.8`
- [ ] **17.8** Next-best-action suggestions — `8.9`
- [ ] **17.9** Founder/expert "best answer" library — `8.10`

---

## Stage 18 — Team Plans & Access Control
*Why now: convert individual usage into team rollouts.*

- [ ] **18.1** Team & role management (admin/manager/rep) — `12.6`
- [ ] **18.2** Role-based access control (RBAC) — `15.4`
- [ ] **18.3** Pro plan ($49/mo) — `16.4`
- [ ] **18.4** Team plan ($69–$89/user/mo) — `16.5`
- [ ] **18.5** Seat management / team invites — `16.11`
- [ ] **18.6** Fair-use limits & overage handling — `16.8`
- [ ] **18.7** Hybrid base + usage pricing option — `16.10`
- [ ] **18.8** Plan upgrade/downgrade & self-serve checkout — `16.12`
- [ ] **18.9** Shared vs. private playbooks — `12.9`
- [ ] ~~**18.10** Trigger/tracker configuration (admin) — `12.5`~~ *(dropped — no auto-fire; the rep triggers via Suggest)*
- [ ] **18.11** Methodology configuration — `12.10`

---

## Stage 19 — CRM & Calendar Integration (stickiness)
- [ ] **19.1** HubSpot integration — `10.1`
- [ ] **19.2** Salesforce integration — `10.2`
- [ ] **19.3** Automatic post-call CRM write-back — `10.3`
- [ ] **19.4** CRM-ready notes / field suggestions — `9.4`
- [ ] **19.5** Follow-up email draft — `9.3`
- [ ] **19.6** Objections-encountered log — `9.5`
- [ ] **19.7** Full searchable transcript + recording (opt-in) — `9.6`
- [ ] **19.8** Calendar integration (Google/Outlook) for auto call-start — `10.5`

---

## Stage 20 — Manager Analytics
- [ ] **20.1** Per-rep & team usage dashboard — `13.1`
- [ ] **20.2** Suggestion-usefulness analytics — `13.3`
- [ ] **20.3** Content-gap report — `13.6`

---

## Stage 21 — ★ Per-Employee Performance & Coaching Engine (§13A), core
*Why now: the team-wide coaching moat; depends on post-call + analytics.
**Consent + registry land BEFORE any scoring.***

- [ ] **21.1** Employee consent / transparency & monitoring-notice controls — `13A.19`
- [ ] **21.2** Privacy guardrails (rep sees own data; configurable visibility) — `13A.20`
- [ ] **21.3** Employee registry / roster — `13A.1`
- [ ] **21.4** Auto rep-identification per call — `13A.2`
- [ ] **21.5** Scored performance rubric / dimensions — `13A.4`
- [ ] **21.6** Per-call "what you did well / what you did poorly" summary — `13A.3`
- [ ] **21.7** Evidence-linked feedback (cites transcript moments) — `13A.6`
- [ ] **21.8** Longitudinal performance tracking per employee — `13A.7`
- [ ] **21.9** Personalized improvement plan — `13A.8`
- [ ] **21.10** Strengths reinforcement ("keep doing this") — `13A.9`
- [ ] **21.11** Rep self-view dashboard — `13A.11`
- [ ] **21.12** Manager view (per-rep & team strengths/gaps) — `13A.12`

**Expectation:** every registered, informed rep gets a scored, evidence-backed
review after every call, plus an improvement plan and strengths — tracked over time.

---

## Stage 22 — Trust & Data Controls (V1 close-out)
- [ ] **22.1** Configurable data-retention controls — `14.5`
- [ ] **22.2** Delete-on-request (per call / per contact) — `14.6`
- [ ] **22.3** Consent logs visible to admins — `14.7`
- [ ] **22.4** Two-party-consent-aware defaults by location — `14.10`
- [ ] **22.5** Configurable meeting-notice banner / spoken disclosure — `14.13`
- [ ] **22.6** Eval suite of recorded calls / synthetic objections — `18.4`
- [ ] **22.7** Prompt & model versioning + rollback — `18.7`
- [ ] **22.8** Auto-updating desktop client — `20.2`
- [ ] **22.9** Crash reporting & telemetry — `20.3`
- [ ] **22.10** Observability / SLO dashboards — `20.6`
- [ ] **22.11** Horizontal scalability of STT/LLM workers — `20.4`
- [ ] **22.12** Browser extension (Chrome/Edge) for Meet captions — `1.9`
- [ ] **22.13** Platform-policy-change handling (bot detection/consent) — `11.6`
- [ ] **22.14** Offline / poor-network resilience — `20.7`
- [ ] **22.15** Feature flags / staged rollout — `20.10`

> **✅ V1 COMPLETE.** Bar to clear: ≥20% team trial→paid; CRM live with reference
> customers; §13A in daily use and *not* perceived as surveillance.

---

## Stage 23 — Microsoft & Platform Expansion (V2 start)
- [ ] **23.1** Microsoft Teams support — `11.3`
- [ ] **23.2** Native marketplace listings (Zoom / Google Workspace) — `11.5`
- [ ] **23.3** Public API + webhooks — `10.9`
- [ ] **23.4** Slack / Teams notifications — `10.6`
- [ ] **23.5** Email integration (send follow-ups) — `10.7`
- [ ] **23.6** Deal/account context pull from CRM into the call — `10.4`

---

## Stage 24 — Enterprise Security & Compliance
- [ ] **24.1** SSO / SAML / OIDC — `15.2`
- [ ] **24.2** Audit logs (access/exports/deletions) — `15.3`
- [ ] **24.3** Region / data-residency controls (EU/India) — `14.9`
- [ ] **24.4** DPA (Data Processing Agreement) support — `14.11`
- [ ] **24.5** Live PII redaction in transcript — `2.8`
- [ ] **24.6** PII detection & redaction at rest — `15.7`
- [ ] **24.7** PII/sensitive-data filtering before LLM calls — `18.11`
- [ ] **24.8** Penetration testing & vulnerability management — `15.10`
- [ ] **24.9** SOC 2 Type II — `15.5`

---

## Stage 25 — Knowledge & Guidance Depth (V2)
- [ ] **25.1** Per-team / per-product knowledge spaces — `4.9`
- [ ] **25.2** CRM-note & past-call ingestion (deal context) — `4.10`
- [ ] **25.3** Stale-content detection & re-index alerts — `4.11`
- [ ] **25.4** Compliance / security-questionnaire answers — `8.11`
- [ ] **25.5** Call-stage awareness (discovery/demo/negotiation) — `8.13`
- [ ] **25.6** Tone/style adaptation to rep & brand voice — `6.10`
- [ ] **25.7** Sentiment / tone-shift detection — `3.8`
- [ ] **25.8** Talk-ratio / monologue / interruption detection — `3.9`

---

## Stage 26 — ★ Per-Employee Engine, advanced (§13A V2)
- [ ] **26.1** Configurable rubric (dimensions, weights, methodology) — `13A.5`
- [ ] **26.2** Skill/competency progress tracking — `13A.10`
- [ ] **26.3** Benchmarking vs. team & top performers — `13A.13`
- [ ] **26.4** Regression / struggle alerts — `13A.14`
- [ ] **26.5** Micro-learning / coaching nudges — `13A.15`
- [ ] **26.6** Goal setting & review cadence (auto coaching reports) — `13A.17`
- [ ] **26.7** Ramp-tracking for new hires — `13A.18`
- [ ] **26.8** Aggregated team-skill report — `13A.21`

---

## Stage 27 — Advanced Manager Analytics (V2)
- [ ] **27.1** Objection-frequency & win/loss correlation — `13.2`
- [ ] **27.2** Ramp-time tracking for new reps — `13.4`
- [ ] **27.3** Call scorecards (methodology adherence) — `13.5`
- [ ] **27.4** Manager call review with cards-shown timeline — `13.7`
- [ ] **27.5** Rep self-scorecard / call review — `9.7`
- [ ] **27.6** Conversion-lift / ROI reporting — `13.9`

---

## Stage 28 — Enterprise Plan & Outcome Learning (V2 close-out)
- [ ] **28.1** Business / Enterprise plan ($120+/user/mo or custom) — `16.6`
- [ ] **28.2** Outcome-learning loop (suggestions used → calls converted) — `18.5` / `19.2`
- [ ] **28.3** Approved-knowledge accuracy moat (versioned/approved/cited) — `19.1`
- [ ] **28.4** Trust & compliance posture as a product — `19.5`
- [ ] **28.5** Distribution integrations (marketplaces, RevOps partners) — `19.7`
- [ ] **28.6** Accessibility (WCAG) for overlay & dashboards — `20.11`

> **✅ V2 COMPLETE.** Bar to clear: SOC 2 achieved; enterprise logos; documented
> ROI/ramp proof; conversion lift trending into the 15–28% range.

---

## Stage 29 — Sales-Engagement & Automation Integrations (V2/Future bridge)
- [ ] **29.1** Sales-engagement tool hooks (Outreach/Salesloft) — `10.8`
- [ ] **29.2** Zapier / Make connectors — `10.10`
- [ ] **29.3** Slack / Teams summary delivery refinements — (extends `10.6`)

---

## Stage 30 — Future Bets (evidence-gated, not pre-committed)
- [ ] **30.1** Per-vertical templates (SaaS/insurance/real estate/edtech) — `8.12`
- [ ] **30.2** Vertical specialization packs — `19.4`
- [ ] **30.3** Per-customer model/prompt tuning from approved content — `18.10`
- [ ] **30.4** Best-call exemplars (own & teammates' great moments) — `13A.16`
- [ ] **30.5** Optional screen / slide capture (OCR of shared deck) — `1.10`
- [ ] **30.6** Multi-language transcription — `2.9`
- [ ] **30.7** Phone / dialer call support (PSTN/VoIP) — `11.7`
- [ ] **30.8** Highlight reel / key-moment clips — `9.8`
- [ ] **30.9** A/B testing of talk-tracks / cards — `12.8`
- [ ] **30.10** Leaderboards / benchmarking — `13.8`
- [ ] **30.11** Private / VPC / self-hosted deployment — `15.8`
- [ ] **30.12** Linux desktop client — `20.12`

---

## The build sequence at a glance

```
0 Foundations
        │
1 Capture ─► 2 Transcription ─► 3 Trigger (Suggest) ─► 4 Knowledge ─► 5 Retrieval ─► 6 Generation ─► 7 Overlay
                                                                                                  │
                                                            8 Compliance (GATE: real calls) ◄─────┘
                                                                                                  │
                          9 Sales guidance ─► 10 Post-call ─► 11 Admin ─► 12 Billing ─► 13 Hardening
                                                                                                  │
                                                                                       ✅ MVP COMPLETE
                                                                                                  │
14 STT depth ─► 15 RAG quality ─► 16 Ingestion ─► 17 Polish ─► 18 Teams ─► 19 CRM ─► 20 Analytics ─►
                                                21 Per-Employee Engine (consent→registry→scoring) ─► 22 Trust
                                                                                                  │
                                                                                       ✅ V1 COMPLETE
                                                                                                  │
23 Platforms ─► 24 Enterprise security ─► 25 Depth ─► 26 §13A advanced ─► 27 Analytics+ ─► 28 Enterprise
                                                                                                  │
                                                                                       ✅ V2 COMPLETE
                                                                                                  │
                                                                       29 Automation ─► 30 Future bets
```

**Two ordering rules you must not break:**
1. **Stage 8 (Compliance) before any real prospect call.**
2. **Stage 21 consent + registry (21.1–21.4) before any employee scoring (21.5+).**

Everything else follows the dependency chain above: capture → text → trigger (Suggest) →
knowledge → retrieve → generate → display, then breadth.
