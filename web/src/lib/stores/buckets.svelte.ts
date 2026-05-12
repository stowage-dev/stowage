// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { SvelteMap, SvelteSet } from 'svelte/reactivity';

import { ApiClient } from '$lib/api';
import type { Backend, Bucket } from '$lib/types';

export type BucketState =
	| { status: 'loading' }
	| { status: 'ok'; buckets: Bucket[]; loadedAt: number }
	| { status: 'error'; message: string };

const state = $state<Record<string, BucketState>>({});
const inflight = new SvelteMap<string, AbortController>();
const api = new ApiClient();

// One slow/dead backend mustn't gate the rest of the UI on its TCP timeout —
// we cap the per-backend wait here so the user always sees an `unreachable`
// state within a bounded window. The proxy itself may still be working its way
// through a real probe; this only affects what the UI displays.
const FETCH_TIMEOUT_MS = 8000;

/** Reactive snapshot for a backend id. Unknown ids are treated as still loading. */
export function bucketState(id: string): BucketState {
	return state[id] ?? { status: 'loading' };
}

/** Convenience: returns the bucket array if currently `ok`, otherwise null. */
export function bucketList(id: string): Bucket[] | null {
	const s = state[id];
	return s?.status === 'ok' ? s.buckets : null;
}

async function fetchOne(id: string): Promise<void> {
	inflight.get(id)?.abort();
	const ac = new AbortController();
	inflight.set(id, ac);
	const timer = setTimeout(() => ac.abort(), FETCH_TIMEOUT_MS);
	const prev = state[id];
	// Keep stale data visible while a refresh is in flight; only flip to
	// `loading` on the first load or after a previous error so the sidebar
	// doesn't flicker on every navigation.
	if (!prev || prev.status === 'error') {
		state[id] = { status: 'loading' };
	}
	try {
		const buckets = await api.listBuckets(id, { signal: ac.signal });
		if (ac.signal.aborted) return;
		state[id] = { status: 'ok', buckets, loadedAt: Date.now() };
	} catch (err) {
		if (ac.signal.aborted) {
			state[id] = { status: 'error', message: 'Backend unreachable (timed out)' };
			return;
		}
		state[id] = {
			status: 'error',
			message: err instanceof Error ? err.message : 'Backend unreachable'
		};
	} finally {
		clearTimeout(timer);
		if (inflight.get(id) === ac) inflight.delete(id);
	}
}

/**
 * Idempotently kick off a fetch for any backend we haven't tried yet.
 * `ok` and `error` are both terminal here — the layout effect re-runs on
 * every state mutation, so retrying errored ids would loop forever against
 * an offline backend. Recovery goes through `refreshBuckets` (Retry button,
 * explicit user action) instead.
 */
export function primeBuckets(backends: Backend[]): void {
	for (const b of backends) {
		const s = state[b.id];
		if (s?.status === 'ok' || s?.status === 'error') continue;
		if (inflight.has(b.id)) continue;
		void fetchOne(b.id);
	}
}

/** Force a refresh of one backend, or every cached id if none specified. */
export function refreshBuckets(id?: string): void {
	if (id) {
		void fetchOne(id);
		return;
	}
	for (const k of Object.keys(state)) void fetchOne(k);
}

/** Drop cache entries for backends that no longer exist. */
export function reconcileBackends(ids: Iterable<string>): void {
	const known = new SvelteSet(ids);
	for (const id of Object.keys(state)) {
		if (!known.has(id)) {
			inflight.get(id)?.abort();
			inflight.delete(id);
			delete state[id];
		}
	}
}
