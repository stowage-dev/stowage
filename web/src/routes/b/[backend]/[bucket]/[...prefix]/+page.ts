// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { ApiClient } from '$lib/api';
import type { BucketQuota } from '$lib/types';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	const api = new ApiClient(fetch);
	const backendId = params.backend;
	const bucket = params.bucket;
	const prefix = (params.prefix ?? '').split('/').filter(Boolean);
	const s3Prefix = prefix.length ? prefix.join('/') + '/' : '';
	const [listingRes, quotaRes] = await Promise.all([
		api.listObjects(backendId, bucket, { prefix: s3Prefix }).catch((e: Error) => e),
		api.getBucketQuota(backendId, bucket).catch(() => null)
	]);
	const listing = listingRes instanceof Error ? null : listingRes;
	const error = listingRes instanceof Error ? listingRes.message : null;
	const quota: BucketQuota | null = quotaRes;
	return { backendId, bucket, prefix, listing, quota, error };
};
