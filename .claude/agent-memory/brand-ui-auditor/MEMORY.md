# Brand & UI Auditor — Agent Memory

## South Hills Church of Christ — Key Project Facts

- Hugo static site, no Tailwind. Uses handwritten CSS with custom properties in `:root`.
- CSS file: `assets/css/main.css` — ALL color/typography is defined via CSS custom properties here.
- Templates: `layouts/` — Hugo template hierarchy, no component framework.
- Brand guide: `/home/dukerupert/Repos/south-hills-coc/brand-guide.md`
- Audit report: `/home/dukerupert/Repos/south-hills-coc/site-audit.md`

## Critical Pattern: CSS-Variable Projects

When a project uses CSS custom properties (`:root { --color-primary: ... }`) instead of
Tailwind utility classes, the ENTIRE audit must evaluate the custom property definitions
first — not individual element rules. A single wrong root variable cascades to every
downstream rule. Always read `:root` block before any individual class.

## South Hills — Palette Mismatch (Audited 2026-02-21)

The site was built with a WRONG palette:
- Current `--color-primary`: `#1a365d` (navy) — NOT in brand guide
- Current `--color-secondary`: `#c4a35a` (gold) — NOT in brand guide
- Brand guide primary: `#975849` (terracotta)
- Brand guide charcoal (headings): `#3F3D3C`
- Brand guide earth brown (footer): `#6B5347`
- Brand guide background-alt: `#FAF7F4` (warm cream) — site uses `#f7fafc` (cold Tailwind gray-50)

## South Hills — Typography Mismatch

- Site uses `--font-heading: Georgia, serif` for ALL headings
- Brand guide: Inter for ALL headings and body; serif (Lora/Merriweather) for scripture ONLY
- Google Fonts import needed in `baseof.html`

## South Hills — Recurring Issues Across All Templates

1. Emoji icons (📖🤝🎵🍞) used everywhere — brand guide mandates Lucide SVG icons
2. Scripture blocks missing on Visit and Ministries pages; no footer verse
3. "Contact Us" CTA used — brand guide forbids it (too cold), prefers "We'd Love to Hear From You"
4. "Learn More" CTA used — explicitly in anti-patterns list
5. YouTube linked but not embedded — anti-pattern
6. "All are welcome" in meta description — anti-pattern
7. "Families Welcome" h2 — should be "What About My Kids?" per CTA language table
8. Lord's Supper unexplained on first use (needs "(Communion)" inline)
9. Contact form JS uses `'green'` and `'red'` instead of `#059669` and `#DC2626`
10. Contact page map iframe missing `title` attribute (visit page has it, contact does not)

## Service Time Discrepancy

Brand guide says Bible Class: 9:00 AM. `hugo.toml` says `bibleClass = '9:30 AM'`.
Must verify actual time with client — site is internally consistent at 9:30 but may differ from real schedule.

## Image Inventory

- Hero: `hero-worship.webp` — real congregation photo, WebP ✓
- Content: `congregation.jpg` — JPG not WebP, missing lazy+srcset
- Leadership: mix of `.jpg`, `.png`, `.webp` — not all WebP
- Orphaned file: `static/images/leadership/chiranjeevi-allada.png` — person removed from YAML

## Hugo-Specific Notes

- Data files drive leadership and ministries: `data/leadership.yaml`, `data/ministries.yaml`
- Service times flow from `hugo.toml` params → templates via `{{ .Site.Params.serviceTimes.* }}`
- Schema.org structured data is in `layouts/partials/schema.html` — correctly implements Church + PlaceOfWorship types
- Font imports must go in `layouts/_default/baseof.html` `<head>` block

## See Also

- `patterns.md` — general patterns across project types (to be created as needed)
