// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { ApiClient } from '$lib/api';
import type { BackendHealth } from '$lib/types';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = new ApiClient(fetch);
	try {
		const backends = await api.listBackendHealth();
		return { backends, error: null as string | null };
	} catch (err) {
		return {
			backends: [] as BackendHealth[],
			error: err instanceof Error ? err.message : 'load failed'
		};
	}
};
