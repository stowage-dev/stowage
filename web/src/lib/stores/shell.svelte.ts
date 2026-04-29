// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { BrowserItem } from '$lib/types';

interface ShareTarget {
	item: BrowserItem;
	backend: string;
	bucket: string;
	prefix: string[];
}

/** UI overlay state shared across the shell. */
export const overlay = $state<{
	palette: boolean;
	share: ShareTarget | null;
	bannerDismissed: boolean;
	sidebarOpen: boolean;
}>({
	palette: false,
	share: null,
	bannerDismissed: false,
	sidebarOpen: false
});

export function openPalette(): void {
	overlay.palette = true;
}
export function closePalette(): void {
	overlay.palette = false;
}
export function openShare(
	item: BrowserItem,
	backend: string,
	bucket: string,
	prefix: string[]
): void {
	overlay.share = { item, backend, bucket, prefix };
}
export function closeShare(): void {
	overlay.share = null;
}
export function dismissBanner(): void {
	overlay.bannerDismissed = true;
}
export function openSidebar(): void {
	overlay.sidebarOpen = true;
}
export function closeSidebar(): void {
	overlay.sidebarOpen = false;
}
export function toggleSidebar(): void {
	overlay.sidebarOpen = !overlay.sidebarOpen;
}
