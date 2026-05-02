<script lang="ts" generics="TData">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-svelte';
	import type { Table } from '@tanstack/table-core';
	import { num } from '$lib/format';

	interface Props {
		table: Table<TData>;
		pageSizes?: number[];
	}

	let { table, pageSizes = [25, 50, 100, 250] }: Props = $props();

	const state = $derived(table.getState().pagination);
	const total = $derived(table.getFilteredRowModel().rows.length);
	const pageCount = $derived(table.getPageCount());
	const start = $derived(total === 0 ? 0 : state.pageIndex * state.pageSize + 1);
	const end = $derived(Math.min(total, (state.pageIndex + 1) * state.pageSize));

	const btnCls =
		'stw-focus inline-flex h-6 w-6 items-center justify-center rounded-md text-[var(--stw-fg-mute)] hover:bg-[var(--stw-bg-hover)] hover:text-[var(--stw-fg)] disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-[var(--stw-fg-mute)]';
</script>

<div
	class="flex flex-wrap items-center justify-between gap-3 border-t border-[var(--stw-border)] bg-[var(--stw-bg-panel)] px-3 py-2 text-[12px] text-[var(--stw-fg-mute)]"
>
	<div class="flex items-center gap-2">
		<span class="font-mono">{num(start)}–{num(end)} of {num(total)}</span>
		<label class="ml-2 inline-flex items-center gap-1.5">
			<span class="text-[11.5px] text-[var(--stw-fg-soft)]">Rows</span>
			<select
				class="stw-input h-6 py-0 pr-5 pl-2 font-mono text-[11.5px]"
				value={state.pageSize}
				onchange={(e) => table.setPageSize(Number((e.target as HTMLSelectElement).value))}
			>
				{#each pageSizes as ps (ps)}
					<option value={ps}>{ps}</option>
				{/each}
			</select>
		</label>
	</div>

	<div class="flex items-center gap-2">
		<span class="font-mono">
			Page {num(state.pageIndex + 1)} / {num(Math.max(1, pageCount))}
		</span>
		<span class="inline-flex gap-0.5">
			<button
				type="button"
				class={btnCls}
				aria-label="First page"
				title="First"
				onclick={() => table.setPageIndex(0)}
				disabled={!table.getCanPreviousPage()}
			>
				<ChevronsLeft size={13} strokeWidth={1.7} />
			</button>
			<button
				type="button"
				class={btnCls}
				aria-label="Previous page"
				title="Previous"
				onclick={() => table.previousPage()}
				disabled={!table.getCanPreviousPage()}
			>
				<ChevronLeft size={13} strokeWidth={1.7} />
			</button>
			<button
				type="button"
				class={btnCls}
				aria-label="Next page"
				title="Next"
				onclick={() => table.nextPage()}
				disabled={!table.getCanNextPage()}
			>
				<ChevronRight size={13} strokeWidth={1.7} />
			</button>
			<button
				type="button"
				class={btnCls}
				aria-label="Last page"
				title="Last"
				onclick={() => table.setPageIndex(pageCount - 1)}
				disabled={!table.getCanNextPage()}
			>
				<ChevronsRight size={13} strokeWidth={1.7} />
			</button>
		</span>
	</div>
</div>
