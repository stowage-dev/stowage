// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { ApiClient } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = new ApiClient(fetch);
	try {
		const shares = await api.listShares('mine');
		return { shares, error: null as string | null };
	} catch (err) {
		return { shares: [], error: err instanceof Error ? err.message : 'list failed' };
	}
};
