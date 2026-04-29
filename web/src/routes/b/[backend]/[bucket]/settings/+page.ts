// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { ApiClient } from '$lib/api';
import type { BucketQuota, BucketSizeTracking, CORSRule, LifecycleRule } from '$lib/types';
import type { PageLoad } from './$types';

export type Loadable<T> = { value: T | null; error: string | null };

function loadable<T>(p: Promise<T>): Promise<Loadable<T>> {
	return p
		.then((value) => ({ value, error: null }))
		.catch((e: Error) => ({ value: null, error: e.message }));
}

const empty = <T>(): Promise<Loadable<T>> => Promise.resolve({ value: null, error: null });

export const load: PageLoad = async ({ fetch, params, parent }) => {
	const api = new ApiClient(fetch);
	const layout = await parent();
	const backend = layout.backends.find((b) => b.id === params.backend);
	const caps = backend?.capabilities;

	// Kick off every fetch immediately but DON'T await — return the promises
	// so the page can render skeletons while each section loads independently.
	// Per-section failures are folded into the resolved value as `error` so
	// one missing endpoint doesn't blank the whole page.
	return {
		backend,
		bucket: params.bucket,
		versioning: caps?.versioning
			? loadable(api.getBucketVersioning(params.backend, params.bucket))
			: empty<boolean>(),
		cors: caps?.cors
			? loadable(api.getBucketCORS(params.backend, params.bucket))
			: empty<CORSRule[]>(),
		policy: caps?.bucket_policy
			? loadable(api.getBucketPolicy(params.backend, params.bucket))
			: empty<string>(),
		lifecycle: caps?.lifecycle
			? loadable(api.getBucketLifecycle(params.backend, params.bucket))
			: empty<LifecycleRule[]>(),
		quota: loadable(api.getBucketQuota(params.backend, params.bucket)),
		sizeTracking: loadable(api.getBucketSizeTracking(params.backend, params.bucket))
	};
};
