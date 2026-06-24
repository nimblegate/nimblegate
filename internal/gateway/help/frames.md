# Frames

Read-only browse view of every stdlib frame nimblegate ships. Use this page to discover what's available; use [Policy](/policy) to actually turn frames on or off per repo.

## Layout: four v2 axes

Frames group by the v2 three-axis + core model:

- **Core**: universal floor (git, commands). Applies to every repo.
- **Framework**: what the project is built in (Astro / Go / Python / React / Svelte / Vue). The sub-buckets are shown empty by default; frames declaring a framework will populate them.
- **Platform**: what the project deploys to. Cloudflare nests its sub-buckets (Cf Pages, Cf D1) as children rather than as flat siblings, so the hierarchy matches the v2 stdlib tree.
- **Domain**: conceptual coverage you opt into (Database, Documentation, Encoding, Filesystem, HTML, Network, Security). Multi-select per project.

Within each axis sub-buckets sort alphabetically by display name (HTML between Filesystem and Network), frames within a sub-bucket sort alphabetically by their short summary.

## Key actions

- **Search / filter**: top search box narrows by frame ID or visible summary text; severity chips (BLOCK / WARN / INFO) below the search box let you filter by severity.
- **Click a frame**: opens its detail view: severity, tier, what it catches, override markers, body documentation.
- **Click any axis or sub-bucket header**: expands or collapses to navigate the tree.

Total frame count shows on the right of the search box ("N frame(s)" / "N shown" when filtered).

## Frames vs Policy vs Custom linters

- **Frames** (this page) is the read-only catalog of every stdlib frame. Always shows everything that exists.
- **[Policy](/policy)** is the per-repo selection: which frames actually run when pushes hit this repo, with optional severity overrides per frame.
- **Custom linters** live on the [Policy](/policy) page's Custom linters tab; they're repo-scoped regex rules authored from the dashboard. They do NOT appear in the Frames browse tree (this page is stdlib only).

For why frames vs linters exist as separate categories: see the comparison table in the [Policy](/policy) page's help sidepanel.

## Common gotchas

- A frame's default severity (BLOCK / WARN / INFO) ships from the frame's frontmatter but can be overridden per-repo on [Policy](/policy).
- Empty axis sub-buckets (most of Framework today) stay visible by design so the v2 axis shape is discoverable: operators see what could populate them when a frame is authored.

For depth: [docs/frames.md](https://github.com/nimblegate/nimblegate/blob/main/docs/frames.md).
