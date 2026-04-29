<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { File as FileIcon } from 'lucide-svelte';
	import ObjectIcon from './ObjectIcon.svelte';
	import { api } from '$lib/api';
	import { bytes } from '$lib/format';
	import type { BrowserItem } from '$lib/types';

	interface Props {
		item: BrowserItem;
		backend: string;
		bucket: string;
		prefix: string[];
	}

	let { item, backend, bucket, prefix }: Props = $props();

	const fullKey = $derived([...prefix, item.key].join('/'));
	const url = $derived(api.previewURL(backend, bucket, fullKey));

	// Inline-fetch text only when small enough to make rendering safe.
	const TEXT_PREVIEW_LIMIT = 256 * 1024;
	let text = $state<string | null>(null);
	let textErr = $state<string | null>(null);
	let textTruncated = $state(false);

	$effect(() => {
		text = null;
		textErr = null;
		textTruncated = false;
		if (item.kind !== 'text') return;
		if (item.size != null && item.size > TEXT_PREVIEW_LIMIT) {
			textTruncated = true;
			return;
		}
		const ac = new AbortController();
		fetch(url, { credentials: 'same-origin', signal: ac.signal })
			.then(async (r) => {
				if (!r.ok) {
					textErr = `${r.status} ${r.statusText}`;
					return;
				}
				const t = await r.text();
				if (t.length > TEXT_PREVIEW_LIMIT) {
					text = t.slice(0, TEXT_PREVIEW_LIMIT);
					textTruncated = true;
				} else {
					text = t;
				}
			})
			.catch((e) => {
				if (e?.name !== 'AbortError') textErr = e?.message ?? 'fetch failed';
			});
		return () => ac.abort();
	});
</script>

<div class="stw-preview">
	{#if item.kind === 'image'}
		<img src={url} alt={item.displayName} loading="lazy" />
	{:else if item.kind === 'video'}
		<!-- svelte-ignore a11y_media_has_caption -->
		<video src={url} controls preload="metadata"></video>
	{:else if item.kind === 'pdf'}
		<iframe src={url} title={item.displayName}></iframe>
	{:else if item.kind === 'text'}
		{#if textTruncated && text == null}
			<div class="stw-preview-fallback">
				<ObjectIcon kind={item.kind} size={28} />
				<div class="stw-preview-label">
					File is {bytes(item.size)} — too large for an inline preview.
				</div>
				<a href={api.objectURL(backend, bucket, fullKey)} class="stw-preview-link"
					>Download to read</a
				>
			</div>
		{:else if textErr}
			<div class="stw-preview-fallback">
				<ObjectIcon kind={item.kind} size={28} />
				<div class="stw-preview-label">Couldn't load preview: {textErr}</div>
			</div>
		{:else if text != null}
			<pre>{text}</pre>
			{#if textTruncated}
				<div class="stw-truncated-note">
					Truncated at {bytes(TEXT_PREVIEW_LIMIT)}.
					<a href={api.objectURL(backend, bucket, fullKey)}>Download full file</a>
				</div>
			{/if}
		{:else}
			<div class="stw-preview-fallback">
				<span style="color:var(--stw-fg-soft);font-size:12px;">Loading preview…</span>
			</div>
		{/if}
	{:else}
		<div class="stw-preview-fallback">
			<span style="transform:scale(2.4);display:inline-flex;color:var(--stw-fg-soft);">
				{#if item.kind === 'folder'}
					<ObjectIcon kind="folder" />
				{:else}
					<FileIcon size={20} strokeWidth={1.4} />
				{/if}
			</span>
			<div class="stw-preview-label">No inline preview for this file type.</div>
			<a href={api.objectURL(backend, bucket, fullKey)} class="stw-preview-link">Download</a>
		</div>
	{/if}
</div>

<style>
	.stw-preview {
		margin: 14px;
		border: 1px solid var(--stw-border);
		border-radius: 7px;
		background: var(--stw-bg-sunken);
		overflow: hidden;
		display: flex;
		flex-direction: column;
		min-height: 200px;
		max-height: 60vh;
	}
	.stw-preview img {
		width: 100%;
		height: auto;
		max-height: 60vh;
		object-fit: contain;
		display: block;
		background:
			linear-gradient(45deg, #00000010 25%, transparent 25%),
			linear-gradient(-45deg, #00000010 25%, transparent 25%),
			linear-gradient(45deg, transparent 75%, #00000010 75%),
			linear-gradient(-45deg, transparent 75%, #00000010 75%);
		background-size: 16px 16px;
		background-position:
			0 0,
			0 8px,
			8px -8px,
			-8px 0;
	}
	.stw-preview video {
		width: 100%;
		max-height: 60vh;
		display: block;
		background: black;
	}
	.stw-preview iframe {
		width: 100%;
		height: 60vh;
		border: 0;
		display: block;
		background: white;
	}
	.stw-preview pre {
		margin: 0;
		padding: 12px;
		font-family: var(--stw-font-mono);
		font-size: 12px;
		line-height: 1.5;
		white-space: pre-wrap;
		word-break: break-word;
		overflow: auto;
		flex: 1;
		min-height: 0;
	}
	.stw-preview-fallback {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 10px;
		padding: 32px 20px;
		text-align: center;
	}
	.stw-preview-label {
		font-size: 12.5px;
		color: var(--stw-fg-mute);
	}
	.stw-preview-link {
		font-size: 12.5px;
		color: var(--stw-accent-600);
		text-decoration: none;
	}
	.stw-preview-link:hover {
		text-decoration: underline;
	}
	.stw-truncated-note {
		padding: 6px 12px;
		font-size: 11.5px;
		color: var(--stw-fg-soft);
		border-top: 1px solid var(--stw-border);
		background: var(--stw-bg-panel);
	}
	.stw-truncated-note a {
		color: var(--stw-accent-600);
	}
</style>
