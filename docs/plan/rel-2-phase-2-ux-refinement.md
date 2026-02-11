# Release 2, Phase 2: UX refinement

**Goal:** Polish UI/UX based on usage; small improvements that improve usability without changing the core flow. This phase was originally planned as Release 1, Phase 7 and was not started before the 0.2 pivot.

**Context:** After Phase 1 (PostgreSQL and new data model), the app has the same capabilities as 0.1 but on the new stack. This phase addresses UX debt and incremental improvements identified from real use: layout, copy, loading states, accessibility, or small workflow tweaks. It can drive multiple minor releases (e.g. v0.2.1, v0.2.2) once the 0.2 release is out.

**References:** [ADR-004](../decisions/ADR-004-htmx-tailwind.md) (HTMX, Tailwind). Builds on the existing UI shell and scan/duplicate pages.

---

## TDD and review

- Changes are incremental; each improvement can be a small PR with manual or automated UI checks as appropriate.
- No new ADRs required unless a refinement implies a design change (e.g. new interaction pattern).

---

## Scope (to be refined with usage)

- **Layout and copy:** Improve labels, headings, and page structure for clarity.
- **Loading and feedback:** Better loading states, empty states, or error messages where the current UI is lacking.
- **Accessibility:** Keyboard navigation, focus management, or screen-reader improvements as needed.
- **Small workflow tweaks:** E.g. “back” navigation, breadcrumbs, or filters that make common tasks faster.

Detailed steps will be added as we identify concrete improvements (e.g. from user feedback or dogfooding after 0.2.0).
