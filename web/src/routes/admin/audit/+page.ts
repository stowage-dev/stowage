// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { ApiClient } from '$lib/api';
import type { AuditEvent } from '$lib/types';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = new ApiClient(fetch);
	try {
		const { events, total } = await api.listAudit({ limit: 200 });
		return { events, total, error: null as string | null };
	} catch (err) {
		return {
			events: [] as AuditEvent[],
			total: 0,
			error: err instanceof Error ? err.message : 'load failed'
		};
	}
};
