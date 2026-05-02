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
				<span class="text-[12px] text-stw-fg-soft">Loading preview…</span>
			</div>
		{/if}
	{:else}
		<div class="stw-preview-fallback">
			<span class="inline-flex scale-[2.4] text-stw-fg-soft">
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
