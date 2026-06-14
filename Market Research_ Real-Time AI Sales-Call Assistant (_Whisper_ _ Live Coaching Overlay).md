# **Market Research: Real-Time AI Sales-Call Assistant ("Whisper" / Live Coaching Overlay)**

## **TL;DR**

* **There is real, growing demand for AI sales tooling — but the specific "always-on-top, undisclosed overlay that whispers what to say mid-call" is the riskiest, least-validated slice of it.** The proven, fundable demand is for *post-call* conversation intelligence (Gong surpassed $300M ARR) and *contact-center* real-time agent assist (Balto, Cresta); true live whisper coaching on B2B video calls remains thin on evidence and is dogged by latency, distraction, and severe legal/consent exposure.  
* **The legal and platform-policy risks are potentially product-killing, not marginal.** Real-time transcription of a prospect without disclosure can trigger two-party-consent wiretap statutes (CIPA, etc.), GDPR consent obligations, and an active wave of class-action litigation (*Ambriz v. Google*, *In re Otter.ai*), while Zoom, Google Meet, and Microsoft Teams are actively tightening controls on third-party bots and capture in 2025-2026.  
* **A viable product exists, but only if it is built as a transparent, consented, document-grounded "co-pilot" — most realistically sold to high-volume SMB/inside-sales and contact-center teams, not as a covert "Cluely for sales."** Pay-as-you-go pricing is viable and differentiating at the low end, but enterprises prefer predictable per-seat budgets.

## **Key Findings**

### **1\. Market demand and validation**

* The **conversation intelligence** category is large and growing, but estimates vary widely by analyst: Future Market Insights pegs it at \~$25.3B (2025) → $55.7B (2035) at 8.2% CAGR; other firms cite figures ranging from \~$3.2B to \~$29B in 2025 depending on scope. The wide divergence reflects unsettled category definitions — treat all such figures as directional.  
* The **sales enablement platform** market is \~$5-7B in 2025 with consensus CAGRs of 16-22%, reaching \~$12-35B by the early 2030s depending on source.  
* **Proof of willingness to pay exists at the category level:** Sacra estimates Gong crossed $300M ARR in January 2025, up 28% YoY (versus a 16% growth bottom in 2023); Gong's own March 5, 2025 release states it "surpassed $300 million in ARR for fiscal year 2025," with AI usage up \~50% and "Ask Anything" usage up over 400% YoY. Sacra notes the company's $7.25B 2021 valuation now implies \~24x ARR (secondary transactions in late 2025 implied a lower \~$4.5B). This validates demand for sales conversation AI broadly — but Gong is explicitly *post-call*, not live whisper coaching.  
* **The critical nuance:** demand for *real-time, in-call* guidance is strongest and best-proven in **contact centers** (scripted, high-volume, compliance-driven). Balto states it has "guided over 400 million calls and surfaced 1 billion real-time recommendations," and its 2021 Series B release cited "statistically significant results, including 26% increases in conversion rates, 25% increases in CSAT scores, and 75% faster ramp times for new agents" (customers named include AmTrust Financial, Katapult, and National General Insurance). Real-time guidance is far less proven for B2B AEs on consultative video discovery calls.  
* Independent analysts caution the category is overhyped. Per the Forbes Research 2025 AI Study of 1,075 global C-suite executives, "less than 1% of global executives surveyed say their organizations have realized a significant ROI from AI" (defined as a 20%+ profitability or cost-savings increase); only 3% report 10-20% ROI and 53% report just 1-5%. One industry review bluntly characterized the live-assistant space as "80% marketing and 20% substance right now."

### **2\. Competitive landscape**

**Post-call / conversation intelligence (indirect competitors, dominant incumbents):**

* **Gong** — market leader, $300M+ ARR, \~4,500 customers (Canva, Google, LinkedIn, Square); post-call analytics, deal intelligence, forecasting. Pricing \~$5,000-50,000 platform fee \+ \~$1,300-1,600/user/year; **no live whisper coaching**. Pushed toward more real-time/agentic features ("Mission Andromeda" / Gong Orchestrate) in late 2025/early 2026 but the core remains post-call.  
* **Chorus (ZoomInfo)** — conversation intelligence bundled with ZoomInfo; typically 10-20% cheaper than Gong (\~$8,000/yr for 3 users \+ \~$1,200/additional user); widely seen as having stagnated post-acquisition (acquired 2021).  
* **Salesloft** — sales engagement first, CI added; acquired by Clari (Dec 2025), combining \~$450M ARR.  
* **Sybill, Attention, tl;dv, Fathom, Otter.ai, Avoma, Fireflies** — AI note-takers/assistants. Sybill raised \~$11M Series A (Greycroft, 2024; \~$14.5M total), \~$6.3M ARR. Attention raised \~$38M+ total and offers "real-time" coaching scorecards (Starter \~$59/user/mo). These are mostly post-call or light real-time.

**True real-time / live in-call coaching (direct competitors):**

* **Balto** — the clearest real-time leader, but in *contact centers*. Always-on visible guidance overlay listening to both sides, surfacing prompts/checklists/rebuttals. $52M total funding ($37.5M Series B led by Stripes, with RingCentral Ventures). Pricing \~$50-150+/agent/month (custom). A reviewer described the overlay "automatically pop\[ping\] up on the screen you last put it on" — confirming an overlay UX.  
* **Cresta** — real-time contact-center agent assist; $52M ARR (2025), $1.6B valuation, $276M raised.  
* **Aircover** — explicitly real-time in-call AI for B2B sales/GTM, document-grounded ("virtual sales engineer"); reviewers note a \~2-minute join delay.  
* **Dialpad** — "AI Live Coach Cards" within its UCaaS stack; Dialpad Sell \~$110/user/mo.  
* **Salesken, Wingman (Clari Copilot), Poised** — varying degrees of live nudges; Salesken and Poised are explicitly real-time. Reviews note live alerts "can be distracting" and "break concentration."  
* **Hyperbound** — AI roleplay/practice (pre-call, not live), $15M Series A (Peak XV; \~$18.3M total).  
* **Cluely** — the closest analog to the proposed product: an "invisible," always-on-top GPU-rendered overlay (rendered via DirectX/Metal hooks so it is invisible to screen-share on Zoom/Meet/Teams) that listens and surfaces real-time suggestions for interviews, sales calls, and exams. Launched April 2025 ("Cheat on Everything"), 70,000 signups in week one; raised $5.3M seed (Abstract Ventures, Susa Ventures) \+ a $15M a16z Series A in June 2025 (\~$20M total, \~$120M valuation). **Founder Roy Lee admitted on X on March 5, 2026 that the \~$7M ARR he gave TechCrunch in summer 2025 was false; actual June 2025 Stripe figures were consumer ARR $2.7M (run rate $3.8M) plus enterprise ARR $2.5M \= \~$5.2M total — a \~$1.8M / \~35% overstatement.** Repeatedly criticized for 5-90 second latency, hallucinations, and a 2025 data breach exposing data of 83,000+ users.

**Overlay-specific note:** Only Cluely (and clones) market the *covert, always-on-top, invisible-to-screenshare* overlay. Balto/Aircover use a *visible* on-screen overlay for the rep. The covert overlay is the proposed product's core differentiator — and its core risk.

### **3\. Willingness to pay and pricing**

* **Per-seat is the category norm:** enterprise CI runs \~$1,200-1,600/user/year \+ platform fees; contact-center real-time assist \~$50-150/agent/month; mid-market AI assistants \~$30-110/user/month; note-takers \~$10-30/user/month.  
* **Usage-based/PAYG is viable and increasingly common in AI tooling:** per Metronome's *State of Usage-Based Pricing 2025*, 85% of respondents have adopted some form of usage-based pricing; credit-based models grew 126% YoY per Kyle Poyar/PricingSaaS analysis (though that 126% reflects a small base, \~35→79 companies). Speech-to-text inputs are cheap (OpenAI \~$0.006/min; AWS \~$0.024/min at scale; transcription APIs $0.016-0.50/min), so a per-minute model is technically feasible with healthy margins.  
* **Tradeoffs:** PAYG suits individuals/freelancers/SMBs and product-led growth (low commitment, pay-for-what-you-use), but enterprises dislike unpredictable bills and procurement prefers fixed budgets; PAYG also makes ARR forecasting volatile. **A hybrid (base \+ usage/overage) is the pragmatic answer.**  
* **Realistic price points:** individuals/freelancers \~$20-40/mo (Cluely is $20/mo consumer, with a \~$150/mo "stealth"/undetectable tier); SMB teams \~$40-110/user/mo; enterprise \~$1,000-1,600/user/yr \+ platform fee.

### **4\. Customer segments and use cases**

* **Strongest pain \+ budget \+ fit:** high-volume **inside sales / SDR-BDR teams and contact centers** (insurance, financial services, telecom, collections) — scripted, repeatable, compliance-heavy calls where real-time prompts demonstrably help (Balto's entire business). New-hire ramp is the killer use case (75% faster-ramp claims).  
* **Moderate fit:** mid-market AEs running structured demos; solo founders/freelancers selling their own product (Cluely's consumer base).  
* **Weak fit:** complex, consultative enterprise B2B discovery calls where reading an overlay mid-conversation breaks rapport and where buyers are sophisticated.  
* **B2C/outbound telesales** is a natural fit operationally but carries the heaviest consent/telemarketing legal load.

### **5\. Legal, ethical, and practical risks (the decisive section)**

**(a) Recording/wiretapping consent:**

* US federal law is one-party consent, but \~11-13 states (CA, FL, IL, MD, PA, WA, etc.) require **all-party consent**; violations can be criminal. The conservative rule: if any participant is in a two-party state, get everyone's consent.  
* **Real-time transcription may legally count as "interception," not just recording** — a stricter trigger under wiretap statutes. Multiple legal sources note AI that "listens and transcribes in real time can be treated as interception."  
* **Illinois BIPA** treats voiceprints as biometric data requiring written consent (active suit: *Cruz v. Fireflies.AI*).  
* **Active litigation wave:** In *Ambriz et al. v. Google LLC*, Case No. 3:23-cv-05437-RFL (N.D. Cal., Feb. 10, 2025), the court denied Google's motion to dismiss CIPA §631(a) claims over Google Cloud Contact Center AI, adopting the "capability test" from *Javier v. Assurance IQ* — a vendor with "the capability to use the user data to its benefit, regardless of whether or not it actually did so" can be a third-party wiretapper. Plaintiffs alleged interception on calls to Verizon, Hulu, GoDaddy, and Home Depot. This directly threatens any cloud AI overlay that processes prospect audio. *In re Otter.ai Privacy Litigation* (N.D. Cal.) consolidates multiple suits, with a motion-to-dismiss hearing set for May 2026\. (Note: the Ninth Circuit's *Popa v. Microsoft*, Aug 2025, pushed the opposite way on Article III standing, so the law is genuinely unsettled.)

**(b) GDPR/EU:**

* GDPR does not ban recording but requires a lawful basis (consent or legitimate interest) plus transparency. Implied "this call may be recorded" notices are **insufficient** for valid consent (Danish DPA ruling). Some EU states (e.g., Germany) are two-party-consent with criminal penalties. Fines can reach €20M or 4% of global turnover. A covert overlay that never discloses is squarely non-compliant for EU prospects.

**(c) Platform policy:**

* **Bot-based capture is being actively restricted:** Microsoft Teams (notice MC1251206, March 2026\) will detect and label external bots "Unverified," requiring explicit admit; Google Meet rolled out (March 2026\) a two-queue system auto-denying flagged bots; Zoom AI Companion requires host initiation and visible indicators. Universities (University of Washington, Chapman, UC Riverside) blocked Read AI/third-party notetakers in 2025\.  
* **Overlay/extension capture (the proposed approach) sidesteps the bot lobby** by reading native captions or capturing audio locally — but browser automation "often violates Google Meet's Terms of Service," and Cluely-style GPU overlays exist specifically to evade screen-share detection, which is reputationally and contractually fraught.

**(d) Ethics/reputation:**

* Coaching reps to "persuade/handle objections" with an undisclosed AI sits on the manipulation/dark-patterns spectrum that regulators (FTC, EU AI Act, UNESCO) are increasingly scrutinizing. Cluely's "cheat on everything" launch became a cautionary tale; competitors (tl;dv and others) now explicitly position around *transparency and consent* as a differentiator.

**(e) Accuracy/latency:**

* Until \~2023, real-time coaching had 2-4 second latency and reps disabled it within a week. By 2026 leading tools claim sub-400ms suggestion lag, which one industry analysis called "usable for the first time." But Cluely real-world testing showed 5-90 second delays and hallucinations. The "acceptable" threshold for conversational flow is \~300ms; above it, prompts arrive after the moment has passed. Practitioner reviews repeatedly cite split attention as the core UX failure of mid-call overlays — e.g., one tester noted "I was waiting for the prompt rather than leading the conversation," and a review concluded reps end up "half-present."

### **6\. Go-to-market and differentiation**

* **Gaps the product could fill:** (1) a genuinely *document-grounded* (RAG over company pricing/objection docs) live assistant for SMB/inside-sales that incumbents under-serve at the low end; (2) PAYG pricing that lets individuals and small teams start for cents; (3) a transparent, consent-first design that turns the biggest risk into a trust differentiator.  
* **Distribution:** product-led growth/self-serve for individuals and SMBs; communities (r/sales, sales Slack/Discord groups, LinkedIn sales-influencer channels); integration marketplaces (Salesforce, HubSpot, Zoom/Meet/Teams app stores — subject to policy compliance).  
* **Demand signal from communities:** practitioner sentiment is genuinely split. Contact-center agents praise real-time prompts (e.g., "I'm able to improve instantly… which boosts my confidence and performance right in the moment"), while many sellers find mid-call overlays distracting and laggy. The market wants the *outcome* (confidence, objection handling, faster ramp) but is skeptical of the *mid-call reading* mechanic.

## **Details**

The strategic tension is between two product philosophies that the market treats very differently:

1. **The "Cluely for sales" path** — covert, always-on-top, invisible-to-screenshare overlay, undisclosed to the prospect. This maximizes the "secret weapon" appeal and is the literal description in the product brief. It is also the path with (a) the gravest legal exposure (interception/wiretap, GDPR, BIPA), (b) direct platform-policy conflict, (c) the worst reputational profile (Cluely's fabricated revenue admission, data breach, and "cheat" branding are now industry shorthand for what not to do), and (d) the weakest UX evidence (split attention, latency).

2. **The transparent co-pilot path** — disclosed recording/transcription with consent capture, document-grounded suggestions, designed to be *glanceable and peripheral* rather than scripts to read aloud. This aligns with where funded, durable players (Balto, Cresta, Aircover, Gong) actually operate, and where buyers with budgets (sales orgs, contact centers) will sign contracts.

The funding and revenue data strongly favor path 2: every company with real enterprise traction — Gong ($300M+ ARR), Cresta ($52M ARR / $1.6B valuation), Balto ($52M raised, 400M+ calls guided) — operates transparently and/or in contact centers. Cluely, the one covert-overlay company, has \~$5.2M ARR built largely on consumers and heavy marketing spend, an admitted fabricated-revenue scandal, and a data breach.

## **Recommendations**

**Stage 1 — Validate and de-risk (0-3 months):**

* Build a transparent MVP: an in-call assistant with an explicit, built-in consent prompt and disclosure, document-grounded (RAG) suggestions, targeting **one high-volume, scripted segment** (e.g., SMB inside sales or a contact-center vertical like insurance) where real-time prompts are proven to help.  
* Engage a privacy/telecoms attorney to design the consent flow for two-party-consent states and GDPR *before* launch. Treat real-time transcription as "interception" and obtain affirmative all-party consent by default.  
* **Kill criterion:** if you cannot design a consent flow that preserves the value while being lawful, the covert version is not a viable business — pivot to the transparent co-pilot.

**Stage 2 — Product-led growth (3-9 months):**

* Launch PAYG/per-minute pricing (\~$0.20-0.50/min retail over \~$0.006-0.03/min STT cost) plus a hybrid base tier (\~$20-40/mo individuals, \~$40-110/user/mo teams) for predictability.  
* Differentiate on **latency (\<400ms), glanceable UX, and document-grounding accuracy** — the three things reviews say make or break real-time tools.  
* Distribute via r/sales and sales communities, and (compliantly) via CRM/meeting-platform marketplaces.

**Stage 3 — Move upmarket (9-18 months):**

* Add team analytics, manager dashboards, SOC 2, CRM sync, and per-seat enterprise pricing once you have proof points (conversion lift, ramp-time reduction).  
* **Benchmark to change course:** if win-rate/ramp improvements don't hit the 15-28% range competitors cite within pilots, the real-time mechanic isn't earning its keep — refocus on pre-call prep \+ post-call coaching (lower-risk, proven).

**Thresholds that should change the strategy:**

* If *In re Otter.ai* or a similar case produces a plaintiff-favorable ruling in 2026, sharply curtail any undisclosed-capture ambitions globally.  
* If Zoom/Meet/Teams extend bot detection to local overlays/extensions, the technical moat for covert capture collapses — double down on official APIs and disclosed integration.

## **Caveats**

* **Market-size figures are highly inconsistent** across analyst firms (CI estimates range from \~$3B to \~$29B for 2025); treat them as directional, not precise.  
* **Most vendor ROI claims (26% conversion lift, 75% faster ramp, 15-28% win-rate) are self-reported** and come disproportionately from contact-center (scripted) deployments; they may not transfer to consultative B2B video selling.  
* **Cluely's metrics are founder-reported and partly recanted** (the founder admitted overstating ARR); its \~$5.2M figure should be read with that in mind.  
* **The legal landscape is genuinely unsettled** (Ambriz "capability test" vs. the Ninth Circuit's Popa standing analysis); 2026 rulings could materially shift risk in either direction.  
* **Raw r/sales threads with verbatim rep quotes specifically on live whisper coaching were not directly retrievable**; community sentiment here is inferred from review sites, vendor-collected (sometimes incentivized) testimonials, and product reviews, which carry bias.

