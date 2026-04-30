// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { browser } from '$app/environment';

export type Density = 'compact' | 'cosy' | 'roomy';
export type SidebarStyle = 'nested' | 'flat';
export type TopbarStyle = 'breadcrumb' | 'search';

export interface Tweaks {
	density: Density;
	sidebarStyle: SidebarStyle;
	topbarStyle: TopbarStyle;
	showUploadQueue: boolean;
}

const DEFAULTS: Tweaks = {
	density: 'cosy',
	sidebarStyle: 'nested',
	topbarStyle: 'search',
	showUploadQueue: true
};

function read(): Tweaks {
	if (!browser) return DEFAULTS;
	try {
		const raw = localStorage.getItem('stw-tweaks');
		if (!raw) return DEFAULTS;
		return { ...DEFAULTS, ...JSON.parse(raw) };
	} catch {
		return DEFAULTS;
	}
}

export const tweaks = $state<Tweaks>(read());

export function setTweak<K extends keyof Tweaks>(key: K, value: Tweaks[K]): void {
	tweaks[key] = value;
	if (browser) {
		try {
			localStorage.setItem('stw-tweaks', JSON.stringify(tweaks));
		} catch {
			/* ignore */
		}
	}
}
