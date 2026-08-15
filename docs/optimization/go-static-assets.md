# Go Static Assets

**Date:** 2026-08-14
**Baseline:** none — `/docs/optimization/audit-baseline.md` has not been run
**Action taken:** Audit + content fingerprinting implemented

## Summary

| Check | Before | After |
|-------|--------|-------|
| Assets served via embed.FS | ✗ | ✗ (deliberate — see Notes) |
| Content hashing | ✗ | ✓ |
| Cache-Control on assets | ✗ | ✓ |
| Template references via helper | ✗ | ✓ |
| HTML Cache-Control set | ✗ | ✓ |

## What prompted this

A CSS fix deployed successfully (GitHub Actions run `31769130738`, tag
`v2.0.1`) but stayed invisible on the live site. The origin served the new
file; Cloudflare served the March build from its edge cache:

```
cf-cache-status: HIT
age: 1835
cache-control: max-age=14400
last-modified: Wed, 18 Mar 2026 17:14:38   ← previous build
```

`/static/css/main.css` never changes name, so the edge had no way to know the
bytes behind it had changed. Every deploy touching CSS had the same 4-hour lag.

## Current State (after)

- **Static handler:** `internal/handlers/assets.go` — `Handler.StaticHandler()`
- **Registration:** `cmd/server/main.go:50`
- **Asset source:** `http.Dir(cfg.StaticDir)`
- **Fingerprint index:** built at startup by `newAssetIndex`, SHA-256 truncated
  to 8 hex characters, inserted before the extension
- **Template helper:** `{{asset "css/main.css"}}` → `/static/css/main.ba010de3.css`
- **Template references:** 28, all migrated (0 literal `/static/` paths remain)

## Changes made

| File | Change |
|------|--------|
| `internal/handlers/assets.go` | New. Index, `AssetURL`, `StaticHandler` |
| `internal/handlers/handlers.go` | `staticDir`/`assets` fields; single shared `funcMap()` replacing four duplicates; `Cache-Control: no-cache` on HTML |
| `internal/config/config.go` | `StaticDir` (env `STATIC_DIR`, default `static`) |
| `cmd/server/main.go` | Uses `h.StaticHandler()`; root files read from `cfg.StaticDir` |
| `templates/*` | 28 asset references migrated to `{{asset}}` |
| `CLAUDE.md` | Static Assets section |

## Cache-Control policy

| Response | Header | Why |
|----------|--------|-----|
| Hashed asset | `public, max-age=31536000, immutable` | Name is derived from content; it can never be stale |
| Plain asset name | `public, max-age=300` | Content under this name can change |
| HTML | `no-cache` | Names the hashed assets; must stay fresh |
| Any asset in dev | `no-store` | Local iteration, no CDN in front |

Both names serve the same file. A browser tab holding an old page across a
deploy keeps working — the previous hashed URL is not withdrawn until the next
build changes those bytes.

## Verification

Against a local `APP_ENV=production` server:

- Hashed URL → 200, `public, max-age=31536000, immutable`
- Plain URL → 200, `public, max-age=300`
- Wrong hash (`main.deadbeef.css`) → 404
- HTML → `Cache-Control: no-cache`
- Path traversal (`/static/../go.mod`, encoded variant) → 404
- `/favicon.ico`, `/robots.txt` → 200
- All 11 page routes → 200; every `/static/` URL they emit → 200
- Appending a byte to `main.css` changed the hash `ba010de3` → `36aed2b1`
- `APP_ENV=development` emits plain URLs with `no-store`

## Also fixed

**Directory listings were public.** `http.FileServer` renders a browsable index
for any directory path. `https://www.southhillscoc.org/static/css/` returned a
listing of the whole directory — including `input.css`, the uncompiled Tailwind
source, sitting next to the built stylesheet. Pre-existing, not introduced
here, but the handler was being rewritten anyway. Requests for a directory path
now return 404.

`og:image` and `twitter:image` in `templates/base.html` pointed at
`{{.Config.BaseURL}}images/og-image.jpg` — a 404, since images are served under
`/static/`. Confirmed 404 on the live site, 200 at the `/static/` path. Now
`{{.Config.BaseURL}}static/images/og-image.jpg`. Left unhashed and absolute:
social crawlers want a stable URL.

## Notes

**embed.FS was not adopted.** The skill's recommended handler embeds assets in
the binary. That does not fit this Dockerfile: Tailwind compiles `main.css` in
stage 1, the Go binary builds in stage 2 from the repo (where `main.css` is
gitignored and absent), and stage 3 copies the compiled CSS in alongside the
binary. `//go:embed` would bake in a missing or stale stylesheet. Adopting it
means restructuring the Dockerfile to copy the built CSS into the Go stage
before `go build`. Worth doing — it would also let the fingerprint index be
built at compile time — but it is a separate change with its own deploy risk.

The 9.6 MB of images under `static/images/` would also move into the binary.

**Hash length.** 8 hex characters across ~60 assets. Collision risk is far
below the risk of the deploy itself failing.

**Startup cost.** Hashing 9.6 MB at boot is a few tens of milliseconds, once.

## Follow-ups

- [ ] Run `/audit-baseline` — this project has no baseline recorded
- [ ] Run `/caddy-config-review` to confirm Caddy passes Cache-Control through
      rather than overriding it
- [ ] Consider the embed.FS migration with the Dockerfile restructure
- [ ] One-time Cloudflare purge of `/static/css/main.css` — the old plain-name
      object is still cached at the edge with a 4-hour TTL
