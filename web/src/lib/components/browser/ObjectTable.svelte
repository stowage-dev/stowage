<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { Share2, Download } from 'lucide-svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import Tooltip from '$lib/components/ui/Tooltip.svelte';
	import { DataTable, createDataTable, type Column } from '$lib/components/ui/table';
	import ObjectIcon from './ObjectIcon.svelte';
	import { bytes } from '$lib/format';
	import type { BrowserItem } from '$lib/types';
	import type { Density } from '$lib/stores/tweaks.svelte';

	type FolderSize = number | 'loading' | 'error';

	interface Props {
		items: BrowserItem[];
		density?: Density;
		selected: string[];
		folderSizes?: Record<string, FolderSize>;
		setSelected: (s: string[]) => void;
		onopen: (it: BrowserItem) => void;
		onshare: (it: BrowserItem) => void;
		onpreview: (it: BrowserItem) => void;
		ondownload?: (it: BrowserItem) => void;
		onFolderVisible?: (key: string) => void;
	}

	let {
		items,
		density = 'compact',
		selected,
		folderSizes = {},
		setSelected,
		onopen,
		onshare,
		onpreview,
		ondownload,
		onFolderVisible
	}: Props = $props();

	function folderSizeText(key: string): string {
		const s = folderSizes[key];
		if (s === undefined) return '—';
		if (s === 'loading') return '…';
		if (s === 'error') return '?';
		return bytes(s);
	}

	// Fires onFolderVisible when a folder row enters the extended viewport.
	// A 300px rootMargin pre-loads rows just outside the visible area.
	function observeFolder(node: HTMLElement, key: string | null) {
		if (!key || !onFolderVisible) return {};
		let currentKey: string | null = key;
		const observer = new IntersectionObserver(
			(entries) => {
				if (entries[0].isIntersecting && currentKey) onFolderVisible!(currentKey);
			},
			{ rootMargin: '300px 0px' }
		);
		observer.observe(node);
		return {
			update(newKey: string | null) {
				currentKey = newKey;
			},
			destroy() {
				observer.disconnect();
			}
		};
	}

	const cellPadCls = $derived(density === 'compact' ? 'px-2' : 'px-3');
	const cellPadLCls = $derived(density === 'compact' ? 'pl-2' : 'pl-3');

	let anchor = $state<string | null>(null);
	$effect(() => {
		if (anchor && !items.some((o) => o.key === anchor)) anchor = null;
	});

	function rangeKeys(fromKey: string, toKey: string): string[] {
		const fromIdx = items.findIndex((i) => i.key === fromKey);
		const toIdx = items.findIndex((i) => i.key === toKey);
		if (fromIdx === -1 || toIdx === -1) return [toKey];
		const [a, b] = fromIdx < toIdx ? [fromIdx, toIdx] : [toIdx, fromIdx];
		return items.slice(a, b + 1).map((i) => i.key);
	}

	function union(a: string[], b: string[]): string[] {
		return Array.from(new Set([...a, ...b]));
	}

	function onRowClick(key: string, e: MouseEvent | KeyboardEvent): void {
		const shift = e.shiftKey;
		const additive = (e as MouseEvent).metaKey || (e as MouseEvent).ctrlKey;
		if (shift && anchor) {
			const range = rangeKeys(anchor, key);
			setSelected(additive ? union(selected, range) : range);
			return;
		}
		if (additive) {
			setSelected(selected.includes(key) ? selected.filter((k) => k !== key) : [...selected, key]);
			anchor = key;
			return;
		}
		setSelected(selected.length === 1 && selected[0] === key ? [] : [key]);
		anchor = key;
	}

	function onCheckboxClick(key: string, e: MouseEvent): void {
		e.stopPropagation();
		if (e.shiftKey && anchor) {
			const range = rangeKeys(anchor, key);
			const allSelected = range.every((k) => selected.includes(k));
			setSelected(
				allSelected ? selected.filter((k) => !range.includes(k)) : union(selected, range)
			);
			return;
		}
		setSelected(selected.includes(key) ? selected.filter((k) => k !== key) : [...selected, key]);
		anchor = key;
	}

	function selectAll(checked: boolean): void {
		setSelected(checked ? items.map((o) => o.key) : []);
		anchor = null;
	}

	function indeterminate(el: HTMLInputElement | null): void {
		if (el) el.indeterminate = selected.length > 0 && selected.length < items.length;
	}

	const columns: Column<BrowserItem>[] = [
		{ id: 'select', header: '', enableSorting: false, headerClass: 'w-[32px]' },
		{ id: 'name', accessorKey: 'displayName', header: 'Name', enableSorting: true },
		{
			id: 'size',
			accessorKey: 'size',
			header: 'Size',
			align: 'right',
			enableSorting: true,
			headerClass: 'w-[110px]'
		},
		{
			id: 'modified',
			accessorKey: 'modified',
			header: 'Modified',
			enableSorting: true,
			headerClass: 'w-[170px]'
		},
		{ id: 'type', accessorKey: 'ct', header: 'Type', headerClass: 'w-[130px]' },
		{ id: 'actions', header: '', enableSorting: false, headerClass: 'w-[60px]' }
	];

	const objectTable = createDataTable<BrowserItem>({
		data: () => items,
		columns,
		initialSorting: [{ id: 'name', desc: false }]
	});
</script>

{#snippet selectAllCheckboxHeader()}
	<span class="inline-flex items-center">
		<input
			type="checkbox"
			class="stw-check"
			aria-label="Select all"
			checked={selected.length > 0 && selected.length === items.length}
			use:indeterminate
			onchange={(e) => selectAll((e.target as HTMLInputElement).checked)}
		/>
	</span>
{/snippet}

<DataTable
	table={objectTable.table}
	stickyHeader
	{density}
	scrollClass="overflow-auto"
	class="flex-1 rounded-none border-0"
	emptyText=""
	headerSnippets={{ select: selectAllCheckboxHeader }}
	rowClass={(r) => (selected.includes(r.original.key) ? 'bg-stw-bg-sel group' : 'group')}
	onRowClick={(r, e) => onRowClick(r.original.key, e)}
	ondblclick={(r) => (r.original.kind === 'folder' ? onopen(r.original) : onpreview(r.original))}
>
	{#snippet row(o)}
		{@const sel = selected.includes(o.key)}
		<td class="px-3 align-middle" use:observeFolder={o.kind === 'folder' ? o.key : null}>
			<span class="inline-flex items-center align-middle">
				<input
					type="checkbox"
					class="stw-check"
					aria-label={sel ? `Deselect ${o.displayName}` : `Select ${o.displayName}`}
					checked={sel}
					onclick={(e) => onCheckboxClick(o.key, e)}
				/>
			</span>
		</td>
		<td class={cellPadCls + ' align-middle'}>
			<span class="inline-flex items-center gap-2.5 align-middle">
				<ObjectIcon kind={o.kind} />
				<span
					role="button"
					tabindex="0"
					onclick={(e) => {
						e.stopPropagation();
						if (o.kind === 'folder') onopen(o);
						else onpreview(o);
					}}
					onkeydown={(e) => {
						if (e.key === 'Enter') {
							e.stopPropagation();
							if (o.kind === 'folder') onopen(o);
							else onpreview(o);
						}
					}}
					class="cursor-pointer text-stw-fg {o.kind === 'folder' ? 'font-medium' : 'font-normal'}"
				>
					{o.displayName}
				</span>
			</span>
		</td>
		<td
			class={cellPadCls +
				' text-right align-middle font-mono text-[12px] text-stw-fg-mute tabular-nums'}
		>
			{o.kind === 'folder' ? folderSizeText(o.key) : bytes(o.size)}
		</td>
		<td class={cellPadCls + ' align-middle font-mono text-[12px] text-stw-fg-mute'}>
			{o.modified ? new Date(o.modified).toLocaleString() : '—'}
		</td>
		<td class={cellPadCls + ' align-middle font-mono text-[12px] text-stw-fg-mute'}>
			{o.ct || (o.kind === 'folder' ? 'folder' : '—')}
		</td>
		<td class={cellPadCls + ' text-right align-middle'} onclick={(e) => e.stopPropagation()}>
			{#if o.kind !== 'folder'}
				<span
					class="inline-flex gap-0.5 transition-opacity duration-[120ms] group-hover:opacity-100 {sel
						? 'opacity-100'
						: 'opacity-0'}"
				>
					{#snippet shareIcon()}<Share2 size={13} strokeWidth={1.7} />{/snippet}
					{#snippet downloadIcon()}<Download size={13} strokeWidth={1.7} />{/snippet}
					<Tooltip text="Share">
						<IconButton label="Share" size={24} icon={shareIcon} onclick={() => onshare(o)} />
					</Tooltip>
					<Tooltip text="Download">
						<IconButton
							label="Download"
							size={24}
							icon={downloadIcon}
							onclick={() => ondownload?.(o)}
						/>
					</Tooltip>
				</span>
			{/if}
		</td>
	{/snippet}
</DataTable>
