# UX proposal: Tailwind and visual direction

**Goal:** Make Ditto’s UI feel more professional by adopting real Tailwind CSS and choosing a clear visual direction. This doc proposes **three options** so we can pick one (or mix) before implementing.

---

## Current state (short)

- **Stack:** Go templates, hand-written CSS that mimics Tailwind class names, HTMX for scan status, vanilla JS for API-driven home/scans.
- **Screens:** Layout (nav + main), Home (duplicate groups + summary), Scans (roots, recent scans table), Scan progress (status table + CSV link), Duplicate groups (by hash/inode, file lists).
- **Pain points:** Styling is a custom subset; no real design system, so the UI reads as “default gray forms and tables.” Nav (slate/purple) is the only strong visual identity.

---

## Proposed directions

Three distinct directions. Each uses **real Tailwind** (build step, one `app.css`), keeps your existing structure (layout, templates, HTMX, JS), and improves hierarchy, spacing, and components.

---

### Option A — **“Clean dashboard”** (neutral, data-first)

**Vibe:** Calm, trustworthy, “admin tool.” Lots of white/light gray, clear typography, subtle borders and shadows. Feels like a small internal dashboard (e.g. Backstage, Grafana).

- **Palette:** White / gray-50–100 backgrounds; gray-700–900 text; blue-600 primary (links, primary buttons); keep nav slate in prod, purple in dev.
- **Typography:** System font stack or **Inter** (clean, readable). Slightly larger base (e.g. 16px), clear heading scale (text-xl → text-3xl).
- **Components:**
  - **Cards:** White bg, `rounded-lg`, light `shadow-sm`, `border border-gray-200`. Section headers inside cards: `text-sm font-semibold text-gray-500 uppercase tracking-wide`.
  - **Tables:** Same card container; header row `bg-gray-50`; row hover `hover:bg-gray-50`; no heavy borders, use `divide-y divide-gray-200`.
  - **Buttons:** Primary `bg-blue-600 hover:bg-blue-700`; secondary `border border-gray-300 bg-white hover:bg-gray-50`. Slightly larger tap targets (`px-4 py-2.5`).
  - **Summary block:** Light blue-gray tint (`bg-slate-50` or `bg-blue-50/50`) instead of plain gray, so it reads as “key metric” not “another box.”
- **Best for:** “We want it to look solid and professional without standing out.”

---

### Option B — **“Dark sidebar”** (product-style)

**Vibe:** Modern SaaS: dark nav/sidebar, light content area. Strong contrast and a clear “chrome vs content” split.

- **Palette:** Nav/sidebar: dark (e.g. gray-900 or slate-900). Content: white or gray-50. Primary: blue-500–600 or a single accent (e.g. emerald) for actions. Version badge: muted (gray-500 in nav).
- **Typography:** Same as A (Inter or system). Optional: slightly bolder nav labels.
- **Components:**
  - **Nav:** Full-width dark bar; links white/gray-300 with `hover:bg-white/10` or `hover:text-white`. Optional: light border-b or shadow under nav.
  - **Cards/tables:** Same as A but on light content area so they pop. Optional: one accent color for “Start scan” / “Add” (e.g. green or blue).
  - **Empty states:** Short copy + one clear CTA; optional icon or illustration placeholder.
- **Best for:** “We want it to feel like a real product, not an internal script.”

---

### Option C — **“Minimal with accent”** (distinct, low-noise)

**Vibe:** Plenty of whitespace, minimal chrome, one strong accent (e.g. teal or indigo) used sparingly for links and primary actions. Feels “designed” but not busy.

- **Palette:** Mostly white and gray-50; text gray-800/600. **Single accent** (e.g. teal-600 or indigo-600) for: primary buttons, links, key labels, optional thin accent line under nav or on card borders.
- **Typography:** Slightly more character—e.g. **DM Sans** or **Outfit** for headings, system for body. Or keep system and use weight/size for hierarchy.
- **Components:**
  - **Nav:** Light (white or gray-100) with subtle bottom border; accent color for “Ditto” and active/hover. Dev mode: small accent pill/badge instead of full purple bar.
  - **Cards:** White, generous padding, `rounded-xl`, very light shadow or border. Accent used for “View group details” and “Start scan” only.
  - **Tables:** Minimal: no card border or very light; row hover only. Accent for “View files” links.
  - **Summary:** One line with accent on the number (e.g. “**12** duplicate groups · …” in accent color).
- **Best for:** “We want a clear identity without a full dark theme.”

---

## Comparison

| Aspect            | A — Clean dashboard | B — Dark sidebar   | C — Minimal accent   |
|------------------|---------------------|--------------------|----------------------|
| Nav              | Slate/purple (current) | Dark bar           | Light + accent      |
| Content area     | Light gray          | White / gray-50    | White, lots of space |
| Primary actions  | Blue                | Blue or green      | Single accent (teal/indigo) |
| Tables/cards     | Classic, bordered   | Same, in light area| Minimal, soft       |
| “Feels like”     | Internal dashboard  | SaaS product       | Focused tool        |

---

## Recommendation and next steps

- **If you want “professional and safe”:** Option **A** is the smallest jump from today and gets you real Tailwind + better hierarchy and components.
- **If you want a stronger product feel:** Option **B** (dark nav) is a clear, one-time shift; we keep content area light so tables stay readable.
- **If you want a distinct look without going dark:** Option **C** gives you a clear accent and a more “designed” feel with minimal change to layout.

**Next steps I suggest:**

1. **Choose one option** (or e.g. “A + dark nav from B” or “C with Inter from A”). We can mix (e.g. A’s components + B’s nav).
2. **Add Tailwind for real:** `tailwind.config.js`, `input.css` with `@tailwind base/components/utilities`, content = `./internal/server/templates/**/*.html` and `./internal/server/static/*.js`. Build: `npx tailwindcss -i ... -o internal/server/static/app.css`. Replace current `app.css` with build output (and keep dev/prod nav classes in templates).
3. **Apply the chosen direction** to layout and one page (e.g. Home) first, then Scans, progress, duplicates. We can do it incrementally so you can review each step.

If you’d like, next I can:
- **Sketch a static HTML preview** (one file with nav + card + table + buttons in each of A/B/C) so you can open it in a browser and compare, or  
- **Implement Option A (or your pick)** in the repo: Tailwind setup + layout + home page, so you can run the app and tweak from there.

Tell me which option (or mix) you prefer and whether you want the preview file or to go straight to implementation.
