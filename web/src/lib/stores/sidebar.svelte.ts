// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { browser } from '$app/environment';

const SECTIONS_KEY = 'stowage:sidebar:sections';
const BACKENDS_KEY = 'stowage:sidebar:backends';

export type SectionName = 'pinned' | 'backends' | 'workspace' | 'admin';

function read<T>(key: string, fallback: T): T {
	if (!browser) return fallback;
	try {
		const raw = localStorage.getItem(key);
		if (!raw) return fallback;
		const parsed = JSON.parse(raw);
		return { ...(fallback as object), ...parsed } as T;
	} catch {
		return fallback;
	}
}

function write<T>(key: string, value: T): void {
	if (!browser) return;
	try {
		localStorage.setItem(key, JSON.stringify(value));
	} catch {
		/* ignore */
	}
}

/** True when the section is collapsed. Default for every section is expanded (false). */
export const sectionCollapsed = $state<Record<SectionName, boolean>>(
	read(SECTIONS_KEY, { pinned: false, backends: false, workspace: false, admin: false })
);

export function toggleSection(name: SectionName): void {
	sectionCollapsed[name] = !sectionCollapsed[name];
	write(SECTIONS_KEY, sectionCollapsed);
}

/** True when the backend's bucket tree is collapsed. Default is expanded. */
export const backendCollapsed = $state<Record<string, boolean>>(read(BACKENDS_KEY, {}));

export function toggleBackend(id: string): void {
	backendCollapsed[id] = !backendCollapsed[id];
	write(BACKENDS_KEY, backendCollapsed);
}

export function expandBackend(id: string): void {
	if (!backendCollapsed[id]) return;
	backendCollapsed[id] = false;
	write(BACKENDS_KEY, backendCollapsed);
}
