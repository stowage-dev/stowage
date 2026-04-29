// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { Route } from './types';
import type { Page } from '@sveltejs/kit';

/**
 * Map the SvelteKit URL into the internal Route object the components were built around.
 * Centralised so screens stay declarative.
 */
export function routeFromPage(page: Page): Route {
	const p = page.url.pathname;
	if (p === '/' || p === '/backends') return { type: 'backends' };
	if (p === '/shares') return { type: 'shares' };
	if (p === '/search') return { type: 'search' };
	if (p.startsWith('/admin/users')) return { type: 'admin-users' };
	if (p.startsWith('/admin/audit')) return { type: 'admin-audit' };
	if (p.startsWith('/admin/health')) return { type: 'admin-health' };
	if (p.startsWith('/admin/s3-proxy')) return { type: 'admin-s3-proxy' };
	if (p.startsWith('/admin/dashboard')) return { type: 'admin-dashboard' };
	if (p.startsWith('/me/s3-credentials')) return { type: 'me-s3-credentials' };
	if (p.startsWith('/b/')) {
		const parts = p.split('/').filter(Boolean); // ["b", backend, bucket?, ...prefix]
		const backend = parts[1];
		if (parts.length === 2) return { type: 'backend', backend };
		const bucket = parts[2];
		const prefix = parts.slice(3);
		return { type: 'bucket', backend, bucket, prefix };
	}
	return { type: 'backends' };
}

export function urlForRoute(r: Route): string {
	switch (r.type) {
		case 'backends':
			return '/backends';
		case 'backend':
			return `/b/${encodeURIComponent(r.backend)}`;
		case 'bucket': {
			const segs = r.prefix.map((s) => encodeURIComponent(s)).join('/');
			return `/b/${encodeURIComponent(r.backend)}/${encodeURIComponent(r.bucket)}${segs ? '/' + segs : ''}`;
		}
		case 'shares':
			return '/shares';
		case 'admin-users':
			return '/admin/users';
		case 'admin-audit':
			return '/admin/audit';
		case 'admin-dashboard':
			return '/admin/dashboard';
		case 'admin-health':
			return '/admin/health';
		case 'admin-s3-proxy':
			return '/admin/s3-proxy';
		case 'me-s3-credentials':
			return '/me/s3-credentials';
		case 'search':
			return '/search';
	}
}
