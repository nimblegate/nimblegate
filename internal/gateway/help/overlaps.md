# Overlaps

Open branches that change the same files. When several agents work on one repo at the same time, two of them editing the same file is the usual cause of a painful merge. This page shows it while the work is still in progress, not at merge time.

A branch's changes are measured from where it split off the default branch, so work that has already landed on the default branch never counts as an overlap.

## Sections

- **Now**: pairs of open branches that share files right now, computed live from the repos each time the page loads. Each branch shows its current short commit SHA.
- **Recorded on push**: every accepted push that overlapped another branch at the moment it arrived, newest first. The pushed branch carries the same short SHA as its [Feed](/feed) row (where the push also has an **overlap** pill), and the other branch shows the commit it was on when the push arrived. Each SHA opens that commit on your git host, or copies it when no web address is known (see [Settings → Commit links](/settings?tab=links)).

## Key actions

- **Filter by repo**: use the repo dropdown.
- **Tell the orchestrator**: turn on *Also send overlap events* in the repo's Notification rail (Policy page or Auto-PR config). Each overlapping push then sends a `push.overlap` webhook, so whatever runs your agents can pause one, reorder the tasks, or let both continue knowingly.

## What counts

- Enforce-mode repos only. Observe mode records nothing here.
- Branches with no commit in the last 14 days are ignored, and at most 50 of the most recent branches are compared.
- The check is advisory: it never rejects or delays a push, and nothing about it reaches the agent that pushed.

## Common gotchas

- Agents that push only when they are finished are seen only when they are finished. Have them push checkpoints so an overlap shows up early.
- Two branches touching one file on purpose is fine; the page reports, it does not judge.
- A check that times out on a very large repo is logged on the [Events](/events) page as `overlap-check-failed`.
