// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { ApiClient } from '$lib/api';
import type { Endpoint } from '$lib/types';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, parent }) => {
	const { me } = await parent();
	if (me?.role !== 'admin') return { admin: null };
	const api = new ApiClient(fetch);
	try {
		const endpoints = await api.listEndpoints();
		return { admin: { endpoints, error: null as string | null } };
	} catch (err) {
		const empty: Endpoint[] = [];
		return { admin: { endpoints: empty, error: err instanceof Error ? err.message : 'list failed' } };
	}
};
