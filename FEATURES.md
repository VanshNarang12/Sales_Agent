# Feature Analysis — Real-Time AI Sales-Call Copilot

> **Purpose of this document.** A complete, prioritized inventory of every feature
> required to build the product described in `Verdict.md` and the market-research
> file. Features are grouped by capability area. Each feature has a **release tier**
> (MVP / V1 / V2 / Future) and a one-line rationale. This is deliberately
> exhaustive — but every feature is tied to a real need from the source documents
> or from researched competitors (LivePitchAI, Nomi, Aircover, Gong, Outreach Kaia,
> Balto, Cresta, Cluely, Clari Copilot, CallCopilot, Microsoft Copilot for Sales,
> Zoom Revenue Accelerator).

**Tiers:** **MVP** (0–3 mo, paid pilot) · **V1** (3–9 mo, product-led growth) ·
**V2** (9–18 mo, upmarket) · **Future** (18 mo+, moat/bets).

**Legend for priority:** 🔴 must-have · 🟠 should-have · 🟢 nice-to-have.

---

## Table of contents

1. [Audio & Screen Capture](#1-audio--screen-capture)
2. [Real-Time Transcription & Diarization](#2-real-time-transcription--diarization)
3. [Intent, Objection & Signal Detection](#3-intent-objection--signal-detection)
4. [Knowledge Base & Document Ingestion](#4-knowledge-base--document-ingestion)
5. [Retrieval & Grounding (RAG)](#5-retrieval--grounding-rag)
6. [Real-Time Suggestion Generation](#6-real-time-suggestion-generation)
7. [In-Call Overlay UX](#7-in-call-overlay-ux)
8. [Sales-Specific Guidance Features](#8-sales-specific-guidance-features)
9. [Post-Call Features](#9-post-call-features)
10. [CRM & Tool Integrations](#10-crm--tool-integrations)
11. [Meeting-Platform Integrations](#11-meeting-platform-integrations)
12. [Admin, Playbook & Content Management](#12-admin-playbook--content-management)
13. [Manager Analytics & Coaching](#13-manager-analytics--coaching)
13A. [Per-Employee Performance & Coaching Engine](#13a-per-employee-performance--coaching-engine)
14. [Consent & Compliance](#14-consent--compliance)
15. [Security & Privacy](#15-security--privacy)
16. [Accounts, Billing & Pricing](#16-accounts-billing--pricing)
17. [Onboarding & Activation](#17-onboarding--activation)
18. [AI Quality, Latency & Trust Infrastructure](#18-ai-quality-latency--trust-infrastructure)
19. [Defensibility / Moat Features](#19-defensibility--moat-features)
20. [Platform, Infrastructure & Non-Functional](#20-platform-infrastructure--non-functional)
21. [Feature summary by release tier](#21-feature-summary-by-release-tier)

---

## 1. Audio & Screen Capture

The foundation: get clean audio of both the rep and the prospect into the pipeline.

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 1.1 | **Desktop app (macOS + Windows)** as the primary capture client | MVP | 🔴 | Desktop app gives the most control across tabs/apps and any meeting platform; the docs recommend it over a bot/extension. |
| 1.2 | **Microphone capture** (the rep's voice) | MVP | 🔴 | Half of the conversation; needed for speaker context. |
| 1.3 | **System/loopback audio capture** (the prospect's voice) | MVP | 🔴 | Required to hear the buyer. macOS via CoreAudio Tap API; Windows via WASAPI loopback. |
| 1.4 | **Two-stream separation at capture** (mic vs. system) | MVP | 🔴 | Cheap, reliable speaker separation without ML diarization — rep audio and prospect audio arrive on separate channels. |
| 1.5 | **OS permission handling** (mic, screen/system-audio recording prompts) | MVP | 🔴 | macOS/Windows require explicit grants; must guide the user through them gracefully. |
| 1.6 | **Audio device selection / switching** (headset, AirPods, external interface) | MVP | 🟠 | Reps change devices mid-day; capture must follow the active device. |
| 1.7 | **Noise suppression / echo handling on capture** | V1 | 🟠 | Improves transcription accuracy on noisy lines; reduces double-capture echo. |
| 1.8 | **Instant pause / mute-listening control** | MVP | 🔴 | Compliance + trust: rep can stop capture instantly (e.g., off-topic, personal). |
| 1.9 | **Browser extension (Chrome/Edge) as a lightweight alternative** for Google Meet captions | V1 | 🟢 | Reads native captions; lower-friction entry for Meet-only users (note ToS constraints). |
| 1.10 | **Optional screen / slide capture (OCR of shared deck)** | Future | 🟢 | Context-aware suggestions tied to the slide being shown; only after audio path is solid. |
| 1.11 | **Audio buffering & reconnection** (network blips, device changes) | V1 | 🟠 | Avoid losing the conversation when Wi-Fi hiccups or device switches. |
| 1.12 | **Local-only / "no audio storage" capture mode** | MVP | 🔴 | Process the live stream without persisting raw audio — a key compliance & trust feature. |

---

## 2. Real-Time Transcription & Diarization

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 2.1 | **Streaming speech-to-text** (partial + final transcripts) | MVP | 🔴 | The substrate for everything else; needs WebSocket streaming STT. |
| 2.2 | **Sub-400 ms transcription latency target** | MVP | 🔴 | Research: ~300 ms is the conversational threshold; >400 ms and prompts arrive late. ElevenLabs Scribe v2 (<150 ms), AssemblyAI Universal-3 (~307 ms P50), Deepgram Nova-3 are candidates. |
| 2.3 | **Speaker labeling (rep vs. prospect)** | MVP | 🔴 | Driven primarily by the two-stream split (1.4); ML diarization as a fallback for single-stream sources. |
| 2.4 | **End-of-turn / end-of-speech detection** | MVP | 🔴 | Lets the system fire suggestions at natural pauses, not mid-sentence. |
| 2.5 | **Punctuation, casing & formatting** | MVP | 🟠 | Readable transcript for the post-call summary and for RAG quality. |
| 2.6 | **Custom vocabulary / keyterm prompting** (product names, competitors, acronyms) | V1 | 🔴 | Sales calls are full of proper nouns STT mishears; boosting these directly improves objection/competitor detection. |
| 2.7 | **Multi-accent / noisy-line robustness** | V1 | 🟠 | Real calls have accents, crosstalk, and bad connections. |
| 2.8 | **Live PII redaction in transcript** (cards, SSNs, etc.) | V2 | 🟠 | Reduces sensitive-data exposure; required for regulated verticals. |
| 2.9 | **Multi-language transcription** (beyond English) | Future | 🟢 | Docs: do not build before English is excellent. |
| 2.10 | **Pluggable STT provider abstraction** | MVP | 🟠 | Avoid vendor lock-in; swap Deepgram/AssemblyAI/Gladia/ElevenLabs on cost/latency. |
| 2.11 | **Live scrolling transcript view** (optional panel) | V1 | 🟢 | Some reps want to glance at the running transcript. |

---

## 3. Intent, Objection & Signal Detection

The "trigger" layer that decides *when* to surface a card.

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 3.1 | **Objection detection** ("too expensive," "not now," "send info," "need approval," "already use a competitor") | MVP | 🔴 | The #1 killer use case — buyer refusal is where deals die. |
| 3.2 | **Buyer-question detection** (product Q&A, integration, pricing, security) | MVP | 🔴 | Triggers document-grounded answers — the core LivePitchAI/Aircover behavior. |
| 3.3 | **Competitor-mention detection** | MVP | 🔴 | Fires the live battlecard (killer feature #2); Nomi's headline behavior. |
| 3.4 | **Pricing / discount-request detection** | MVP | 🔴 | High-risk moment; pairs with "do-not-say" guardrails. |
| 3.5 | **Buying-signal detection** (timeline, budget, authority, urgency cues) | V1 | 🟠 | Surfaces next-best-action and feeds CRM/deal fields. |
| 3.6 | **Risk / red-flag detection** (legal/security/compliance promises about to be made) | V1 | 🔴 | Powers the "do-not-say" guardrail (killer feature #3). |
| 3.7 | **Discovery-gap detection** (methodology fields not yet covered) | V1 | 🟠 | Prompts junior reps to ask the right discovery question. |
| 3.8 | **Sentiment / tone shift detection** | V2 | 🟢 | Nomi adapts prompts to tone & price pressure; useful but secondary. |
| 3.9 | **Talk-ratio / monologue / interruption detection** | V2 | 🟢 | Live nudge: "you've been talking for 3 min — ask a question." |
| 3.10 | **Configurable trigger phrases / trackers** (admin-defined keywords) | V1 | 🟠 | Gong-style "smart trackers"; lets teams define their own moments. |
| 3.11 | **Suggestion-throttling / relevance gating** | MVP | 🔴 | Too many prompts make reps worse — must suppress low-confidence/low-value triggers. |

---

## 4. Knowledge Base & Document Ingestion

The grounding source — what makes answers "approved," not generic.

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 4.1 | **Document upload** (PDF, DOCX, PPTX, TXT, MD) | MVP | 🔴 | Pitch decks, pricing sheets, FAQs, case studies, battlecards. |
| 4.2 | **Manual battlecard / objection-handler entry** (structured) | MVP | 🔴 | The highest-value content; should be first-class, not just a doc. |
| 4.3 | **Website / URL ingestion** (crawl pricing pages, docs sites) | V1 | 🟠 | Fast onboarding; docs cite painful setup as a churn risk. |
| 4.4 | **Google Drive / Notion connectors** | V1 | 🔴 | Docs explicitly call out fast import from Drive/Notion to avoid onboarding pain. |
| 4.5 | **Automatic chunking + embedding pipeline** | MVP | 🔴 | Semantic search foundation (LivePitchAI indexes everything via semantic search). |
| 4.6 | **Document versioning** | V1 | 🟠 | Pricing/positioning change; answers must reflect the current version. |
| 4.7 | **Admin approval / publish workflow for content** | V1 | 🔴 | "Approved knowledge" is the moat — only vetted content can be cited live. |
| 4.8 | **Source tagging / categorization** (pricing, security, competitor, case study) | V1 | 🟠 | Improves retrieval precision and lets cards label their source type. |
| 4.9 | **Per-team / per-product knowledge spaces** | V2 | 🟠 | Larger orgs sell multiple products to multiple segments. |
| 4.10 | **CRM-note & past-call ingestion** (deal context) | V2 | 🟢 | Personalize suggestions with account history. |
| 4.11 | **Stale-content detection & re-index alerts** | V2 | 🟢 | Flag docs that haven't been updated; keep the "brain" fresh. |
| 4.12 | **Bulk import & de-duplication** | V1 | 🟢 | Teams upload overlapping docs; dedupe keeps retrieval clean. |

---

## 5. Retrieval & Grounding (RAG)

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 5.1 | **Vector / semantic search over the KB** | MVP | 🔴 | Core retrieval for relevant snippets. |
| 5.2 | **Hybrid search (semantic + keyword/BM25)** | V1 | 🟠 | Proper-noun-heavy queries (product/competitor names) need keyword precision. |
| 5.3 | **Conversation-aware query construction** (use recent transcript as context) | MVP | 🔴 | The query is the buyer's actual words + deal context, not a typed prompt. |
| 5.4 | **Mandatory source citation on every answer** | MVP | 🔴 | The anti-hallucination cornerstone — "every answer cites the source." |
| 5.5 | **"Answer only from approved docs" mode** (refuse if no source) | MVP | 🔴 | LivePitchAI's promise: no source → no confident answer. Critical for pricing/security. |
| 5.6 | **Retrieval confidence scoring** | MVP | 🔴 | Drives whether to show a card and how strongly to phrase it. |
| 5.7 | **Re-ranking of retrieved chunks** | V1 | 🟠 | Picks the single best snippet to keep cards short and correct. |
| 5.8 | **Context-overload handling** (choose the right doc, ignore noise) | V1 | 🔴 | Named as a hard technical problem in the docs. |
| 5.9 | **Caching of common Q&A / objections** | V1 | 🟠 | Latency + cost win for the most frequent moments. |
| 5.10 | **Freshness / version-aware retrieval** (prefer current pricing) | V2 | 🟠 | Never cite a deprecated price or claim. |

---

## 6. Real-Time Suggestion Generation

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 6.1 | **Short "suggestion card" generation (1–3 bullets)** | MVP | 🔴 | Reps can't read paragraphs mid-call; cue cards, not ChatGPT essays. |
| 6.2 | **Suggested verbatim response / talk-track** | MVP | 🔴 | "What to say now" — the core deliverable. |
| 6.3 | **Source citation rendered on the card** | MVP | 🔴 | Trust + lets the rep verify at a glance. |
| 6.4 | **Confidence indicator on the card** | MVP | 🟠 | Tells the rep how much to trust it (esp. pricing/security). |
| 6.5 | **"Do-not-say" / guardrail warnings** | MVP | 🔴 | Killer feature #3: warn before over-promising on legal/pricing/security/timeline. |
| 6.6 | **Clarifying / discovery-question suggestions** | MVP | 🟠 | Helps junior reps ask rather than pitch. |
| 6.7 | **Stall-line suggestions** ("Great question — let me get you the precise answer") | MVP | 🟠 | Buys the rep time gracefully; explicitly requested in the docs. |
| 6.8 | **Sub-2–4-second end-to-end suggestion latency** | MVP | 🔴 | The product's pass/fail metric per the docs. |
| 6.9 | **Streaming card rendering** (show first bullet as it generates) | V1 | 🟠 | Perceived latency reduction. |
| 6.10 | **Tone/style adaptation to the rep & brand voice** | V2 | 🟢 | Suggestions sound like the rep, not a robot. |
| 6.11 | **Multi-suggestion ranking** (best card first, alternatives on demand) | V1 | 🟢 | Avoid overwhelming; let the rep expand if needed. |
| 6.12 | **Hallucination guardrail: refuse / hedge when unsupported** | MVP | 🔴 | "Wrong pricing/security answers can kill deals." |

---

## 7. In-Call Overlay UX

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 7.1 | **Always-on-top floating overlay widget** | MVP | 🔴 | Suggestions must appear above any meeting window/tab. |
| 7.2 | **Glanceable, peripheral design** (not a wall of text) | MVP | 🔴 | Research: split attention is the #1 UX failure of live overlays. |
| 7.3 | **Draggable / resizable / repositionable window** | MVP | 🟠 | Reps place it where it doesn't block faces. |
| 7.4 | **Keyboard shortcuts / hotkeys** ("Answer this," "Objection," "Discovery Q," "Stall line," "Summarize concern") | MVP | 🔴 | Hands-on-keyboard control without breaking eye contact. |
| 7.5 | **Manual "Ask" box** (rep types/clicks a question) | MVP | 🟠 | When auto-detection misses, the rep can pull an answer. |
| 7.6 | **Card dismissal / snooze / pin** | V1 | 🟠 | Manage clutter; keep one card visible. |
| 7.7 | **Privacy: overlay not captured by screen-share** (`setContentProtection` / WDA_EXCLUDEFROMCAPTURE / NSWindowSharingNone) | MVP | 🔴 | The rep's coaching is private — visible only to the rep, *not* the prospect on a shared screen. (This is privacy-for-the-rep, **not** covert capture of the prospect.) |
| 7.7a | **Transparent disclosure that AI assistance is in use** (paired with 7.7) | MVP | 🔴 | Distinguishes us from "covert cheat" tools — the *assistance* is private but its *existence* is disclosed. |
| 7.8 | **Dark/light theme & font-size controls** | V1 | 🟢 | Readability in different lighting / on shared screens. |
| 7.9 | **In-call inline feedback on each card** (helpful / not helpful / wrong / too late) | MVP | 🔴 | Powers the success metrics and the outcome-learning loop. |
| 7.10 | **Live objection/competitor "ticker"** of detected moments | V1 | 🟢 | Lightweight awareness of what the AI is tracking. |
| 7.11 | **Minimal "focus mode"** (one card, nothing else) | V1 | 🟠 | For reps who find overlays distracting. |
| 7.12 | **Mock-call / practice mode** | V1 | 🟠 | Reps trial the tool risk-free; aids onboarding & buyer demos. |

---

## 8. Sales-Specific Guidance Features

What makes this a *sales* copilot, not a generic LLM overlay (a core moat per the docs).

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 8.1 | **Real-time objection-handling cards** | MVP | 🔴 | **Killer feature #1.** |
| 8.2 | **Live competitor battlecards** | MVP | 🔴 | **Killer feature #2** — appear when a competitor is named. |
| 8.3 | **"Do-not-say" compliance guardrails** | MVP | 🔴 | **Killer feature #3.** |
| 8.4 | **Product Q&A answers (cited)** | MVP | 🔴 | Reps don't know every technical detail. |
| 8.5 | **Pricing / packaging guidance** | MVP | 🔴 | Prevents wrong numbers / overpromising. |
| 8.6 | **Discovery-question prompts** | MVP | 🟠 | Helps reps qualify before pitching. |
| 8.7 | **Case-study / proof-point surfacing** | V1 | 🟠 | "ACME reduced ramp time 30%" at the moment of doubt. |
| 8.8 | **Methodology overlays (MEDDICC / BANT / SPICED / SPIN)** | V1 | 🟠 | Live checklist of which qualification fields are still open. |
| 8.9 | **Next-best-action suggestions** | V1 | 🟠 | "Propose a pilot," "loop in a SE," "send the security packet." |
| 8.10 | **Founder/expert "best answer" library** | V1 | 🟠 | The "founder's sales brain on every call" wedge. |
| 8.11 | **Compliance / security-questionnaire answers** | V2 | 🟠 | Strong fit for technical & regulated sales (Aircover's Virtual SE). |
| 8.12 | **Per-vertical templates** (SaaS, insurance, real estate, edtech, agencies) | Future | 🟢 | Vertical specialization is a named moat. |
| 8.13 | **Call-stage awareness** (discovery vs. demo vs. negotiation) | V2 | 🟢 | Tailors guidance to where the call is. |

---

## 9. Post-Call Features

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 9.1 | **Post-call summary** (key points, objections raised, outcomes) | MVP | 🔴 | Listed as must-have MVP; immediate value even when live cards miss. |
| 9.2 | **Action items / next steps extraction** | MVP | 🔴 | The most-used post-call artifact. |
| 9.3 | **Follow-up email draft** | V1 | 🔴 | Aircover/Gong staple; huge time-saver. |
| 9.4 | **CRM-ready notes / field suggestions** | V1 | 🔴 | Feeds the CRM sync; reduces admin. |
| 9.5 | **Objections-encountered log** (per call & aggregated) | V1 | 🟠 | Feeds coaching and content gaps. |
| 9.6 | **Full searchable transcript + recording (opt-in)** | V1 | 🟠 | Reference & coaching; gated by consent settings. |
| 9.7 | **Rep self-scorecard / call review** | V2 | 🟢 | Self-coaching; methodology adherence. |
| 9.8 | **Highlight reel / key-moment clips** | Future | 🟢 | Gong-style snippets for coaching/sharing. |
| 9.9 | **"Unanswered question" capture** (what the KB couldn't answer) | V1 | 🔴 | Direct content-gap signal — tells admins what to add. |

---

## 10. CRM & Tool Integrations

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 10.1 | **HubSpot integration** (sync notes, next steps, deal fields) | V1 | 🔴 | Half of the ICP's CRM; named explicitly. |
| 10.2 | **Salesforce integration** | V1 | 🔴 | The other half of the ICP. |
| 10.3 | **Automatic post-call CRM write-back** | V1 | 🔴 | "Automatic CRM updates" is table stakes (Aircover/Gong/Nomi). |
| 10.4 | **Deal/account context pull from CRM into the call** | V2 | 🟠 | Personalizes live suggestions with deal stage & history. |
| 10.5 | **Calendar integration** (Google/Outlook) for auto call-start | V1 | 🟠 | Auto-launch the copilot when a sales meeting begins. |
| 10.6 | **Slack / Teams notifications** (summaries, alerts) | V2 | 🟢 | Push summaries & coaching nudges where teams live. |
| 10.7 | **Email integration** (send follow-ups directly) | V2 | 🟢 | Close the loop on the drafted email. |
| 10.8 | **Sales-engagement tool hooks** (Outreach/Salesloft) | Future | 🟢 | Sequence enrollment from call outcomes. |
| 10.9 | **Public API + webhooks** | V2 | 🟠 | Customer/partner extensibility; ecosystem play. |
| 10.10 | **Zapier / Make connectors** | Future | 🟢 | Long-tail integrations without custom builds. |

---

## 11. Meeting-Platform Integrations

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 11.1 | **Zoom support** (via desktop audio capture) | MVP | 🔴 | One of the two must-have platforms. |
| 11.2 | **Google Meet support** | MVP | 🔴 | The other must-have; a key wedge ("Google Meet sales copilot"). |
| 11.3 | **Microsoft Teams support** | V1 | 🟠 | Add for Microsoft-heavy orgs; not required day one. |
| 11.4 | **Platform-agnostic capture** (works on any app via system audio) | MVP | 🔴 | Desktop audio capture means dialers/phone/web calls work too. |
| 11.5 | **Native marketplace listings** (Zoom App Marketplace, Google Workspace Marketplace) | V2 | 🟢 | Distribution channel — must be policy-compliant. |
| 11.6 | **Graceful handling of platform policy changes** (bot detection, consent prompts) | V1 | 🔴 | Research: Zoom/Meet/Teams are tightening bot & capture controls in 2025–26. |
| 11.7 | **Phone / dialer call support** (PSTN/VoIP) | Future | 🟢 | Opens inside-sales / contact-center adjacency. |

---

## 12. Admin, Playbook & Content Management

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 12.1 | **Admin playbook builder** (control what guidance reps see) | MVP | 🔴 | Sales leaders must govern the live guidance — a named must-have. |
| 12.2 | **Objection → response mapping editor** | MVP | 🔴 | The core authoring surface for killer feature #1. |
| 12.3 | **Battlecard editor** (per competitor) | MVP | 🔴 | Authoring for killer feature #2. |
| 12.4 | **"Do-not-say" rules editor** | MVP | 🔴 | Authoring for killer feature #3. |
| 12.5 | **Trigger/tracker configuration** (keywords → cards) | V1 | 🟠 | Lets teams define their own live moments. |
| 12.6 | **Team & role management** (admin/manager/rep) | V1 | 🔴 | Needed for team plans & permissions. |
| 12.7 | **Content approval workflow** | V1 | 🔴 | Only approved content goes live (the accuracy moat). |
| 12.8 | **A/B testing of talk-tracks / cards** | Future | 🟢 | Learn which phrasing converts. |
| 12.9 | **Shared vs. private playbooks** | V1 | 🟠 | Balance individual customization with team consistency. |
| 12.10 | **Methodology configuration** (pick MEDDICC/BANT/etc.) | V1 | 🟢 | Aligns prompts to the team's framework. |

---

## 13. Manager Analytics & Coaching

Managers want visibility; reps may want privacy — the docs flag this tension.

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 13.1 | **Per-rep & team usage dashboard** (adoption, calls assisted) | V1 | 🔴 | VP Sales asks for rollout visibility; key to expansion revenue. |
| 13.2 | **Objection-frequency & win/loss correlation** | V2 | 🟠 | Which objections kill deals → content & coaching priorities. |
| 13.3 | **Suggestion-usefulness analytics** (from in-call feedback) | V1 | 🔴 | Proves value; tunes the model; the pilot success metric. |
| 13.4 | **Ramp-time tracking for new reps** | V2 | 🟠 | The "75% faster ramp" outcome story for upmarket selling. |
| 13.5 | **Call scorecards (methodology adherence)** | V2 | 🟠 | Gong/Clari-style coaching scorecards. |
| 13.6 | **Content-gap report** (questions the KB couldn't answer) | V1 | 🔴 | Closes the loop with content management (9.9). |
| 13.7 | **Manager call review with cards-shown timeline** | V2 | 🟢 | See what the rep was offered vs. what they did. |
| 13.8 | **Leaderboards / benchmarking** | Future | 🟢 | Motivation; comparative performance. |
| 13.9 | **Conversion-lift / ROI reporting** | V2 | 🟠 | Needed to justify renewal & upmarket pricing. |

---

## 13A. Per-Employee Performance & Coaching Engine

**The capability.** A company registers **all of its employees (reps)** in the
product. From then on, **every call** each rep takes automatically produces a
personalized performance review — *what they did well* and *what they did poorly* —
scored against a consistent rubric. Performance is **monitored over time**, and the
product tells each rep **how to improve** and **what to keep doing**, turning every
call into coaching and building a longitudinal development record per employee.

> This extends the post-call summary (§9) and manager analytics (§13) into a
> dedicated, per-person development loop — effectively an always-on AI sales coach
> for every employee.

> **⚠️ Trust & consent note.** This is employee monitoring, which carries its own
> legal/ethical weight (worker-monitoring laws, works-council/EU rules, morale).
> It must ship with transparency to the employee, clear opt-in/notice, and the
> consent/compliance controls in §14. Frame it as *development*, not surveillance —
> visible to the rep, not a black-box ranking.

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 13A.1 | **Employee registry / roster** — company registers all reps (name, role, team, manager, tenure, ramp stage) | V1 | 🔴 | The entry point: link every call to a known employee. |
| 13A.2 | **Auto rep-identification per call** (map the call's "rep" stream to a registered employee) | V1 | 🔴 | Summaries must attach to the right person automatically. |
| 13A.3 | **Per-call performance summary: "what you did well / what you did poorly"** | V1 | 🔴 | The core ask — a strengths-and-weaknesses readout after **every** call. |
| 13A.4 | **Scored performance rubric / dimensions** (discovery quality, objection handling, talk-to-listen ratio, methodology adherence, next-step setting, accuracy/compliance, rapport, filler/monologue) | V1 | 🔴 | Consistent, comparable scoring across calls and reps. |
| 13A.5 | **Configurable rubric** (admins choose dimensions, weights, and methodology, e.g. MEDDICC/BANT/SPICED) | V2 | 🟠 | Different teams grade on different things. |
| 13A.6 | **Evidence-linked feedback** (each "did well/poorly" item cites the transcript moment) | V1 | 🔴 | Coaching must be specific and verifiable, not vague. |
| 13A.7 | **Longitudinal performance tracking per employee** (trends across all their calls) | V1 | 🔴 | "Monitored over time" — the heart of the request. |
| 13A.8 | **Personalized improvement plan** (top 1–3 things to work on, with how-to guidance & examples) | V1 | 🔴 | "Tell them how they need to improve." |
| 13A.9 | **Strengths reinforcement** ("what you're doing well — keep doing it") | V1 | 🔴 | "Tell them what they are doing good." |
| 13A.10 | **Skill/competency progress tracking** (is each weakness improving call-over-call?) | V2 | 🟠 | Proves coaching is working; closes the loop. |
| 13A.11 | **Rep self-view dashboard** (my scores, trends, strengths, focus areas) | V1 | 🔴 | The employee sees their own development, building trust. |
| 13A.12 | **Manager view** (per-rep and team-wide strengths/gaps, who needs help on what) | V1 | 🔴 | Managers coach to the data; targets the budget holder. |
| 13A.13 | **Benchmarking vs. team & top performers** ("your discovery is below team median") | V2 | 🟠 | Context for each score; surfaces best-practice patterns. |
| 13A.14 | **Regression / struggle alerts** (flag when a rep repeatedly misses the same skill or trends down) | V2 | 🟠 | Proactive coaching before it costs deals. |
| 13A.15 | **Micro-learning / coaching nudges** (assign a short tip, drill, or example call for a weak area) | V2 | 🟢 | Turns feedback into action. |
| 13A.16 | **Best-call exemplars** (surface the rep's own/teammates' great moments for a given skill) | Future | 🟢 | Learn from real, approved examples. |
| 13A.17 | **Goal setting & review cadence** (set targets, weekly/monthly auto coaching report per rep) | V2 | 🟢 | Structures the development loop for managers. |
| 13A.18 | **Ramp-tracking for new hires** (measure time-to-competency per skill) | V2 | 🟠 | Ties to the "faster ramp" outcome story. |
| 13A.19 | **Employee consent / transparency & monitoring-notice controls** | V1 | 🔴 | Required for lawful, trusted employee monitoring (see §14). |
| 13A.20 | **Privacy guardrails** (reps see their own data; configurable manager visibility; no covert scoring) | V1 | 🔴 | Development tool, not surveillance — protects adoption & morale. |
| 13A.21 | **Aggregated team-skill report** (where the whole team is strong/weak → enablement & content gaps) | V2 | 🟢 | Feeds playbook (§12) and content priorities (§13.6). |

---

## 14. Consent & Compliance

The decisive risk area in both source docs. These are product features, not just policy.

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 14.1 | **In-call consent / disclosure prompt** ("This meeting may use AI transcription/coaching") | MVP | 🔴 | Core to the "transparent co-pilot" path; non-negotiable for serious buyers. |
| 14.2 | **Admin toggle to *require* explicit consent** | MVP | 🔴 | Two-party-consent states (CA/IL/etc.) and GDPR need affirmative consent. |
| 14.3 | **"No-recording" mode** (process transcript live, store nothing) | MVP | 🔴 | Lowers legal exposure; a CallCopilot-style differentiator. |
| 14.4 | **Audio-off-by-default storage setting** | MVP | 🔴 | Don't persist raw audio unless explicitly enabled. |
| 14.5 | **Configurable data-retention controls** (auto-delete after N days) | V1 | 🔴 | GDPR/DPDP data-minimization; enterprise requirement. |
| 14.6 | **Delete-on-request (per call / per contact)** | V1 | 🔴 | Data-subject rights (GDPR/CCPA/DPDP). |
| 14.7 | **Consent logs visible to admins** | V1 | 🔴 | Auditable proof of consent per call. |
| 14.8 | **Instant "pause listening"** (also a UX feature, 1.8) | MVP | 🔴 | Stop processing sensitive moments. |
| 14.9 | **Region/data-residency controls** (EU/India) | V2 | 🟠 | Enterprise & regulated-vertical requirement. |
| 14.10 | **Two-party-consent-aware defaults by participant location** | V1 | 🟠 | Conservative default: require all-party consent. |
| 14.11 | **DPA (Data Processing Agreement) support** | V2 | 🔴 | Required to sell to business/enterprise. |
| 14.12 | **"No training on customer data" guarantee (default)** | MVP | 🔴 | Trust differentiator; contractual must-have. |
| 14.13 | **Configurable meeting-notice banner / spoken disclosure** | V1 | 🟠 | Implied notices are insufficient under GDPR; offer a clear one. |

---

## 15. Security & Privacy

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 15.1 | **Encryption in transit (TLS) and at rest** | MVP | 🔴 | Baseline for handling call/personal data. |
| 15.2 | **SSO / SAML / OIDC** | V2 | 🔴 | Enterprise gate. |
| 15.3 | **Audit logs** (access, exports, deletions) | V2 | 🔴 | Enterprise & compliance requirement. |
| 15.4 | **Role-based access control (RBAC)** | V1 | 🔴 | Who can see which calls/content. |
| 15.5 | **SOC 2 Type II** | V2 | 🔴 | Named milestone for moving upmarket. |
| 15.6 | **Tenant data isolation** | MVP | 🔴 | Multi-tenant SaaS baseline. |
| 15.7 | **PII detection & redaction at rest** | V2 | 🟠 | Minimize stored sensitive data. |
| 15.8 | **Private / VPC / self-hosted deployment option** | Future | 🟢 | For the most security-sensitive enterprises. |
| 15.9 | **Secrets / key management & rotation** | MVP | 🟠 | Protect API keys (STT/LLM/CRM tokens). |
| 15.10 | **Penetration testing & vulnerability management** | V2 | 🟠 | Required for enterprise security review. |

---

## 16. Accounts, Billing & Pricing

Mirrors the pricing model from `Verdict.md` (Free → Solo → Pro → Team → Enterprise).

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 16.1 | **Sign-up / auth (email + Google/Microsoft OAuth)** | MVP | 🔴 | Self-serve entry. |
| 16.2 | **Free trial** (limited calls/minutes) | MVP | 🔴 | "Let reps experience the magic quickly." |
| 16.3 | **Solo plan ($19–$29/mo)** | MVP | 🔴 | Founders / individual reps. |
| 16.4 | **Pro plan ($49/mo)** | V1 | 🔴 | Serious individual sellers. |
| 16.5 | **Team plan ($69–$89/user/mo)** | V1 | 🔴 | Shared playbooks, admin, CRM, analytics. |
| 16.6 | **Business/Enterprise ($120+/user/mo or custom)** | V2 | 🟠 | SSO, audit logs, DPA, retention, SOC 2. |
| 16.7 | **Usage metering** (minutes / hours transcribed) | MVP | 🔴 | STT+LLM cost control; fair-use enforcement. |
| 16.8 | **Fair-use limits & overage handling** (30–50 hrs/user/mo, pooled team minutes) | V1 | 🔴 | Unlimited calls can blow up costs. |
| 16.9 | **Billing / payments (Stripe)** | MVP | 🔴 | Collect revenue from day one (paid pilots). |
| 16.10 | **Hybrid base + usage pricing option** | V1 | 🟠 | Research: hybrid is the pragmatic PAYG answer. |
| 16.11 | **Seat management / team invites** | V1 | 🔴 | Team expansion motion ("invite your team"). |
| 16.12 | **Plan upgrade/downgrade & self-serve checkout** | V1 | 🟠 | Product-led growth. |

---

## 17. Onboarding & Activation

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 17.1 | **<10-minute setup** (install → upload docs → first call) | MVP | 🔴 | "A founder can upload docs and use it on a call within 10 minutes" — a named win condition. |
| 17.2 | **Guided doc-upload wizard** | MVP | 🔴 | Reduce the painful-data-setup churn risk. |
| 17.3 | **Mock-call onboarding** (test before a real call) | V1 | 🟠 | Builds confidence without risk; also a demo asset. |
| 17.4 | **Sample/templated content** (objection & battlecard starters) | V1 | 🟠 | Cold-start value before the team uploads everything. |
| 17.5 | **In-app checklists & tooltips** | V1 | 🟢 | Drive activation milestones. |
| 17.6 | **Team-invite & playbook-share prompt after activation** | V1 | 🔴 | The PLG → team-plan expansion trigger. |
| 17.7 | **Quick connectors (Drive/Notion/website)** during onboarding | V1 | 🟠 | Fast KB population (ties to 4.3/4.4). |

---

## 18. AI Quality, Latency & Trust Infrastructure

The features that decide whether this is "valuable" or "a distracting toy."

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 18.1 | **End-to-end latency budget & monitoring** (capture→STT→detect→retrieve→generate→render) | MVP | 🔴 | Must hit 2–4 s; instrument every stage. |
| 18.2 | **Suggestion feedback capture** (helpful/wrong/late) | MVP | 🔴 | Powers metrics + the learning loop. |
| 18.3 | **Hallucination / groundedness evaluation harness** | MVP | 🔴 | Continuously measure wrong-answer rate (target <5%, ~0 for pricing/security). |
| 18.4 | **Eval suite of recorded calls / synthetic objections** | V1 | 🔴 | Regression-test model & prompt changes against real moments. |
| 18.5 | **Outcome-learning loop** (which suggestions were used → which calls converted) | V2 | 🟠 | A named moat: "a team-specific sales brain." |
| 18.6 | **Pluggable LLM provider abstraction** | MVP | 🟠 | Cost/quality/latency flexibility; avoid lock-in. |
| 18.7 | **Prompt & model versioning + rollback** | V1 | 🟠 | Safe iteration on the live-critical path. |
| 18.8 | **Cost-per-call monitoring & guardrails** | V1 | 🔴 | STT+LLM economics determine margin. |
| 18.9 | **Confidence-gated display** (suppress low-confidence cards) | MVP | 🔴 | Don't show a card unless it clears a quality bar. |
| 18.10 | **Per-customer model/prompt tuning from approved content** | Future | 🟢 | Deepens the team-specific-brain moat. |
| 18.11 | **PII/sensitive-data filtering before LLM calls** | V2 | 🟠 | Minimize what leaves the device/region. |

---

## 19. Defensibility / Moat Features

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 19.1 | **Approved-knowledge accuracy** (versioned, admin-approved, cited) | V1 | 🔴 | Hard-to-copy correctness vs. "we use GPT." |
| 19.2 | **Outcome-learning loop** (best calls/reps → better cards) | V2 | 🟠 | The strongest stated moat. |
| 19.3 | **Sales-specific workflow depth** (methodologies, CRM sync, playbooks) | V1 | 🟠 | Generic LLM wrappers can't match. |
| 19.4 | **Vertical specialization packs** | Future | 🟢 | SaaS/insurance/real estate/edtech templates. |
| 19.5 | **Trust & compliance posture as a product** (consent, retention, no-training) | V1 | 🔴 | Turns the biggest risk into a differentiator. |
| 19.6 | **UX quality** (fast, unobtrusive, genuinely helpful) | MVP | 🔴 | The thing reviews say makes or breaks live tools. |
| 19.7 | **Distribution integrations** (CRM marketplaces, Workspace/Zoom marketplaces, RevOps partners) | V2 | 🟢 | Channel moat. |

---

## 20. Platform, Infrastructure & Non-Functional

| # | Feature | Tier | Pri | Rationale |
| --- | --- | --- | --- | --- |
| 20.1 | **Real-time streaming backend** (WebSocket / gRPC pipeline) | MVP | 🔴 | Low-latency audio→suggestion path. |
| 20.2 | **Auto-updating desktop client** | V1 | 🟠 | Ship fixes fast across macOS/Windows. |
| 20.3 | **Crash reporting & telemetry (privacy-respecting)** | V1 | 🟠 | Reliability on a live-critical tool. |
| 20.4 | **Horizontal scalability of STT/LLM workers** | V1 | 🟠 | Concurrent live calls across customers. |
| 20.5 | **Graceful degradation** (if STT/LLM/CRM down, fail soft) | MVP | 🔴 | A crashed copilot mid-call is worse than none. |
| 20.6 | **Observability / dashboards (latency, error, cost SLOs)** | V1 | 🟠 | Operate the latency-sensitive system. |
| 20.7 | **Offline / poor-network resilience** | V1 | 🟢 | Calls happen on bad Wi-Fi. |
| 20.8 | **Multi-tenant data architecture** | MVP | 🔴 | SaaS foundation. |
| 20.9 | **Vector store + relational store + object store** | MVP | 🔴 | KB embeddings, app data, transcripts/recordings. |
| 20.10 | **Feature flags / staged rollout** | V1 | 🟢 | Safe release of live-path changes. |
| 20.11 | **Accessibility (WCAG) for overlay & dashboards** | V2 | 🟢 | Broader usability & enterprise procurement. |
| 20.12 | **Linux desktop client** | Future | 🟢 | Long-tail demand. |

---

## 21. Feature summary by release tier

### MVP (0–3 months) — deliver the "magic moment" for a paid pilot
Desktop app (mac/Win) with mic + system-audio capture and two-stream split ·
streaming STT (<400 ms) with rep/prospect labeling · objection / question /
competitor / pricing detection with throttling · doc upload + battlecard/objection
authoring · embedding + semantic retrieval · **cited, source-only** short
suggestion cards (objection / Q&A / pricing / **do-not-say** / discovery / stall
lines) at 2–4 s · always-on-top glanceable overlay with hotkeys, manual ask,
screen-share privacy + **disclosure**, and per-card feedback · post-call summary +
action items · admin playbook builder · **consent prompt, require-consent toggle,
no-recording mode, no-audio-storage, no-training default, instant pause** ·
encryption + tenant isolation · auth, free trial, Solo plan, usage metering,
Stripe · <10-min onboarding · latency/groundedness/feedback instrumentation.

### V1 (3–9 months) — product-led growth
Teams support · browser extension · custom vocab · hybrid search + re-ranking +
caching · streaming cards · case studies · methodology overlays · next-best-action ·
follow-up email + CRM-ready notes + unanswered-question capture · **HubSpot +
Salesforce sync** · calendar auto-start · Drive/Notion/URL ingestion + versioning +
approval workflow · team/role mgmt + RBAC · usage dashboard + usefulness analytics
+ content-gap report · **employee registry + per-call "what you did well/poorly"
scored summaries, longitudinal per-rep tracking, personalized improvement plan +
strengths reinforcement, rep self-view + manager view, with employee-monitoring
consent & privacy guardrails** · retention controls, delete-on-request, consent
logs · Pro &
Team plans, fair-use limits, hybrid pricing, seat mgmt · mock-call onboarding +
team-invite prompt · eval suite, prompt/model versioning, cost guardrails ·
auto-update, observability.

### V2 (9–18 months) — move upmarket
Live PII redaction · per-team knowledge spaces · CRM context pull · Slack/Teams +
email integrations · public API/webhooks · marketplace listings · security-Q&A &
compliance answers · call-stage awareness · **configurable coaching rubric,
skill-progress tracking, benchmarking vs. team/top performers, regression alerts,
micro-learning nudges, goal-setting + auto coaching reports, new-hire ramp
tracking, team-skill report** · win/loss & ramp analytics, scorecards,
ROI reporting, manager call review · region/data-residency, DPA · SSO, audit logs,
SOC 2, pen-testing · Business/Enterprise plan · outcome-learning loop · per-customer
tuning · accessibility.

### Future (18 months+) — moat & bets
Screen/slide OCR context · multi-language · vertical packs · phone/dialer support ·
highlight reels · A/B talk-track testing · sales-engagement-tool hooks · Zapier ·
self-hosted/VPC · Linux client · leaderboards.

---

## Explicitly **out of scope early** (anti-features)

From `Verdict.md` — things to *not* build first, to stay focused and trustworthy:

- ❌ Autonomous AI that **speaks to the client** directly.
- ❌ Full CRM **forecasting** / deep revenue-intelligence platform (don't fight Gong head-on).
- ❌ Heavy **manager analytics** before the live experience is great.
- ❌ **Too many** meeting platforms at once (start Zoom + Meet).
- ❌ **Multi-language** before English is excellent.
- ❌ **"Undetectable" / "secret" / "cheat"** positioning (legal + trust + procurement risk).
- ❌ **Real-time video/screen analysis** before the audio/text path is solid.

---

## Sources

- LivePitchAI — [livepitchai.com](https://www.livepitchai.com/) · [pricing](https://www.livepitchai.com/pricing)
- Nomi — [nomi.so](https://www.nomi.so/) · [copilot intro](https://www.nomi.so/blog/2025-06-12-introducing-nomi-copilot) · [battlecards](https://www.nomi.so/blog/sales-battlecards-templates)
- Aircover — [aircover.ai](https://www.aircover.ai/) · [Virtual Sales Engineer](https://www.aircover.ai/virtual-sales-engineer) · [In-Call AI](https://www.aircover.ai/in-call-ai)
- Gong — [Conversation Intelligence](https://www.gong.io/conversation-intelligence)
- Outreach Kaia — [Conversation Intelligence](https://www.outreach.ai/platform/features/conversation-intelligence)
- Real-time STT — [AssemblyAI: best real-time STT 2026](https://www.assemblyai.com/blog/best-api-models-for-real-time-speech-recognition-and-transcription) · [Deepgram: best STT APIs 2026](https://deepgram.com/learn/best-speech-to-text-apis-2026) · [AssemblyAI: streaming diarization](https://www.assemblyai.com/blog/streaming-speaker-diarization)
- Overlay & audio capture — [Electron desktopCapturer](https://www.electronjs.org/docs/latest/api/desktop-capturer) · [Invisible-to-screenshare overlay (Electron)](https://levelup.gitconnected.com/how-i-made-a-desktop-app-invisible-to-screen-sharing-electron-os-level-tricks-5734513c1e67) · [Capturing system audio in Electron (macOS)](https://medium.com/@kadircekim.07/capturing-system-audio-in-electron-js-macos-df404c9e6e6b)
- Sales methodologies — [BANT/MEDDIC/SPICED overview](https://gtmsecondbrain.com/content-library/the-most-common-sales-methodologies-bant-meddic-and-spiced) · [Spekit MEDDIC](https://www.spekit.com/blog/meddic-sales)
- Strategic inputs — `Verdict.md`, `Market Research_ Real-Time AI Sales-Call Assistant (...).md` (this repo)
