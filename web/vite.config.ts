// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

const backend = process.env.STOWAGE_BACKEND ?? 'http://localhost:8080';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	server: {
		port: 5173,
		strictPort: true,
		proxy: {
			'/api': { target: backend, changeOrigin: false, ws: true },
			'/auth': { target: backend, changeOrigin: false, ws: true },
			'/healthz': { target: backend, changeOrigin: false },
			'/readyz': { target: backend, changeOrigin: false },
			// The bare /s/<code> URL is rendered by SvelteKit; only the JSON
			// + bytes sub-paths proxy through to the Go server. Without this
			// scoping, Vite would forward the page request and the recipient
			// would get a raw download page instead of the preview UI.
			//
			// http-proxy-middleware tests the regex against `req.url`, which
			// includes the query string — so a trailing `$` makes the rule
			// miss any request that carries `?inline=1` (the preview tags
			// always do). Match either end-of-URL or `?` instead.
			'^/s/[^/]+/(info|unlock|raw)($|\\?)': {
				target: backend,
				changeOrigin: false
			}
		}
	}
});
