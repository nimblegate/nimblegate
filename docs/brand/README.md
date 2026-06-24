# nimblegate brand kit

> First-pass kit. Iterates later when a designer pass happens; the goal here
> is *intentional + consistent across surfaces*, not *finished*.

## Assets in this directory

| File | Purpose | Size |
|---|---|---|
| `icon.svg` | The particle-orb mark. Transparent background; scales to any size. | 64×64 viewBox |
| `icon-512.png` | Raster icon for platforms that don't accept SVG. | 512×512 |
| `icon-32.png` | Favicon (sized for browser tabs). | 32×32 |
| `favicon-16.png` | Smaller favicon variant (legacy tabs). | 16×16 |
| `wordmark.svg` | "nimblegate" set in a system sans-serif at weight 600. | 400×80 viewBox |
| `wordmark-512.png` | Wordmark raster export. | 512px wide |
| `avatar.svg` | Social-profile avatar: orb + wordmark on dark brand surface. | 400×400 |
| `avatar.png` | The same, rasterised: upload this to every social profile picture slot. | 400×400 |
| `header.svg` | Social-profile banner: orb + wordmark + tagline + URL. | 1500×500 |
| `header.png` | Header rasterised for X / LinkedIn / dev.to standard. | 1500×500 |
| `header-yt.png` | YouTube channel art variant. | 2048×1152 |

## Visual concept

The mark is a **particle orb**: 30 dots distributed via Fibonacci-sphere
sampling, orthographically projected, with apparent depth driven by varied
dot radius + opacity. Inspired by Nate Wiley's "Particle Orb CSS" CodePen
(`particle-orb-css/dist/`), adapted as a static SVG so it renders identically
on every platform.

Why this shape (over the rail's earlier cube icon): cubes are heavily used
across dev tooling: Kubernetes, Docker, Vagrant, Cargo, hundreds of homelab
projects. The particle orb is distinct without requiring custom design work,
and conceptually fits the product story: many small events being observed
simultaneously, organised into a coherent whole. "Watches every push" maps
to "watches every dot in this orb."

## 5-step brand palette

Two locked-in source colors, the brand surface `#0b0d11` (dark) and the
brand accent `#79c0ff` (light blue), interpolated linearly into a 5-step
ladder. This is the **brand palette**; marketing materials, social profile
artwork, slide decks, future redesigns all draw from here. Strip visualised
in `palette.svg` / `palette.png`.

| Step | Hex | Name | Use |
|---|---|---|---|
| 1 | `#0b0d11` | Surface 0 | Base dark: body / app background, avatar surface, login/setup card outer surface. |
| 2 | `#263a4c` | Surface 1 | Elevated dark: cards / panels lifted above the base, hover-darken state. |
| 3 | `#426688` | Surface 2 | Border / mid: dividers, neutral mid-tone, "secondary" buttons that aren't primary actions. |
| 4 | `#5e93c4` | Accent 1 | Secondary: muted blue for body text on dark surfaces, hover-down state for accent-2 buttons. |
| 5 | `#79c0ff` | Accent 2 | Primary accent: links, primary actions, the orb itself, the wordmark. |

Two simple contrast rules cover most surfaces:
- **Text on dark steps (1-3):** Accent 2 (`#79c0ff`) for high contrast, Accent 1 (`#5e93c4`) for muted, `#cdd` for plain body text.
- **Text on light steps (4-5):** Surface 0 (`#0b0d11`) for high-contrast labels, e.g. dark text on the primary-action button.

The dashboard's existing `gwRootVars` block in `internal/commands/dashboard.go`
keeps its 40+ semantic tokens (text-faint, border-subtle, warn-bg, etc.) for
internal UI layering: that's a more granular system than the 5-step brand
palette and they live in separate roles. The brand palette is the source of
truth; the dashboard tokens map to it (Accent 2 = `--gw-accent`, Surface 0 =
`--gw-bg-input`, etc.). When the brand palette changes, the dashboard tokens
follow.

### Where each color shows up today

| Token | Hex | Dashboard CSS variable | Dashboard role |
|---|---|---|---|
| Surface 0 | `#0b0d11` | `--gw-bg-input` | App background, inputs, login/setup card outer surface |
| Surface 1 | `#263a4c` | *(new; pending dashboard adoption)* | Reserved for elevated panels in the next layering pass |
| Surface 2 | `#426688` | *(new; pending dashboard adoption)* | Reserved for borders / muted button surfaces |
| Accent 1 | `#5e93c4` | *(new; pending dashboard adoption)* | Reserved for hover-down state on primary buttons |
| Accent 2 | `#79c0ff` | `--gw-accent` | Links, primary buttons (post-Sign-In-button update), the orb itself, the wordmark |
| `--gw-text-muted` | `#9aa` | URL line / secondary text on the banner. |

When the brand pass happens for real, the way to re-theme everything is one
edit to `gwRootVars` plus rebuilding this kit. The kit's SVGs reference
`#79c0ff` literally (SVG doesn't read CSS variables); the rebuild step is
"sed the new accent through the four `.svg` files + re-run the
`rsvg-convert` lines below."

## Bio paragraph (paste into every social profile)

> Deterministic policy gate for git pushes: your AI agent commits,
> nimblegate decides what reaches your real repo. Self-hosted, PolyForm Noncommercial.

(157 chars; fits X bio at 160-char limit, LinkedIn tagline, dev.to bio,
Bluesky description, GitHub org description.)

## One-line tagline (paste into headline / display-name slots)

> Git push guardrails for AI agents.

(35 chars; fits headlines under any platform limit, matches the README
tagline + the header banner artwork.)

## Default URL (every profile's website field)

> `https://nimblegate.com`

(NOT the GitHub URL. The landing page funnels traffic to GitHub; the GitHub
URL doesn't funnel back to the brand. Always link people to the brand-owned
surface first.)

## Default pinned post (one per platform that has the concept)

> Hello. nimblegate is a deterministic policy gate for git pushes,
> designed for the AI-agent era. Built in the open.
>
> Repo: github.com/nimblegate/nimblegate
> Site: nimblegate.com

(Same copy on X, Bluesky, LinkedIn, dev.to. Pinned, not regularly posted.
Active posting is a separate decision; this just makes the profile look
intentional instead of parked.)

## Rebuilding the kit (when you change the palette or font)

The SVGs are hand-edited; PNGs are derived via `rsvg-convert`. To re-export
everything after editing an SVG:

```sh
cd docs/brand
rsvg-convert -w 512 -h 512 icon.svg     -o icon-512.png
rsvg-convert -w 32  -h 32  icon.svg     -o icon-32.png
rsvg-convert -w 16  -h 16  icon.svg     -o favicon-16.png
rsvg-convert -w 512        wordmark.svg -o wordmark-512.png
rsvg-convert -w 400 -h 400 avatar.svg   -o avatar.png
rsvg-convert -w 1500 -h 500 header.svg  -o header.png
rsvg-convert -w 2048 -h 1152 header.svg -o header-yt.png
```

If `rsvg-convert` isn't installed: `apt install librsvg2-bin` (Debian/Ubuntu)
or `brew install librsvg` (macOS).

## What's NOT in this kit yet (deliberately deferred)

- **ICO format favicon**. Modern browsers accept PNG favicons just fine
  (`<link rel="icon" href="icon-32.png">`); a true `.ico` with multi-size
  embedded variants is needed only for IE / very old browsers. Skip unless
  analytics shows that audience.
- **Social-card OG image** (1200×630 for repo / landing-page sharing).
  Lives in Step 1.4 of the launch playbook: separate composition with the
  README screenshot, not a pure brand asset.
- **Demo GIF**. Lives in Step 1.2 of the launch playbook (the agent-fix-loop
  10-second clip).
- **Designer-pass logomark**. The particle orb is a strong first pass but
  has no proprietary character: anyone could compute the same Fibonacci
  sphere. If the brand grows enough to warrant differentiation, a designer
  iteration is worth a few hundred dollars; before that point, ship what's
  here.

## How this kit gets used at launch

Per launch playbook Step 3.5.5 (this kit) → 3.6 (reserve handles) → 3.7
(populate each reserved profile with these assets). Every Tier 1 + Tier 2
social account uploads `avatar.png` + `header.png`, pastes the bio + tagline
+ URL, posts the default pinned post once. The brand reads as coherent
across platforms because the kit is one source of truth, not seven
hand-tweaked variants.
