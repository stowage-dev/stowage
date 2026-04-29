// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { Backend, BackendKind, BackendKindInfo } from './types';

export const BACKEND_KINDS: Record<BackendKind, BackendKindInfo> = {
	garage: { label: 'Garage', color: '#7c6cff', letter: 'G' },
	minio: { label: 'MinIO', color: '#c7282d', letter: 'M' },
	seaweedfs: { label: 'SeaweedFS', color: '#22a06b', letter: 'S' },
	aws: { label: 'AWS S3', color: '#ff9900', letter: 'A' },
	b2: { label: 'Backblaze', color: '#e41e2b', letter: 'B' },
	r2: { label: 'Cloudflare R2', color: '#f38020', letter: 'R' },
	wasabi: { label: 'Wasabi', color: '#00d18f', letter: 'W' },
	generic: { label: 'Generic S3', color: '#52525b', letter: '◉' }
};

/** The API doesn't expose a backend kind today. Heuristic: match the id/name.
 * `capabilities` is optional so admin-only rows (DB-stored but not registered) can be classified too. */
export function inferKind(
	b: Pick<Backend, 'id' | 'name'> & { capabilities?: { admin_api?: string } }
): BackendKind {
	const hay = (b.id + ' ' + b.name).toLowerCase();
	const admin = b.capabilities?.admin_api;
	if (admin === 'minio' || hay.includes('minio')) return 'minio';
	if (admin === 'garage' || hay.includes('garage')) return 'garage';
	if (admin === 'seaweedfs' || hay.includes('seaweed')) return 'seaweedfs';
	if (hay.includes('aws') || hay.includes('s3.amazonaws')) return 'aws';
	if (hay.includes('backblaze') || hay.includes(' b2') || hay.endsWith('b2')) return 'b2';
	if (hay.includes(' r2') || hay.endsWith('r2') || hay.includes('cloudflare')) return 'r2';
	if (hay.includes('wasabi')) return 'wasabi';
	return 'generic';
}

export interface BackendHealth {
	state: 'ok' | 'warn' | 'err';
	message?: string;
}

export function backendHealth(b: Backend): BackendHealth {
	if (b.healthy) return { state: 'ok' };
	if (b.last_error) return { state: 'err', message: b.last_error };
	return { state: 'warn', message: 'Health unknown' };
}
