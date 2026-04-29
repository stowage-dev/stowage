---
type: how-to
---

# Frontend conventions

The frontend is SvelteKit + Svelte 5 + Tailwind v4. The project
opinions are deliberate and tight; PRs that fight them get pushed
back.

## Toolchain

- **Bun** is the only supported package manager. Don't swap it for
  npm, pnpm, yarn, or hand-rolled Vite config.
- The official **SvelteKit scaffolders** are the source of truth for
  project shape. Don't reorganise routes, hooks, or `app.html`
  outside what `npx sv` would produce.

## Svelte 5 runes only

```svelte
<script lang="ts">
  let count = $state(0);
  let doubled = $derived(count * 2);

  $effect(() => {
    console.log('count is', count);
  });
</script>
```

No legacy `let count = 0` reactive declarations, no `$:` blocks in
new code.

## Stack

| Concern | Choice |
|---|---|
| Fetching | `@tanstack/svelte-query` |
| Schema validation | `zod` |
| Icons | `lucide-svelte` |
| Toasts | `svelte-sonner` |
| Styling | Tailwind v4 |
| Stores | `web/src/lib/stores/*.svelte.ts` |

## Routing

SvelteKit file-based routing under `web/src/routes/`:

```
admin/         /admin/* (admin-only views)
b/             /b/[backend]/[bucket]/* — the object browser
backends/      backend index
login/         /login
me/            self-service settings
s/             /s/[code] — public share recipient (the JSON+bytes
               plumbing lives in the Go backend; this is the SPA shell)
search/        /search — cross-backend search
shares/        share management
```

## Stores

`web/src/lib/stores/*.svelte.ts`. Use `$state` runes for stateful
stores, exported as plain values:

```ts
// web/src/lib/stores/session.svelte.ts
export const session = $state<{ user: User | null }>({ user: null });
```

## Linting and type checking

```sh
cd web
bun run lint    # prettier + eslint
bun run check   # svelte-check with the project tsconfig
```

CI runs both. PRs failing either are blocked.

## Where the frontend talks to the backend

- Every API call goes via `web/src/lib/api.ts`. It handles CSRF
  header injection, response error parsing, and zod validation.
- Don't fetch directly from components. Wrap new endpoints in
  `api.ts` so the CSRF + validation layer applies uniformly.

## Build

```sh
cd web
bun install --frozen-lockfile
bun run build
```

Output lands in `web/dist/`. The Go binary embeds it via
`web/embed.go` (`//go:embed all:dist`). The Go build will fail if
`web/dist/index.html` doesn't exist.

## Adding a new route

1. Drop a `+page.svelte` (and `+page.ts` if you need data loading)
   under `web/src/routes/`.
2. Add the API call to `web/src/lib/api.ts` if it talks to a new
   backend handler.
3. Use existing components from `web/src/lib/components/` — most UI
   primitives are already there.
4. Run `bun run check` and `bun run lint`.

## What not to do

- No legacy reactive blocks (`$:`).
- No new state-management libraries — runes cover it.
- No CSS frameworks other than Tailwind v4.
- No new fonts without a strong reason — current bundle keeps
  loading-time small.
- No `dangerouslySetInnerHTML` equivalents (`{@html ...}` is fine
  with reviewed input; not for user-controlled content).
