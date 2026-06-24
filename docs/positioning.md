# nimblegate: product positioning (direction, not commitment)

> **Status: strategy hypothesis, gated on commercial intent.** This records a direction worked out in conversation so it can anchor roadmap decisions before they're made. It is **not** a statement of current state, and **not** a commitment to build. Nothing here ships until its validation signal fires (see `.appframes/_future.md`). The honest unknowns are flagged at the end: they need customer discovery, not more reasoning.

## What nimblegate is

A **deterministic, un-bypassable code-gate**: a local CLI (frames + linters + incident pipeline) and a server-side **policy gateway** that holds the sole upstream credential and gates pushes on a machine the agent can't touch. The core never calls an LLM: checks are deterministic, free per run, and auditable.

The gateway runs per-repo in **observe** or **enforce** mode: observe records every would-block but relays the push anyway, so a team adopts it with zero push-friction, calibrates against real traffic (tune false positives, exclude fixtures), then flips to enforce when ready. That observe-first on-ramp is the low-risk path to landing the gate, and the deterministic engine is what makes a future **agentic fix-loop** trustworthy (it validates the fixing agent's output rather than trusting it; parked in `_future.md`).

## Who it's for

**Target: businesses/teams, run internally (on-prem / inside their network), open-core.** Not solo beginners, and **not** a low-price high-volume SaaS.

Why:
- **The pain is multi-actor.** "No single actor (human or agent) can silently weaken the gate" only matters when there are multiple actors and real blast radius. A solo beginner is one actor, low stakes, and least feels the problem; they also can't operate a self-hosted git proxy and won't pay enough to fund the support.
- **What nimblegate holds decides the legal shape.** The gateway custodies source + push credentials. Run **internally on the customer's infra**, that never leaves their perimeter → your exposure is "we shipped software they run," the lowest legal surface. As multi-tenant **SaaS**, you'd custody thousands of users' secrets → maximum breach blast radius and legal exposure at the *lowest* revenue-per-user. The volume-SaaS play is the one model where liability scales faster than revenue: avoid it for this product.
- **Internal-run is a selling point, not just a constraint.** "Your code and credentials never leave your network" wins procurement with security-conscious buyers (finance, healthcare, defense) and is exactly where SaaS competitors can't follow.
- **Open-core funnel.** The layers already split along the segmentation: the **local CLI holds nothing** → free/OSS, solo on-ramp, near-zero liability, community-led. The **gateway** → the paid team/business layer where the pain and willingness-to-pay live. Land with the free tool, expand to the team gateway. Solo is the funnel, not the monetization engine.

## The competitive wedge: a gate, not a reviewer

The emerging competitor is "agents watching agents" (one LLM reviews/repairs another's work). That approach **reimports the bypass problem**: a probabilistic, promptable reviewer in the same trust domain can miss things, be prompt-injected, be skipped by whoever runs it, or be talked out of it. **You cannot build an un-bypassable guarantee from a non-deterministic, promptable component.**

They are **complementary on capability, not substitutes**:
- **nimblegate (deterministic gate):** codifiable, pattern-matchable issues (keys, creds, frames, regex, vet). Guaranteed, free, un-bypassable, auditable, but only catches what's encoded.
- **Agent-watcher (probabilistic reviewer):** open-ended semantic issues (logic, "did it meet the requirement"). Broad, but probabilistic, expensive, bypassable.

Position nimblegate as the **deterministic floor *under* the agent layer**, not its replacement. The wedge that lands with a security buyer: *"what stops your watcher agent from being skipped or fooled?"* The competitor's honest answer is "nothing structural"; nimblegate's is "it's deterministic and runs where the agent can't reach it." Competing on **guarantee + cost** (not on reasoning) also dodges the "models keep getting better, your rules are obsolete" objection: guarantee and cost don't erode as models improve.

## The economic argument: a cost-tiered pipeline

Run work through tiers, cheapest first; each tier handles only what the cheaper one structurally can't:

**free deterministic filter (codifiable) → expensive model (semantic/logic) → human (judgment + final audit)**

Two wins, not one:
1. **Spend less**: the expensive model isn't invoked to re-find boilerplate, and the agent fix-loop never spins on the codifiable class (where a lot of hidden token burn lives: generate → review → fix → re-review).
2. **The remaining spend works better**: feeding the reasoning model code with the codifiable noise already stripped means its limited attention goes to the hard logic, not flagging a hardcoded key for the hundredth time. Cleaner input → more accuracy per token.

**Ordering matters:** run the deterministic checks early (pre-commit / at the gate) and only escalate what passes, so the reasoning layer never sees the trivial stuff.

**The flywheel (what makes it durable):** when the expensive layer (or a human) finds a novel recurring issue, promote it to a deterministic frame via the incident pipeline: caught free forever after. The cheap filter keeps widening; the expensive layer shrinks toward only genuinely-novel work. Agent spend trends *down* over time on a maturing codebase instead of staying flat.

**Buyer one-liner (honest, and that's rare here):** *"We move the boring, measurable mistakes onto a free, un-bypassable filter (cutting agent spend on boilerplate and making your reasoning model both cheaper to run and more accurate), and the filter gets wider every time something slips."* CFO-legible (spend down) and CISO-legible (un-bypassable floor) at once.

### Two value axes, and the one we can't yet measure

The product delivers value on two distinct axes, and the metrics only capture the first:

1. **Modeled prevention (partly measurable).** Findings prevented or would-be-prevented × a per-hit time estimate: what the `/stats` "time-prevented" figure models. Honest but conservative, and **near-zero in observe mode** (nothing is blocked yet).
2. **Visibility / triage saving (real but unmeasured).** The time saved by *seeing* issues surfaced (deduped, per-repo, with click-through to what each frame means) instead of hunting through code to find them. **Even in pure observe mode, where axis 1 reads ~0, this is where most of the day-to-day value lives.** So the headline metric, if anything, *undersells* observe-mode value: it measures axis 1 while the operator is mostly getting axis 2.

The honest stance (consistent with the no-fabricated-ROI principle below): **do not invent a number for axis 2.** It's a counterfactual ("how long would finding this by hand have taken?"), which is genuinely unmeasurable live. Show the raw counts (what was surfaced, recurrence, what's standing) and let the reader apply their own sense of the saving. The fix for the observe-mode under-read is *recognizing it's a separate axis*, never inflating the metric.

**To think about later, how one might honestly measure axis 2:** a **direct manual-vs-glance comparison** on a real task: time the manual workflow to find an issue (grep / code-review / *even* run a linter and read its raw output) vs the few-seconds dashboard glance; the delta is the saving, and informal experience says it's large. Run as a one-off empirical study (like the codifiable-vs-semantic measurement in Honest unknowns), **not** a fabricated live metric. (Parked: there is deliberately no metric to construct until this is measured for real.)

## Principle: an aid to review, never a replacement for it

This is load-bearing and must stay honest:
- **The deterministic pass is not "reviewed."** A green gate means "none of the *encoded* checks fired," nothing more. It is never a substitute for reviewer responsibility.
- **The reviewer stays accountable**, and **final audits still run after the filters** as the last check. nimblegate is a tool to *help* that process: it removes the high-frequency codifiable noise so human and reasoning-model attention lands where it matters; it does not absolve either.
- **The trap is false confidence** ("gate's green, ship it") when the gate only ever checked the codifiable slice. The discipline: keep the expensive/human layer on the hard problems, and keep promoting its findings down into the free layer.
- **No fabricated ROI.** Any "time/money saved" figure is a clearly-labeled *model with the customer's own assumptions and conservative defaults* (reusing the audit analyzer's existing time-prevented philosophy), shown alongside the raw block counts it's derived from, never a confident invented number. Honesty is the differentiator, not a limitation.

## Two-layer UI (two personas, opposite needs)

- **Operator console (exists today):** the decision feed + policy tuning + check authoring. Dense, in-the-weeds. Audience: the platform/security engineers who *run* the gate.
- **Executive / assurance dashboard (new):** quick-glance charts and trends. Audience: the buyer/approver who never opens a terminal, the layer that justifies the renewal. Show, ranked by *defensibility* (this reader is professionally skeptical of vendor ROI):
  1. **Coverage**: % of repos gated, % of pushes actually through the gate vs bypassed ("is the control really in place everywhere?"). Most factual, most valuable.
  2. **Block/failure rates + trend**: straight from the audit log ("are we getting better?").
  3. **What got caught**: top recurring issues, per-team/repo concentration ("where's our risk?").
  4. **Modeled prevention**: time/cost saved, *their assumptions*, conservative defaults, shown with the raw counts. The metric they want most and trust least: handle per the honesty principle above.

Same binary, different tab, gated by **role**. Natural roles, mapping to internal SSO groups:
- **Viewer/Exec**: read-only: exec dashboard + feed.
- **Operator**: tune/author/operate (today's `--allow-edits`).
- **Admin**: register repos, manage users/credentials.

The exec layer is cheap to *render* but expensive to make *true*: honest trend charts need aggregated historical data, i.e. the parked **SQLite decision-analytics layer** (the `ReadDecisions` seam was designed for that swap). Don't ship a polished exec chart on thin/faked data: the one persona guaranteed to catch it is the money person.

## What this implies for build order (if the business direction is committed)

These currently sit parked in `.appframes/_future.md`; the positioning reprioritizes them in this order, each gated on real commercial signal:

1. **Provable enforcement + audit/compliance export**: the core buy; mostly exists, needs the export/report surface.
2. **SQLite decision-analytics layer**: the data backend the honest exec charts depend on.
3. **Executive/assurance dashboard**: built on (2), honest metrics only.
4. **SSO + RBAC behind the perimeter**: internal multi-user auth (reverse-proxy/OIDC/LDAP + the 3 roles), *not* internet-facing login (gateway dashboard auth entry).
5. **Productized onboarding**: `gateway init` / packaged install, so a buyer's ops team isn't hand-editing systemd units.
6. **UI polish**: last. A multiplier on a real value prop, never a substitute for one.

Note the dependency: the exec dashboard's value comes from the data layer, not the template; sequence accordingly.

## Honest unknowns (validate before committing the roadmap)

- **Willingness to pay**: reasoning can't supply it. Worth 5–10 customer-discovery calls: *"what would your security/compliance team need to see to approve this?"*
- **The codifiable-vs-semantic ratio**: how much of a given team's agent work is codifiable boilerplate vs genuinely semantic decides how big the cost saving is. Measure on one real repo: a month of deterministic-gate runs (~$0) vs the token cost of an agent reviewing every one of those pushes.
- **The visibility / triage saving (axis 2 above)**: real but currently unmeasured. To quantify honestly: a direct **manual-find-vs-dashboard-glance** comparison on real tasks (time to locate an issue by grep/review/raw-linter-output vs the few-seconds glance), run as a one-off study, never a fabricated live metric. Until measured, show counts only.
- **Overall confidence in this direction: ~7/10**, raised mainly by testing the unknowns above rather than more analysis.

## See also

- `.appframes/_future.md`: the parked items this sequences (gateway dashboard auth, productize deploy/onboarding, SQLite decision-analytics).
- `docs/superpowers/specs/2026-05-25-nimblegate-gateway-design.md`: the gateway design + threat model.
- `docs/server/README.md`: how the gateway is deployed/operated today.
