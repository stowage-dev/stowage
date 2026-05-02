<script lang="ts" generics="T">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Column {
		key: string;
		label: string;
		align?: 'left' | 'right' | 'center';
		width?: string;
		mono?: boolean;
	}

	interface Props {
		columns: Column[];
		rows: T[];
		row: Snippet<[T, number]>;
		empty?: Snippet;
		emptyText?: string;
		rowHeight?: number;
		caption?: string;
		stickyHeader?: boolean;
	}

	let {
		columns,
		rows,
		row,
		empty,
		emptyText = 'No results.',
		rowHeight = 38,
		caption,
		stickyHeader = false
	}: Props = $props();

	function alignCls(a?: string): string {
		return a === 'right' ? 'text-right' : a === 'center' ? 'text-center' : 'text-left';
	}
</script>

<div class="overflow-hidden rounded-lg border border-stw-border bg-stw-bg-panel">
	<table class="w-full border-separate border-spacing-0 text-[13px]">
		{#if caption}
			<caption class="sr-only">{caption}</caption>
		{/if}
		<thead>
			<tr class="h-[32px] bg-stw-bg-sunken row-divider {stickyHeader ? 'sticky top-0 z-10' : ''}">
				{#each columns as col (col.key)}
					<th
						scope="col"
						class="{alignCls(
							col.align
						)} px-3 text-[11.5px] font-medium tracking-[0.04em] text-stw-fg-mute uppercase"
						style={col.width ? `width:${col.width};` : ''}
					>
						{col.label}
					</th>
				{/each}
			</tr>
		</thead>
		<tbody>
			{#each rows as item, i (i)}
				<tr class="row-divider" style="height:{rowHeight}px;">
					{@render row(item, i)}
				</tr>
			{/each}
			{#if rows.length === 0}
				<tr>
					<td colspan={columns.length} class="px-4 py-10 text-center text-[13px] text-stw-fg-soft">
						{#if empty}{@render empty()}{:else}{emptyText}{/if}
					</td>
				</tr>
			{/if}
		</tbody>
	</table>
</div>
