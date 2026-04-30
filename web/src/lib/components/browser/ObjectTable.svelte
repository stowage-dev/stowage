<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { Share2, Download } from 'lucide-svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import Tooltip from '$lib/components/ui/Tooltip.svelte';
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
		ondownload
	}: Props = $props();

	function folderSizeText(key: string): string {
		const s = folderSizes[key];
		if (s === undefined) return '—';
		if (s === 'loading') return '…';
		if (s === 'error') return '?';
		return bytes(s);
	}

	const rowHCls = $derived(
		density === 'compact' ? 'h-[32px]' : density === 'cosy' ? 'h-[40px]' : 'h-[48px]'
	);
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

	const headThCls =
		'text-left text-[11.5px] font-medium tracking-[0.02em] text-[var(--stw-fg-mute)]';
</script>

<div class="stw-scroll flex-1 overflow-auto bg-[var(--stw-bg-panel)]">
	<table class="w-full border-separate border-spacing-0 text-[13px]">
		<thead>
			<tr
				class="sticky top-0 z-[1] h-[30px] bg-[var(--stw-bg-panel)] shadow-[inset_0_-1px_0_var(--stw-border)]"
			>
				<th class="w-[32px] {cellPadLCls} text-left align-middle">
					<input
						type="checkbox"
						class="stw-check"
						aria-label="Select all"
						checked={selected.length > 0 && selected.length === items.length}
						use:indeterminate
						onchange={(e) => selectAll((e.target as HTMLInputElement).checked)}
					/>
				</th>
				<th class="{headThCls} {cellPadCls}">Name</th>
				<th class="{headThCls} {cellPadCls} w-[110px] text-right">Size</th>
				<th class="{headThCls} {cellPadCls} w-[170px]">Modified</th>
				<th class="{headThCls} {cellPadCls} w-[130px]">Type</th>
				<th class="w-[60px]"></th>
			</tr>
		</thead>
		<tbody>
			{#each items as o (o.key)}
				{@const sel = selected.includes(o.key)}
				<tr
					class="group cursor-pointer shadow-[inset_0_-1px_0_var(--stw-border)] {rowHCls} {sel
						? 'bg-[var(--stw-bg-sel)]'
						: 'hover:bg-[var(--stw-bg-hover)]'}"
					onclick={(e) => onRowClick(o.key, e)}
					ondblclick={() => (o.kind === 'folder' ? onopen(o) : onpreview(o))}
				>
					<td class="{cellPadLCls} align-middle">
						<input
							type="checkbox"
							class="stw-check"
							aria-label={sel ? `Deselect ${o.displayName}` : `Select ${o.displayName}`}
							checked={sel}
							onclick={(e) => onCheckboxClick(o.key, e)}
						/>
					</td>
					<td class={cellPadCls}>
						<span class="inline-flex items-center gap-2.5">
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
								class="cursor-pointer text-[var(--stw-fg)] {o.kind === 'folder'
									? 'font-medium'
									: 'font-normal'}"
							>
								{o.displayName}
							</span>
						</span>
					</td>
					<td
						class="{cellPadCls} text-right font-mono text-[12px] text-[var(--stw-fg-mute)] tabular-nums"
					>
						{o.kind === 'folder' ? folderSizeText(o.key) : bytes(o.size)}
					</td>
					<td class="{cellPadCls} font-mono text-[12px] text-[var(--stw-fg-mute)]">
						{o.modified ? new Date(o.modified).toLocaleString() : '—'}
					</td>
					<td class="{cellPadCls} font-mono text-[12px] text-[var(--stw-fg-mute)]">
						{o.ct || (o.kind === 'folder' ? 'folder' : '—')}
					</td>
					<td class="{cellPadCls} text-right" onclick={(e) => e.stopPropagation()}>
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
				</tr>
			{/each}
		</tbody>
	</table>
</div>
