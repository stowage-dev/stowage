// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { ApiClient } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = new ApiClient(fetch);
	try {
		const credentials = await api.listMyS3Credentials();
		return { credentials, error: null as string | null };
	} catch (err) {
		return {
			credentials: [],
			error: err instanceof Error ? err.message : 'Failed to load credentials.'
		};
	}
};
