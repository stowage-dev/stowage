<script lang="ts" generics="TData">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { HeaderContext, Row, Table } from '@tanstack/table-core';
	import { ChevronDown, ChevronUp, ChevronsUpDown } from 'lucide-svelte';
	import type { Snippet } from 'svelte';
	import type { Align, Column, Density } from './types';

	interface Props {
		table: Table<TData>;
		row: Snippet<[TData, { row: Row<TData>; index: number }]>;
		empty?: Snippet;
		emptyText?: string;
		caption?: string;
		stickyHeader?: boolean;
		density?: Density;
		rowClass?: (row: Row<TData>) => string;
		onRowClick?: (row: Row<TData>, e: MouseEvent | KeyboardEvent) => void;
		ondblclick?: (row: Row<TData>, e: MouseEvent) => void;
		headerCell?: Snippet<[HeaderContext<TData, unknown>]>;
		headerSnippets?: Record<string, Snippet<[HeaderContext<TData, unknown>]>>;
		/** Pass a Tailwind class (e.g. `'max-h-[600px] overflow-auto'`) to make the body scroll. */
		scrollClass?: string;
		class?: string;
	}

	let {
		table,
		row,
		empty,
		emptyText = 'No results.',
		caption,
		stickyHeader = false,
		density = 'cosy',
		rowClass,
		onRowClick,
		ondblclick,
		headerCell,
		headerSnippets,
		scrollClass = '',
		class: extraClass = ''
	}: Props = $props();

	const headerGroups = $derived(table.getHeaderGroups());
	const rows = $derived(table.getRowModel().rows);
	const colCount = $derived(table.getVisibleLeafColumns().length);

	const rowHeightCls = $derived(
		density === 'compact' ? 'h-[32px]' : density === 'roomy' ? 'h-[48px]' : 'h-[40px]'
	);

	function alignCls(a?: Align): string {
		return a === 'right' ? 'text-right' : a === 'center' ? 'text-center' : 'text-left';
	}

	function colExtras(c: Column<TData>): { hClass: string; cClass: string; mono: string } {
		return {
			hClass: c.headerClass ?? '',
			cClass: c.cellClass ?? '',
			mono: c.mono ? 'font-mono' : ''
		};
	}

	function rowKeyHandler(r: Row<TData>): (e: KeyboardEvent) => void {
		return (e: KeyboardEvent) => {
			if (!onRowClick) return;
			if (e.key === 'Enter' || e.key === ' ') {
				e.preventDefault();
				onRowClick(r, e);
			}
		};
	}
</script>

<div class="overflow-hidden rounded-lg border border-stw-border bg-stw-bg-panel {extraClass}">
	<div class="stw-scroll {scrollClass || 'overflow-hidden'}">
		<table class="w-full border-separate border-spacing-0 text-[13px]">
			{#if caption}
				<caption class="sr-only">{caption}</caption>
			{/if}
			<thead>
				{#each headerGroups as hg (hg.id)}
					<tr
						class="h-[32px] bg-stw-bg-sunken row-divider {stickyHeader ? 'sticky top-0 z-10' : ''}"
					>
						{#each hg.headers as h (h.id)}
							{@const col = h.column.columnDef as Column<TData>}
							{@const sortable = h.column.getCanSort()}
							{@const sorted = h.column.getIsSorted()}
							{@const extras = colExtras(col)}
							{@const headerLabel = typeof col.header === 'string' ? col.header : ''}
							<th
								scope="col"
								class="px-3 text-[11.5px] font-medium tracking-[0.04em] text-stw-fg-mute uppercase {alignCls(
									col.align
								)} {extras.hClass}"
							>
								{#if h.isPlaceholder}
									&nbsp;
								{:else if col.headerSnippet}
									{@render col.headerSnippet(h.getContext())}
								{:else if headerSnippets?.[h.column.id]}
									{@render headerSnippets[h.column.id](h.getContext())}
								{:else if headerCell}
									{@render headerCell(h.getContext())}
								{:else if sortable}
									<button
										type="button"
										class="focus-ring inline-flex cursor-pointer items-center gap-1.5 tracking-[0.04em] uppercase select-none hover:text-stw-fg"
										onclick={h.column.getToggleSortingHandler()}
									>
										{headerLabel}
										{#if sorted === 'asc'}
											<ChevronUp size={12} strokeWidth={1.7} />
										{:else if sorted === 'desc'}
											<ChevronDown size={12} strokeWidth={1.7} />
										{:else}
											<span class="opacity-40">
												<ChevronsUpDown size={12} strokeWidth={1.7} />
											</span>
										{/if}
									</button>
								{:else}
									{headerLabel}
								{/if}
							</th>
						{/each}
					</tr>
				{/each}
			</thead>
			<tbody>
				{#each rows as r, i (r.id)}
					{@const extra = rowClass?.(r) ?? ''}
					{@const clickable = !!onRowClick}
					<tr
						class="row-divider {rowHeightCls} {clickable
							? 'cursor-pointer hover:bg-stw-bg-hover'
							: ''} {extra}"
						onclick={clickable ? (e) => onRowClick(r, e) : undefined}
						ondblclick={ondblclick ? (e) => ondblclick(r, e) : undefined}
						onkeydown={clickable ? rowKeyHandler(r) : undefined}
						role={clickable ? 'button' : undefined}
						tabindex={clickable ? 0 : undefined}
					>
						{@render row(r.original, { row: r, index: i })}
					</tr>
				{/each}
				{#if rows.length === 0}
					<tr>
						<td colspan={colCount} class="px-4 py-10 text-center text-[13px] text-stw-fg-soft">
							{#if empty}{@render empty()}{:else}{emptyText}{/if}
						</td>
					</tr>
				{/if}
			</tbody>
		</table>
	</div>
</div>
