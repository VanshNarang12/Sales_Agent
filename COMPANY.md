# Sales Copilot — Company Overview

Last updated: September 2026.

## Who we are

Sales Copilot is an early-stage software company building a real-time AI
assistant for B2B sales calls. We are pre-launch and founder-led, currently
preparing the product for our first paid pilot customers.

Our mission is simple: **never lose a deal because a rep forgot the right
answer.** Every sales team has its best answers — pricing rules, case studies,
competitor comparisons, security responses — locked in the founder's head or
buried in documents nobody can open mid-call. Sales Copilot delivers those
answers inside the live call, at the exact moment the buyer asks.

We are deliberately not a "secret cheat tool." Sales Copilot is consent-first:
the rep triggers every suggestion with one click, nothing listens covertly, and
call participants are never deceived. We compete on trust, accuracy, speed, and
the fact that every answer is backed by the customer's own documents.

## What the product does

Sales Copilot listens to a live sales call (Zoom, Google Meet, or Microsoft
Teams) with consent and transcribes it in real time. When the buyer raises an
objection, asks about a competitor, or pushes on pricing, the rep clicks a
**Suggest** button. Within 2–4 seconds, Sales Copilot shows a short answer
card: one to three bullets containing the company's approved answer, with
citations pointing to the exact source documents.

The three core features:

1. **Real-time objection handling.** The rep clicks Suggest on an objection
   ("too expensive", "we're happy with our current vendor", "not now") and
   gets the company's approved, cited rebuttal instantly.
2. **Live competitor answers.** When a competitor comes up, one click pulls
   the team's own competitor comparison from their uploaded documents.
3. **Do-not-say guardrails.** Every suggestion is checked before it is shown,
   so the copilot never coaches a rep to over-promise on pricing, legal,
   security, or implementation.

Every answer is grounded: Sales Copilot only answers from the documents the
customer has uploaded and approved. If it can't find a supported answer, it
says so instead of guessing. There is no hallucinated content — a card either
cites its sources or it is not shown.

## How it works

1. **Upload documents.** A sales team uploads its knowledge: pricing sheets,
   case studies, product docs, FAQs, competitor comparisons, playbooks.
2. **Join a call.** The rep starts a call with the desktop app running. With
   consent, audio is transcribed live. Audio itself is not stored by default —
   only the transcript, which expires automatically after the call.
3. **Click Suggest.** Sales Copilot reads the last minute or two of the
   conversation, figures out what the buyer is actually asking, searches the
   uploaded documents, and generates a short cited answer card.
4. **After the call** (upcoming): a summary with action items and, later, a
   personal coaching review for the rep.

Under the hood, Sales Copilot uses best-in-class streaming speech-to-text,
semantic search over the customer's documents, and large language models to
extract the buyer's question and compose the answer. All AI providers are
swappable, so the product is never locked to a single vendor.

## What is built and working today

The core product pipeline is complete and working end to end:

- **Live transcription** — real-time speech-to-text during calls, streamed
  from the desktop app to our servers.
- **The Suggest button** — the rep can click Suggest at any moment; the system
  reads the recent conversation from a live transcript store.
- **Question extraction** — an AI step that turns the raw conversation window
  into the buyer's actual question.
- **Document knowledge base** — customers can upload documents, which are
  automatically split, indexed, and made searchable by meaning (not just
  keywords).
- **Grounded retrieval** — the extracted question is matched against the
  customer's documents, with a confidence threshold that drops weak matches.
- **Answer cards** — a generated suggestion card with citations, a confidence
  gate, and a refusal path when the documents don't support an answer.

In short: speech goes in, and a grounded, cited suggestion card comes out,
live during a call.

## What we are building next

The immediate work, in order, on the way to a paid pilot:

1. **In-call overlay** — the polished on-screen card display that floats above
   the meeting window.
2. **Consent and compliance** — a consent gate before any transcription
   starts, and ephemeral-by-default audio handling. This ships before we put
   the product on real customer calls.
3. **Sales guidance extras** — do-not-say guardrails and "stall lines" (short
   phrases a rep can use to buy time gracefully).
4. **Post-call summaries** — action items, objections raised, and CRM-ready
   notes after every call.
5. **Admin playbook tools** — sales leaders control exactly what guidance
   their reps can see.
6. **Accounts and billing** — self-serve signup, plans, and payments.
7. **Quality hardening** — proving the 2–4 second answer speed holds up
   reliably on real calls and real networks.

The bar for launch: when a prospect raises an objection and the rep clicks
Suggest, the tool must show a useful, accurate, short, source-backed answer
within 2–4 seconds — consistently.

## Future plans

- **Next 3–9 months — self-serve growth:** self-serve onboarding, team plans
  with roles and access control, HubSpot and Salesforce integration, calendar
  integration, and manager analytics. Plus the flagship follow-on: a
  **per-employee coaching engine** — after every call, each rep gets a scored
  review of what they did well and what to improve, tracked over time, with a
  personalized improvement plan.
- **9–18 months — move upmarket:** Microsoft Teams support, enterprise
  security (SSO, audit logs, SOC 2), large-scale document ingestion, advanced
  coaching and analytics, and outcome learning — measuring which suggestions
  actually correlate with won deals.
- **Beyond 18 months:** integrations with sales-engagement platforms,
  outcome-based answer ranking, and industry-specific playbooks. These are
  funded only when usage data justifies them.

## Who we sell to

Our ideal customer is a B2B software or technical-services company with
**$1M–$30M in annual revenue and 5–50 sales reps**, selling high-ticket,
consultative deals over Zoom or Google Meet, using HubSpot or Salesforce.

The best early customers:

1. **Early and growth-stage B2B SaaS companies** — the founder has the perfect
   answer to every objection; the reps don't. We turn the founder's best
   answers into real-time coaching cards for every rep.
2. **Technical sales teams** — precise answers about integrations, security,
   APIs, and competitive differences. Like giving every rep a live sales
   engineer.
3. **Agencies and high-ticket service businesses** — junior closers selling
   $5k–$100k packages who need the founder's case studies and pricing rules
   at their fingertips.

We deliberately avoid, for now: large enterprises already on tools like Gong,
low-ticket transactional sellers, teams with no written playbook, and heavily
regulated industries until our compliance posture is enterprise-grade.

## Market and competition

Roughly 2.5 million people in the US work in relevant B2B sales roles. If even
10–25% regularly sell over video calls, the reachable market is 250,000–650,000
seats. At $30–$75 per seat per month, that is a **$90M–$585M annual revenue
opportunity in the US alone**, before international expansion.

The competition splits in two:

- **Enterprise revenue-intelligence platforms** (Gong, Zoom Revenue
  Accelerator, Microsoft Copilot for Sales) analyze calls *after* they happen
  and are priced for large enterprises. They validate the category and ignore
  the mid-market.
- **Real-time point tools** (LivePitchAI at ~$29/month, CallCopilot at
  ~$19–40/month, Nomi at ~$99/month) prove buyers pay in this range, but they
  compete on covertness or give shallow, ungrounded answers.

Our position: **the trusted, document-grounded, consent-first live copilot for
mid-market B2B sales teams** — above cheap note-takers, below heavyweight
enterprise suites.

## Pricing

| Plan | Price | For whom |
| --- | --- | --- |
| Free trial | free, limited calls | try the magic moment risk-free |
| Solo | $29/month | founders and individual reps |
| Pro | $49/month | serious sellers, higher limits |
| Team | $79/user/month | shared playbooks, admin controls, CRM sync |
| Business/Enterprise | $120+/user/month | SSO, audit logs, data-retention controls |

Plans include fair-use limits of roughly 30–50 call-hours per user per month,
with overage options, because our costs scale with call minutes.

## Cost structure

Serving one 1-hour call with 10–15 suggestions costs us about **$0.65 all-in**
(range $0.45–$0.85), covering transcription, AI answer generation, document
search, and servers. Transcription is about 90% of that cost; the entire AI
answering side is only 3–4 cents per call. A Solo customer doing 20 calls a
month costs about $13 to serve against $29 in revenue — roughly a 55% gross
margin, improving with scale.

## Our principles

- **Grounded-only AI.** Every answer cites a source document or is not shown.
- **Consent in code, not fine print.** Transcription cannot start without the
  consent gate; audio is ephemeral by default.
- **Speed is the product.** Every step of the live pipeline is measured
  against the 2–4 second budget.
- **Customer data is isolated.** Every customer's documents, transcripts, and
  suggestions are strictly separated at every layer of the system.
- **Honest positioning.** No covert mode, no "undetectable" marketing, ever.

## Frequently asked questions

**Does Sales Copilot record calls?** No. Audio is processed live for
transcription and discarded by default. The transcript itself expires
automatically after the call unless the customer chooses to keep it.

**Can it answer questions about anything?** No — by design. It only answers
from the documents your team uploaded and approved. If your docs don't cover
it, the card says so rather than making something up.

**Does it interrupt or listen for keywords?** No. Nothing happens until the
rep clicks Suggest. There is no automatic detection deciding when to speak.

**Which platforms does it work with?** Zoom and Google Meet first, via our
desktop app. Microsoft Teams is on the roadmap.

**How fast are suggestions?** The target is under 3 seconds from click to
card, with a hard ceiling of 4 seconds.

**What does it cost?** Plans start at $29/month for individuals and
$79/user/month for teams. See Pricing above.
