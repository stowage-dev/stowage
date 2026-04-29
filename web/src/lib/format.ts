// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

export function bytes(n: number | null | undefined): string {
	if (n == null) return '—';
	if (n < 1024) return n + ' B';
	const u = ['KB', 'MB', 'GB', 'TB', 'PB'];
	let i = -1;
	let v = n;
	do {
		v /= 1024;
		i++;
	} while (v >= 1024 && i < u.length - 1);
	return v.toFixed(v < 10 ? 1 : 0) + ' ' + u[i];
}

export function num(n: number | null | undefined): string {
	return n == null ? '—' : n.toLocaleString('en-US');
}

export function middleEllipsis(s: string | null | undefined, max = 40): string {
	if (!s || s.length <= max) return s ?? '';
	const keep = Math.floor((max - 1) / 2);
	return s.slice(0, keep) + '…' + s.slice(-keep);
}
