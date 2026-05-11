<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { X, Share2, Download, Trash2, Pencil, FolderInput, Tag, History } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import CopyChip from '$lib/components/ui/CopyChip.svelte';
	import KVEditor, { type KVRow } from '$lib/components/ui/KVEditor.svelte';
	import KVChips from '$lib/components/ui/KVChips.svelte';
	import ObjectIcon from './ObjectIcon.svelte';
	import Preview from './Preview.svelte';
	import { api, ApiException } from '$lib/api';
	import { bytes, middleEllipsis } from '$lib/format';
	import type { BrowserItem, Capabilities, ObjectVersion } from '$lib/types';

	interface Props {
		item: BrowserItem;
		onclose: () => void;
		onshare: (it: BrowserItem) => void;
		ondelete?: (it: BrowserItem) => void;
		ondownload?: (it: BrowserItem) => void;
		onrename?: (it: BrowserItem) => void;
		onmove?: (it: BrowserItem) => void;
		backend: string;
		bucket: string;
		prefix: string[];
		capabilities: Capabilities;
		canEdit?: boolean;
	}

	let {
		item,
		onclose,
		onshare,
		ondelete,
		ondownload,
		onrename,
		onmove,
		backend,
		bucket,
		prefix,
		capabilities,
		canEdit = false
	}: Props = $props();

	const fullKey = $derived([...prefix, item.key].join('/'));

	let metadata = $state<Record<string, string> | null>(null);
	let tags = $state<Record<string, string> | null>(null);
	let versions = $state<ObjectVersion[] | null>(null);
	let metadataLoading = $state(true);
	let tagsLoading = $state(untrack(() => capabilities.tagging));
	let versionsLoading = $state(untrack(() => capabilities.versioning));
	let metadataError = $state<string | null>(null);
	let tagsError = $state<string | null>(null);
	let versionsError = $state<string | null>(null);

	let editingMetadata = $state(false);
	let editingTags = $state(false);
	let metadataRows = $state<KVRow[]>([]);
	let tagRows = $state<KVRow[]>([]);
	let savingMetadata = $state(false);
	let savingTags = $state(false);

	async function loadMetadata(): Promise<void> {
		metadataLoading = true;
		metadataError = null;
		try {
			const info = await api.headObject(backend, bucket, fullKey);
			metadata = info.metadata ?? {};
		} catch (err) {
			metadataError = err instanceof ApiException ? err.message : 'Failed to load metadata.';
			metadata = {};
		} finally {
			metadataLoading = false;
		}
	}

	async function loadTags(): Promise<void> {
		if (!capabilities.tagging) {
			tags = {};
			tagsLoading = false;
			return;
		}
		tagsLoading = true;
		tagsError = null;
		try {
			tags = await api.getObjectTags(backend, bucket, fullKey);
		} catch (err) {
			tagsError = err instanceof ApiException ? err.message : 'Failed to load tags.';
			tags = {};
		} finally {
			tagsLoading = false;
		}
	}

	async function loadVersions(): Promise<void> {
		if (!capabilities.versioning) {
			versions = [];
			versionsLoading = false;
			return;
		}
		versionsLoading = true;
		versionsError = null;
		try {
			versions = await api.listObjectVersions(backend, bucket, fullKey);
		} catch (err) {
			versionsError = err instanceof ApiException ? err.message : 'Failed to load versions.';
			versions = [];
		} finally {
			versionsLoading = false;
		}
	}

	function downloadVersion(v: ObjectVersion): void {
		if (v.is_delete_marker) return;
		window.location.href = api.objectURL(
			backend,
			bucket,
			fullKey,
			'attachment',
			v.version_id || undefined
		);
	}

	onMount(() => {
		const onKey = (e: KeyboardEvent): void => {
			if (e.key === 'Escape' && !editingMetadata && !editingTags) onclose();
		};
		window.addEventListener('keydown', onKey);
		return () => window.removeEventListener('keydown', onKey);
	});

	$effect(() => {
		void fullKey;
		untrack(() => {
			editingMetadata = false;
			editingTags = false;
			void loadMetadata();
			void loadTags();
			void loadVersions();
		});
	});

	function rowsFromMap(m: Record<string, string>): KVRow[] {
		const out: KVRow[] = Object.entries(m).map(([k, v]) => ({ k, v }));
		if (out.length === 0) out.push({ k: '', v: '' });
		return out;
	}

	function rowsToMap(rows: KVRow[]): Record<string, string> {
		const out: Record<string, string> = {};
		for (const r of rows) {
			const k = r.k.trim();
			if (!k) continue;
			out[k] = r.v;
		}
		return out;
	}

	function startEditMetadata(): void {
		metadataRows = rowsFromMap(metadata ?? {});
		editingMetadata = true;
	}

	function startEditTags(): void {
		tagRows = rowsFromMap(tags ?? {});
		editingTags = true;
	}

	async function saveMetadata(): Promise<void> {
		savingMetadata = true;
		try {
			const info = await api.updateObjectMetadata(
				backend,
				bucket,
				fullKey,
				rowsToMap(metadataRows)
			);
			metadata = info.metadata ?? {};
			editingMetadata = false;
			toast.success('Metadata saved');
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Failed to save metadata.');
		} finally {
			savingMetadata = false;
		}
	}

	async function saveTags(): Promise<void> {
		const next = rowsToMap(tagRows);
		if (Object.keys(next).length > 10) {
			toast.error('At most 10 tags per object.');
			return;
		}
		savingTags = true;
		try {
			await api.setObjectTags(backend, bucket, fullKey, next);
			tags = next;
			editingTags = false;
			toast.success('Tags saved');
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Failed to save tags.');
		} finally {
			savingTags = false;
		}
	}
</script>

{#snippet sectionHeader(
	label: string,
	icon: import('svelte').Snippet,
	trailing: import('svelte').Snippet | null
)}
	<div class="mb-2 flex items-center gap-2">
		<span class="inline-flex text-stw-fg-soft">{@render icon()}</span>
		<div class="flex-1 text-[10.5px] font-semibold tracking-[0.08em] text-stw-fg-soft uppercase">
			{label}
		</div>
		{#if trailing}{@render trailing()}{/if}
	</div>
{/snippet}

<div role="presentation" onclick={onclose} class="absolute inset-0 z-[15] bg-transparent"></div>
<div
	role="dialog"
	aria-label="Object details"
	class="absolute top-0 right-0 bottom-0 z-20 flex w-[min(560px,max(70vw,320px))] max-w-full animate-[stw-slide-in-right_180ms_cubic-bezier(0.4,0,0.2,1)] flex-col border-l border-stw-border bg-stw-bg-panel shadow-stw-lg"
>
	<header class="flex h-[44px] items-center gap-2 border-b border-stw-border px-3.5">
		<ObjectIcon kind={item.kind} />
		<span class="flex-1 truncate text-[13px] font-semibold">{item.displayName}</span>
		<button
			type="button"
			onclick={onclose}
			class="inline-flex h-[24px] w-[24px] cursor-pointer items-center justify-center rounded-[5px] border-0 bg-transparent text-stw-fg-mute focus-ring hover:bg-stw-bg-hover"
			aria-label="Close"
		>
			<X size={14} strokeWidth={1.7} />
		</button>
	</header>

	<div class="stw-scroll flex-1 overflow-auto">
		<Preview {item} {backend} {bucket} {prefix} />

		<section class="px-3.5 pb-3.5">
			<div class="grid grid-cols-[100px_1fr] gap-x-3 gap-y-1.5 text-[12px]">
				<span class="text-stw-fg-mute">Size</span>
				<span class="font-mono">{bytes(item.size)}</span>
				<span class="text-stw-fg-mute">Modified</span>
				<span class="font-mono">
					{item.modified ? new Date(item.modified).toLocaleString() : '—'}
				</span>
				<span class="text-stw-fg-mute">Type</span>
				<span class="font-mono">{item.ct ?? '—'}</span>
				{#if item.etag}
					{@const etag = item.etag}
					<span class="text-stw-fg-mute">ETag</span>
					<span>
						<CopyChip value={etag}>
							{middleEllipsis(etag.replaceAll('"', ''), 20)}
						</CopyChip>
					</span>
				{/if}
				<span class="text-stw-fg-mute">Key</span>
				<span>
					<CopyChip value={fullKey}>
						{middleEllipsis(fullKey, 40)}
					</CopyChip>
				</span>
			</div>
		</section>

		<section class="border-t border-stw-border px-3.5 py-2.5">
			{#snippet metaHeaderIcon()}<Pencil size={12} strokeWidth={1.7} />{/snippet}
			{#snippet metaHeaderAction()}
				{#if canEdit && !editingMetadata}
					<button
						type="button"
						onclick={startEditMetadata}
						class="cursor-pointer border-0 bg-transparent px-1 py-0.5 text-[11.5px] text-stw-accent-600 focus-ring hover:underline"
					>
						Edit
					</button>
				{/if}
			{/snippet}
			{@render sectionHeader('User metadata', metaHeaderIcon, metaHeaderAction)}

			{#if metadataLoading}
				<div class="text-[12px] text-stw-fg-soft">Loading…</div>
			{:else if metadataError}
				<div class="text-[12px] text-stw-err">{metadataError}</div>
			{:else if editingMetadata}
				<KVEditor bind:rows={metadataRows} keyPlaceholder="x-amz-meta-…" valuePlaceholder="value" />
				<div class="mt-2.5 flex items-center gap-1.5">
					<Button variant="primary" size="sm" onclick={saveMetadata} disabled={savingMetadata}>
						{savingMetadata ? 'Saving…' : 'Save'}
					</Button>
					<Button
						variant="ghost"
						size="sm"
						onclick={() => (editingMetadata = false)}
						disabled={savingMetadata}
					>
						Cancel
					</Button>
					<span class="flex-1 self-center text-[11px] text-stw-fg-soft">
						Saving rewrites the object in place (new version on versioned buckets).
					</span>
				</div>
			{:else if Object.keys(metadata ?? {}).length === 0}
				<div class="text-[12px] text-stw-fg-mute">No user metadata.</div>
			{:else}
				<KVChips entries={metadata ?? {}} />
			{/if}
		</section>

		{#if capabilities.versioning}
			<section class="border-t border-stw-border px-3.5 py-2.5">
				{#snippet versionsHeaderIcon()}<History size={12} strokeWidth={1.7} />{/snippet}
				{@render sectionHeader('Versions', versionsHeaderIcon, null)}
				{#if versionsLoading}
					<div class="text-[12px] text-stw-fg-soft">Loading…</div>
				{:else if versionsError}
					<div class="text-[12px] text-stw-err">{versionsError}</div>
				{:else if (versions ?? []).length === 0}
					<div class="text-[12px] text-stw-fg-mute">No versions recorded.</div>
				{:else}
					<div class="flex flex-col gap-1">
						{#each versions ?? [] as v (v.version_id || v.last_modified)}
							<div
								class="flex items-center gap-2 rounded-md border border-stw-border px-2 py-1.5 text-[12px] {v.is_latest
									? 'bg-stw-bg-panel'
									: 'bg-stw-bg-sunken'}"
							>
								{#if v.is_latest}
									<span
										class="inline-flex flex-shrink-0 items-center rounded px-1.5 py-px text-[10.5px] font-semibold tracking-[0.04em] text-stw-accent-600 stw-version-current"
									>
										LATEST
									</span>
								{/if}
								{#if v.is_delete_marker}
									<span
										class="inline-flex flex-shrink-0 items-center rounded px-1.5 py-px text-[10.5px] font-semibold tracking-[0.04em] text-stw-err stw-version-deleted"
									>
										DELETED
									</span>
								{/if}
								<span
									class="min-w-0 flex-1 truncate font-mono text-stw-fg-soft"
									title={v.version_id}
								>
									{v.version_id ? middleEllipsis(v.version_id, 24) : '(no version id)'}
								</span>
								<span class="flex-shrink-0 font-mono text-stw-fg-mute">
									{v.is_delete_marker ? '—' : bytes(v.size)}
								</span>
								<span class="flex-shrink-0 font-mono text-[11px] text-stw-fg-mute">
									{v.last_modified ? new Date(v.last_modified).toLocaleDateString() : '—'}
								</span>
								{#if !v.is_delete_marker}
									<button
										type="button"
										onclick={() => downloadVersion(v)}
										aria-label="Download this version"
										class="inline-flex h-[24px] w-[24px] flex-shrink-0 cursor-pointer items-center justify-center rounded border-0 bg-transparent text-stw-fg-mute focus-ring hover:bg-stw-bg-hover"
									>
										<Download size={12} strokeWidth={1.7} />
									</button>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			</section>
		{/if}

		{#if capabilities.tagging}
			<section class="border-t border-stw-border px-3.5 py-2.5">
				{#snippet tagHeaderIcon()}<Tag size={12} strokeWidth={1.7} />{/snippet}
				{#snippet tagHeaderAction()}
					{#if canEdit && !editingTags}
						<button
							type="button"
							onclick={startEditTags}
							class="cursor-pointer border-0 bg-transparent px-1 py-0.5 text-[11.5px] text-stw-accent-600 focus-ring hover:underline"
						>
							Edit
						</button>
					{/if}
				{/snippet}
				{@render sectionHeader('Tags', tagHeaderIcon, tagHeaderAction)}

				{#if tagsLoading}
					<div class="text-[12px] text-stw-fg-soft">Loading…</div>
				{:else if tagsError}
					<div class="text-[12px] text-stw-err">{tagsError}</div>
				{:else if editingTags}
					<KVEditor bind:rows={tagRows} keyPlaceholder="key" valuePlaceholder="value" />
					<div class="mt-2.5 flex items-center gap-1.5">
						<Button variant="primary" size="sm" onclick={saveTags} disabled={savingTags}>
							{savingTags ? 'Saving…' : 'Save'}
						</Button>
						<Button
							variant="ghost"
							size="sm"
							onclick={() => (editingTags = false)}
							disabled={savingTags}
						>
							Cancel
						</Button>
						<span class="flex-1 self-center text-[11px] text-stw-fg-soft">
							Up to 10 tags. Keys ≤ 128, values ≤ 256.
						</span>
					</div>
				{:else if Object.keys(tags ?? {}).length === 0}
					<div class="text-[12px] text-stw-fg-mute">No tags.</div>
				{:else}
					<KVChips entries={tags ?? {}} />
				{/if}
			</section>
		{/if}
	</div>

	<footer class="flex gap-2 border-t border-stw-border p-3">
		{#snippet shareIcon()}<Share2 size={13} strokeWidth={1.7} />{/snippet}
		{#snippet downloadIcon()}<Download size={13} strokeWidth={1.7} />{/snippet}
		{#snippet trashIcon()}<Trash2 size={13} strokeWidth={1.7} />{/snippet}
		{#snippet renameIcon()}<Pencil size={13} strokeWidth={1.7} />{/snippet}
		{#snippet moveIcon()}<FolderInput size={13} strokeWidth={1.7} />{/snippet}
		<Button variant="primary" icon={shareIcon} onclick={() => onshare(item)}>Share</Button>
		<Button icon={downloadIcon} onclick={() => ondownload?.(item)}>Download</Button>
		<span class="flex-1"></span>
		{#if onmove}
			<Button variant="ghost" icon={moveIcon} onclick={() => onmove?.(item)} title="Move or copy">
				Move
			</Button>
		{/if}
		{#if onrename}
			<Button variant="ghost" icon={renameIcon} onclick={() => onrename?.(item)} title="Rename">
				Rename
			</Button>
		{/if}
		{#if ondelete}
			<Button variant="ghost" icon={trashIcon} onclick={() => ondelete?.(item)} title="Delete">
				Delete
			</Button>
		{/if}
	</footer>
</div>
