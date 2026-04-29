// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { AuthConfig, Backend, Me } from '$lib/types';

// See https://svelte.dev/docs/kit/types#app.d.ts
declare global {
	namespace App {
		interface PageData {
			authConfig: AuthConfig | null;
			me: Me | null;
			backends: Backend[];
		}
	}
}

export {};
