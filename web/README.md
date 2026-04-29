# web/

SvelteKit (Svelte 5, runes mode) + TypeScript frontend for stowage.
Scaffolded from the React prototype in `docs/design/`.

## Stack

- SvelteKit + `@sveltejs/adapter-static` — output to `web/dist/`, embedded into the
  Go binary via `web/embed.go`.
- TypeScript with Svelte 5 runes (`$state`, `$derived`, `$effect`, `$props`).
- Tailwind v4 (utility classes when needed; the design system itself runs on the
  CSS custom properties in `src/lib/styles/stowage.css`).
- `lucide-svelte` for icons.
- `svelte-sonner` for toasts.
- `@tanstack/svelte-query` and `zod` (wired as deps; Phase 2+ will use them).

## Develop

```sh
bun install      # already run by the scaffolder
bun run dev      # vite dev server
bun run check    # svelte-check
bun run build    # vite build → web/dist/
bun run lint     # prettier --check + eslint
```

The Go server (`make run`) serves whatever is currently in `web/dist/` via the
embed in `web/embed.go`. Run `make frontend` (or `bun run build`) before
`make build` whenever you change frontend sources.

## Layout

```
src/
├── app.css                        tailwind import + design-token bridge
├── app.html                       sets data-theme synchronously to avoid flash
├── lib/
│   ├── api.ts                     fetch client (CSRF + JSON envelope)
│   ├── i18n.ts                    error code → user copy
│   ├── format.ts                  bytes / num / middleEllipsis
│   ├── route.ts                   URL ↔ Route object
│   ├── types.ts                   Backend, Bucket, S3Object, User, Share, Route…
│   ├── data/mock.ts               mock data ported from docs/design/src/data.jsx
│   ├── stores/
│   │   ├── theme.svelte.ts        light/dark with localStorage
│   │   ├── tweaks.svelte.ts       density / view / sidebar style
│   │   ├── uploads.svelte.ts      simulated upload queue
│   │   └── shell.svelte.ts        palette, share modal, banner, auth…
│   ├── styles/stowage.css         design tokens (verbatim from docs/design)
│   └── components/
│       ├── ui/                    Button, Chip, Badge, Toggle, BackendMark…
│       ├── shell/                 Sidebar, TopBar, Breadcrumb, banner
│       ├── browser/               BucketBrowser, ObjectTable, DetailDrawer…
│       ├── screens/               BackendsPage, SharesPage, AdminUsersPage…
│       ├── share/ShareModal.svelte
│       ├── upload/UploadQueue.svelte
│       └── command/CommandPalette.svelte
└── routes/
    ├── +layout.{svelte,ts}        shell + overlays + global shortcuts
    ├── +page.svelte               redirects to /backends
    ├── backends/+page.svelte
    ├── b/[backend]/+page.svelte
    ├── b/[backend]/[bucket]/+page.svelte
    ├── b/[backend]/[bucket]/[...prefix]/+page.svelte
    ├── shares/+page.svelte
    ├── uploads/+page.svelte
    └── admin/{users,audit,backends}/+page.svelte
```

The static adapter uses an `index.html` fallback so dynamic routes
(`/b/:backend/:bucket/...`) resolve client-side from any hit. The placeholder
`dist/index.html` is kept in git so `go build` works on a fresh clone before
anyone runs `bun run build`.

## Recreating the scaffold

```sh
bun x sv@0.15.1 create --template minimal --types ts \
  --add prettier eslint tailwindcss="plugins:none" \
       sveltekit-adapter="adapter:static" \
  --no-download-check --install bun web
```

## Status

Phase 0 scaffold. The backend wiring (real `$lib/api.ts` calls, OIDC config
fetch, capability gating from live `/api/backends`) lands in subsequent phases —
the components are built against the mock data in `src/lib/data/mock.ts` so the
shape matches the JSON API in `internal/api/`.
