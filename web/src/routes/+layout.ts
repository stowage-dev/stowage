// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { redirect } from '@sveltejs/kit';
import type { LayoutLoad } from './$types';
import { ApiClient } from '$lib/api';
import { setSession } from '$lib/stores/session.svelte';
import type { Backend, BucketPin } from '$lib/types';

// Single-page app: every route renders client-side.
export const ssr = false;
export const prerender = false;
export const trailingSlash = 'never';

const PUBLIC_ROUTES = ['/login', '/login/callback', '/s'];

function isPublic(pathname: string): boolean {
	return PUBLIC_ROUTES.some((p) => pathname === p || pathname.startsWith(p + '/'));
}

export const load: LayoutLoad = async ({ fetch, url }) => {
	const api = new ApiClient(fetch);
	const [authConfig, me] = await Promise.all([api.authConfig(), api.me()]);
	setSession(authConfig, me);

	if (!me && !isPublic(url.pathname)) {
		throw redirect(307, '/login?next=' + encodeURIComponent(url.pathname + url.search));
	}
	if (me && url.pathname === '/login') {
		throw redirect(307, '/');
	}

	// Per-backend bucket lists are no longer awaited here — a single slow
	// backend used to block every navigation while the layout load resolved.
	// The bucket store (`$lib/stores/buckets`) fetches them in the background
	// per backend and exposes loading/error/ok state to consumers.
	let backends: Backend[] = [];
	let pins: BucketPin[] = [];
	if (me) {
		try {
			backends = await api.listBackends();
		} catch {
			// 401/forbidden — leave empty; the sidebar will show nothing.
			backends = [];
		}
		try {
			pins = await api.listPins();
		} catch {
			pins = [];
		}
	}

	return { authConfig, me, backends, pins };
};
