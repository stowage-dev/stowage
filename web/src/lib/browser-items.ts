// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { BrowserItem, ListObjectsResult, ObjectInfo, ObjectKind } from './types';

const IMAGE_EXT = /\.(jpe?g|png|gif|webp|avif|heic|heif|svg|bmp)$/i;
const VIDEO_EXT = /\.(mp4|mov|webm|mkv|m4v|avi)$/i;
const AUDIO_EXT = /\.(mp3|wav|flac|m4a|ogg)$/i;
const PDF_EXT = /\.pdf$/i;
const TEXT_EXT =
	/\.(txt|md|markdown|csv|tsv|json|yaml|yml|toml|ini|log|sh|js|ts|tsx|jsx|css|html?|xml|go|py|rb|rs|java|c|h|cpp|hpp|sql)$/i;

export function objectKindFor(o: Pick<ObjectInfo, 'key' | 'content_type'>): ObjectKind {
	const ct = (o.content_type ?? '').toLowerCase();
	if (ct.startsWith('image/')) return 'image';
	if (ct.startsWith('video/')) return 'video';
	if (ct === 'application/pdf') return 'pdf';
	if (ct.startsWith('text/') || ct === 'application/json' || ct === 'application/x-yaml')
		return 'text';

	const k = o.key;
	if (IMAGE_EXT.test(k)) return 'image';
	if (VIDEO_EXT.test(k)) return 'video';
	if (AUDIO_EXT.test(k)) return 'video'; // no audio icon in the prototype set
	if (PDF_EXT.test(k)) return 'pdf';
	if (TEXT_EXT.test(k)) return 'text';
	return 'file';
}

/**
 * Convert an S3 listing into the BrowserItems the UI table expects.
 * Folders come from CommonPrefixes, files from Contents (Objects). Both have
 * their `prefix` stripped so display keys are bucket-relative.
 *
 * Skips the prefix marker (the folder itself, key === prefix) when present.
 * Also drops zero-byte trailing-slash files since they are folder placeholders.
 */
export function toBrowserItems(res: ListObjectsResult): BrowserItem[] {
	const prefix = res.prefix ?? '';
	const items: BrowserItem[] = [];

	for (const cp of res.common_prefixes ?? []) {
		const trimmed = cp.startsWith(prefix) ? cp.slice(prefix.length) : cp;
		if (!trimmed) continue;
		const display = trimmed.endsWith('/') ? trimmed.slice(0, -1) : trimmed;
		items.push({
			key: trimmed,
			displayName: display,
			kind: 'folder',
			size: null,
			modified: null
		});
	}

	for (const o of res.objects ?? []) {
		if (o.key === prefix) continue; // the folder marker
		if (o.key.endsWith('/') && o.size === 0) continue; // zero-byte placeholder
		const trimmed = o.key.startsWith(prefix) ? o.key.slice(prefix.length) : o.key;
		if (!trimmed) continue;
		items.push({
			key: trimmed,
			displayName: trimmed,
			kind: objectKindFor(o),
			size: o.size,
			modified: o.last_modified ?? null,
			ct: o.content_type,
			etag: o.etag
		});
	}

	// Folders first, then alphabetical by display name.
	items.sort((a, b) => {
		if (a.kind === 'folder' && b.kind !== 'folder') return -1;
		if (a.kind !== 'folder' && b.kind === 'folder') return 1;
		return a.displayName.localeCompare(b.displayName);
	});

	return items;
}
