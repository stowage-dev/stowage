// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { SvelteMap } from 'svelte/reactivity';
import { toast } from 'svelte-sonner';
import { ApiException, type ApiClient } from '$lib/api';
import type { ObjectKind, UploadItem, UploadStatus } from '$lib/types';
import { objectKindFor } from '$lib/browser-items';

export const queue = $state<{ items: UploadItem[] }>({ items: [] });

// Single-PUT cap matches the server's maxSingleUploadBytes (Phase 2 cap).
// Anything bigger goes through multipart.
const SINGLE_PUT_LIMIT = 5 * 1024 * 1024;
// 16 MB parts per spec; server caps individual parts at 64 MB.
const PART_SIZE = 16 * 1024 * 1024;

// Per-item runtime kept outside the reactive queue so it can be grabbed by
// pause/resume/cancel without going through the proxy. Multipart state here
// is also what makes resume work — completed parts persist across aborts.
interface Runtime {
	api: ApiClient;
	backendId: string;
	bucket: string;
	key: string;
	file: File;
	xhr?: XMLHttpRequest;
	// Set after the user picks "Replace" on a conflict — subsequent attempts
	// for this item drop the If-None-Match guard.
	forceOverwrite?: boolean;
	multipart?: {
		uploadId: string;
		completed: { PartNumber: number; ETag: string }[];
	};
}

// Thrown when the server rejects an upload because the key already exists
// (412 Precondition Failed with code=object_exists). Distinct type so the
// runner can branch into the conflict-resolution flow without string-matching.
class ConflictError extends Error {
	constructor() {
		super('object already exists');
		this.name = 'ConflictError';
	}
}

// SvelteMap keeps the lint happy and gives us reactive size/iteration if a
// future caller wants to render the in-flight set; behaviour is unchanged
// for the existing get/set/delete callers.
const runtimes = new SvelteMap<string, Runtime>();

function kindOf(file: File): ObjectKind {
	return objectKindFor({ key: file.name, content_type: file.type });
}

export function clearQueue(): void {
	const ids = queue.items.map((i) => i.id);
	queue.items = [];
	for (const id of ids) abortRuntime(id);
}

function abortRuntime(id: string): void {
	const rt = runtimes.get(id);
	if (!rt) return;
	rt.xhr?.abort();
	if (rt.multipart) {
		const { uploadId } = rt.multipart;
		void rt.api.abortMultipart(rt.backendId, rt.bucket, rt.key, uploadId).catch(() => {});
	}
	runtimes.delete(id);
}

// Drag-drop / folder-input flows produce files with a path relative to the
// dropped root (e.g. `myfolder/sub/a.txt`); plain file pickers just have the
// filename. The runner preserves that path under the destination prefix so
// folders rematerialise on the server side.
export interface UploadEntry {
	file: File;
	relativePath: string;
}

/**
 * Upload a batch of files to a bucket prefix. Each upload is added to the
 * visible queue. Files <= 5 MB use the simple multipart-form endpoint;
 * larger files use the multipart upload protocol with 16 MB parts.
 */
export async function uploadFiles(
	api: ApiClient,
	backendId: string,
	bucket: string,
	prefix: string,
	entries: UploadEntry[]
): Promise<void> {
	const startIdx = queue.items.length;
	for (const e of entries) {
		const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
		queue.items.push({
			id,
			name: e.relativePath,
			kind: kindOf(e.file),
			progress: 0,
			status: 'uploading'
		});
		runtimes.set(id, {
			api,
			backendId,
			bucket,
			key: prefix + e.relativePath,
			file: e.file
		});
	}

	for (let i = 0; i < entries.length; i++) {
		// Read the reactive proxy back out of the $state array so mutations
		// to item.progress / item.status inside the runners trigger reactivity.
		const item = queue.items[startIdx + i];
		await runItem(item);
	}
}

async function runItem(item: UploadItem): Promise<void> {
	const rt = runtimes.get(item.id);
	if (!rt || item.status !== 'uploading') return;
	try {
		if (rt.file.size <= SINGLE_PUT_LIMIT) {
			await uploadSingle(rt, item);
		} else {
			await uploadMultipart(rt, item);
		}
		item.progress = 100;
		item.status = 'done';
		runtimes.delete(item.id);
	} catch (err) {
		// Pause and cancel abort the XHR; that surfaces here as a rejection
		// we deliberately swallow so the status the caller set stands. The
		// cast defeats TS narrowing across the prior `!== 'uploading'` guard.
		const s = item.status as UploadStatus;
		if (s === 'paused' || s === 'canceled') return;
		if (err instanceof ConflictError) {
			// Park the item until the user picks Replace or Skip via
			// resolveConflict. The runtime is preserved so a Replace can re-run
			// without re-creating the multipart upload state.
			item.status = 'conflict';
			item.progress = 0;
			item.error = undefined;
			return;
		}
		item.error = err instanceof Error ? err.message : 'upload failed';
		item.status = 'error';
		toast.error(`${rt.file.name}: ${item.error}`);
		runtimes.delete(item.id);
	}
}

/**
 * Resolve a single conflicted upload. `replace` retries with the
 * If-None-Match guard removed; `skip` drops the file from the queue without
 * uploading.
 */
export function resolveConflict(id: string, action: 'replace' | 'skip'): void {
	const item = queue.items.find((i) => i.id === id);
	const rt = runtimes.get(id);
	if (!item || !rt || item.status !== 'conflict') return;
	if (action === 'skip') {
		item.status = 'canceled';
		runtimes.delete(id);
		queue.items = queue.items.filter((i) => i.id !== id);
		return;
	}
	rt.forceOverwrite = true;
	item.status = 'uploading';
	item.progress = 0;
	item.error = undefined;
	void runItem(item);
}

/** Apply the same resolution to every conflicted item currently in the queue. */
export function resolveAllConflicts(action: 'replace' | 'skip'): void {
	for (const item of queue.items) {
		if (item.status === 'conflict') resolveConflict(item.id, action);
	}
}

export function pauseUpload(id: string): void {
	const item = queue.items.find((i) => i.id === id);
	const rt = runtimes.get(id);
	if (!item || !rt || item.status !== 'uploading') return;
	item.status = 'paused';
	rt.xhr?.abort();
}

export function resumeUpload(id: string): void {
	const item = queue.items.find((i) => i.id === id);
	if (!item || !runtimes.has(id) || item.status !== 'paused') return;
	item.status = 'uploading';
	item.error = undefined;
	void runItem(item);
}

export function cancelUpload(id: string): void {
	const item = queue.items.find((i) => i.id === id);
	if (!item) return;
	// Terminal states (done / error): the X is "remove from list".
	if (item.status === 'done' || item.status === 'canceled' || item.status === 'error') {
		queue.items = queue.items.filter((i) => i.id !== id);
		runtimes.delete(id);
		return;
	}
	item.status = 'canceled';
	abortRuntime(id);
	queue.items = queue.items.filter((i) => i.id !== id);
}

// ---- Single PUT ----------------------------------------------------------

function uploadSingle(rt: Runtime, item: UploadItem): Promise<void> {
	return new Promise((resolve, reject) => {
		const fd = new FormData();
		fd.set('file', rt.file);
		fd.set('key', rt.key);
		if (rt.file.type) fd.set('content_type', rt.file.type);

		const xhr = new XMLHttpRequest();
		xhr.open(
			'POST',
			`/api/backends/${encodeURIComponent(rt.backendId)}/buckets/${encodeURIComponent(rt.bucket)}/object`
		);
		const csrf = readCookie('stowage_csrf');
		if (csrf) xhr.setRequestHeader('X-CSRF-Token', csrf);
		if (!rt.forceOverwrite) xhr.setRequestHeader('If-None-Match', '*');
		xhr.withCredentials = true;

		xhr.upload.onprogress = (e) => {
			if (e.lengthComputable) {
				item.progress = Math.min(99, Math.round((e.loaded / e.total) * 100));
			}
		};
		xhr.onerror = () => reject(new Error('network error'));
		xhr.onabort = () => reject(new Error('aborted'));
		xhr.onload = () => {
			if (xhr.status >= 200 && xhr.status < 300) {
				resolve();
				return;
			}
			reject(parseUploadError(xhr));
		};
		rt.xhr = xhr;
		// Single-PUT can't resume partially, so a resumed upload restarts at 0.
		item.progress = 0;
		xhr.send(fd);
	});
}

// ---- Multipart -----------------------------------------------------------

async function uploadMultipart(rt: Runtime, item: UploadItem): Promise<void> {
	if (!rt.multipart) {
		try {
			const created = await rt.api.createMultipart(
				rt.backendId,
				rt.bucket,
				rt.key,
				rt.file.type || undefined,
				rt.forceOverwrite ? undefined : { ifNoneMatch: '*' }
			);
			rt.multipart = { uploadId: created.upload_id, completed: [] };
		} catch (err) {
			if (err instanceof ApiException && err.status === 412 && err.code === 'object_exists') {
				throw new ConflictError();
			}
			throw err;
		}
	}
	const { uploadId, completed } = rt.multipart;
	const partCount = Math.ceil(rt.file.size / PART_SIZE);
	const partProgress = new Array<number>(partCount).fill(0);
	// Account for parts already finished on a previous run.
	for (const part of completed) {
		const idx = part.PartNumber - 1;
		partProgress[idx] = idx === partCount - 1 ? rt.file.size - idx * PART_SIZE : PART_SIZE;
	}

	const updateOverall = () => {
		const sent = partProgress.reduce((a, b) => a + b, 0);
		item.progress = Math.min(99, Math.round((sent / rt.file.size) * 100));
	};
	updateOverall();

	for (let i = completed.length; i < partCount; i++) {
		// Pause between parts: the prior part may have completed before the
		// user clicked pause, so check status before starting the next.
		if (item.status !== 'uploading') throw new Error('aborted');
		const start = i * PART_SIZE;
		const end = Math.min(rt.file.size, start + PART_SIZE);
		const blob = rt.file.slice(start, end);
		const partNumber = i + 1;
		const etag = await uploadPartXHR(rt, uploadId, partNumber, blob, (loaded) => {
			partProgress[i] = loaded;
			updateOverall();
		});
		completed.push({ PartNumber: partNumber, ETag: etag });
	}
	await rt.api.completeMultipart(rt.backendId, rt.bucket, rt.key, uploadId, completed);
	rt.multipart = undefined;
}

function uploadPartXHR(
	rt: Runtime,
	uploadId: string,
	partNumber: number,
	blob: Blob,
	onProgress: (loaded: number) => void
): Promise<string> {
	return new Promise((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		const url =
			`/api/backends/${encodeURIComponent(rt.backendId)}/buckets/${encodeURIComponent(rt.bucket)}` +
			`/multipart/parts/${partNumber}` +
			`?key=${encodeURIComponent(rt.key)}&upload_id=${encodeURIComponent(uploadId)}`;
		xhr.open('PUT', url);
		const csrf = readCookie('stowage_csrf');
		if (csrf) xhr.setRequestHeader('X-CSRF-Token', csrf);
		xhr.setRequestHeader('Content-Type', 'application/octet-stream');
		xhr.withCredentials = true;
		xhr.upload.onprogress = (e) => {
			if (e.lengthComputable) onProgress(e.loaded);
		};
		xhr.onerror = () => reject(new Error('network error'));
		xhr.onabort = () => reject(new Error('aborted'));
		xhr.onload = () => {
			if (xhr.status >= 200 && xhr.status < 300) {
				try {
					const body = JSON.parse(xhr.responseText);
					resolve(body.etag as string);
				} catch {
					reject(new Error('invalid part response'));
				}
				return;
			}
			reject(parseUploadError(xhr));
		};
		rt.xhr = xhr;
		xhr.send(blob);
	});
}

// ---- Helpers -------------------------------------------------------------

function parseUploadError(xhr: XMLHttpRequest): Error {
	let msg = `upload failed (${xhr.status})`;
	let code = 'upload_failed';
	try {
		const body = JSON.parse(xhr.responseText);
		if (body?.error?.message) msg = body.error.message;
		if (body?.error?.code) code = body.error.code;
	} catch {
		/* ignore */
	}
	if (xhr.status === 412 && code === 'object_exists') {
		return new ConflictError();
	}
	return new ApiException(xhr.status, { code, message: msg });
}

function readCookie(name: string): string | null {
	if (typeof document === 'undefined') return null;
	const m = document.cookie.match(new RegExp('(?:^|;\\s*)' + name + '=([^;]+)'));
	return m ? decodeURIComponent(m[1]) : null;
}
