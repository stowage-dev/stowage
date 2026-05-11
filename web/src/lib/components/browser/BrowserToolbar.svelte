<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import {
		Download,
		Folder,
		FolderInput,
		Info,
		Plus,
		RotateCw,
		Trash2,
		Upload,
		X
	} from 'lucide-svelte';
	import BackendMark from '$lib/components/ui/BackendMark.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import SearchField from '$lib/components/ui/SearchField.svelte';
	import Segmented from '$lib/components/ui/Segmented.svelte';
	import Tooltip from '$lib/components/ui/Tooltip.svelte';
	import { tweaks, setTweak } from '$lib/stores/tweaks.svelte';
	import { inferKind } from '$lib/backend-kind';
	import { bytes } from '$lib/format';
	import type { Backend, BrowserItem, ListObjectsResult } from '$lib/types';

	interface Props {
		backend: Backend;
		selected: Set<string>;
		items: BrowserItem[];
		allItems: BrowserItem[];
		listing: ListObjectsResult | null;
		canWrite: boolean;
		filter: string;
		refreshing: boolean;
		onclearselection: () => void;
		ondownloadselected: () => void;
		onmoveselected: () => void;
		ondeleteselected: () => void;
		onpickfiles: () => void;
		onpickfolder: () => void;
		onnewfolder: () => void;
		onrefresh: () => void;
	}

	let {
		backend,
		selected,
		items,
		allItems,
		listing,
		canWrite,
		filter = $bindable(),
		refreshing,
		onclearselection,
		ondownloadselected,
		onmoveselected,
		ondeleteselected,
		onpickfiles,
		onpickfolder,
		onnewfolder,
		onrefresh
	}: Props = $props();

	const kind = $derived(inferKind(backend));
	const totalSize = $derived(items.reduce((a, x) => a + (x.size ?? 0), 0));
	const downloadLabel = $derived(
		selected.size > 1 || [...selected].some((k) => k.endsWith('/')) ? 'Download .zip' : 'Download'
	);
</script>

<div
	class="flex min-h-[44px] flex-shrink-0 flex-wrap items-center gap-2.5 border-b border-stw-border bg-stw-bg-panel px-3.5 py-1.5"
>
	<div class="flex items-center gap-1.5 text-[12.5px] text-stw-fg-mute">
		<BackendMark {kind} size={14} />
		<span class="font-mono">{items.length}</span>
		<span>{filter ? 'matches' : 'items'} ·</span>
		<span class="font-mono">{bytes(totalSize)}</span>
		{#if filter && items.length !== allItems.length}
			<span class="text-[11.5px] text-stw-fg-soft">of {allItems.length}</span>
		{/if}
		{#if !backend.capabilities.versioning}
			<span class="px-1 text-stw-fg-soft">·</span>
			<Tooltip text="This backend doesn't expose versioning">
				<span class="inline-flex items-center gap-1 text-[11.5px] text-stw-fg-soft">
					<Info size={11} strokeWidth={1.7} /> versioning unavailable
				</span>
			</Tooltip>
		{/if}
		{#if listing?.is_truncated}
			<span class="text-[11.5px] text-stw-warn">· listing truncated</span>
		{/if}
	</div>
	<span class="flex-1"></span>
	{#if selected.size > 0}
		<div class="flex items-center gap-2 text-[12.5px] text-stw-fg">
			<span class="font-medium">{selected.size} selected</span>
			{#snippet downloadIcon()}<Download size={12} strokeWidth={1.7} />{/snippet}
			{#snippet trashIcon()}<Trash2 size={12} strokeWidth={1.7} />{/snippet}
			{#snippet moveIcon()}<FolderInput size={12} strokeWidth={1.7} />{/snippet}
			<Button size="sm" icon={downloadIcon} onclick={ondownloadselected}>{downloadLabel}</Button>
			{#if canWrite}
				<Button size="sm" icon={moveIcon} onclick={onmoveselected}>Move / Copy</Button>
				<Button size="sm" variant="danger" icon={trashIcon} onclick={ondeleteselected}>Delete</Button>
			{/if}
			<button
				type="button"
				onclick={onclearselection}
				class="cursor-pointer border-0 bg-transparent p-1 text-stw-fg-mute hover:text-stw-fg"
				aria-label="Clear selection"
			>
				<X size={13} strokeWidth={1.7} />
			</button>
		</div>
	{:else}
		<SearchField
			bind:value={filter}
			placeholder="Filter in this view"
			size="sm"
			width="180px"
			onkeydown={(e) => {
				if (e.key === 'Escape') filter = '';
			}}
		/>
		<Segmented
			value={tweaks.density}
			onchange={(v) => setTweak('density', v)}
			size="sm"
			options={[
				{ value: 'compact', label: 'Compact' },
				{ value: 'cosy', label: 'Cosy' },
				{ value: 'roomy', label: 'Roomy' }
			]}
		/>
		{#snippet refreshIcon()}<RotateCw size={12} strokeWidth={1.7} />{/snippet}
		<Tooltip text="Refetch listing and rescan bucket usage">
			<Button size="sm" icon={refreshIcon} onclick={onrefresh} disabled={refreshing}>
				{refreshing ? 'Refreshing…' : 'Refresh'}
			</Button>
		</Tooltip>
		{#if canWrite}
			{#snippet plusIcon()}<Plus size={12} strokeWidth={1.7} />{/snippet}
			<Button size="sm" icon={plusIcon} onclick={onnewfolder}>New folder</Button>
			{#snippet folderUploadIcon()}<Folder size={12} strokeWidth={1.7} />{/snippet}
			<Button size="sm" icon={folderUploadIcon} onclick={onpickfolder}>Upload folder</Button>
			{#snippet uploadIcon()}<Upload size={12} strokeWidth={1.7} />{/snippet}
			<Button size="sm" variant="primary" icon={uploadIcon} onclick={onpickfiles}>Upload</Button>
		{/if}
	{/if}
</div>
