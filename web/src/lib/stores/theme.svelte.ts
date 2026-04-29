// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { browser } from '$app/environment';

type Theme = 'light' | 'dark';

function read(): Theme {
	if (!browser) return 'light';
	const v = localStorage.getItem('stw-theme');
	return v === 'dark' ? 'dark' : 'light';
}

export const theme = $state<{ value: Theme }>({ value: read() });

export function setTheme(v: Theme): void {
	theme.value = v;
	if (browser) {
		document.documentElement.dataset.theme = v;
		localStorage.setItem('stw-theme', v);
	}
}

export function toggleTheme(): void {
	setTheme(theme.value === 'dark' ? 'light' : 'dark');
}
