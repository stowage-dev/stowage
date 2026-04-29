// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { AuthConfig, Me } from '$lib/types';

interface SessionState {
	loaded: boolean;
	authConfig: AuthConfig | null;
	me: Me | null;
}

export const session = $state<SessionState>({
	loaded: false,
	authConfig: null,
	me: null
});

export function setSession(authConfig: AuthConfig, me: Me | null): void {
	session.authConfig = authConfig;
	session.me = me;
	session.loaded = true;
}

export function clearMe(): void {
	session.me = null;
}
