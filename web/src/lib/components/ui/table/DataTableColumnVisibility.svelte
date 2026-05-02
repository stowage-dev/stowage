<script lang="ts" generics="TData">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { Columns3 } from 'lucide-svelte';
	import type { Table } from '@tanstack/table-core';
	import Button from '$lib/components/ui/Button.svelte';

	interface Props {
		table: Table<TData>;
		label?: string;
	}

	let { table, label = 'Columns' }: Props = $props();

	let open = $state(false);
	let popover: HTMLDivElement | null = $state(null);

	const hidableColumns = $derived(table.getAllLeafColumns().filter((c) => c.getCanHide()));

	function onDocClick(e: MouseEvent): void {
		if (!open) return;
		if (popover && !popover.contains(e.target as Node)) open = false;
	}

	$effect(() => {
		if (typeof document === 'undefined') return;
		document.addEventListener('mousedown', onDocClick);
		return () => document.removeEventListener('mousedown', onDocClick);
	});
</script>

<div class="relative inline-block" bind:this={popover}>
	{#snippet icon()}<Columns3 size={12} strokeWidth={1.7} />{/snippet}
	<Button size="sm" {icon} onclick={() => (open = !open)}>{label}</Button>
	{#if open}
		<div
			class="absolute right-0 z-20 mt-1 flex min-w-[200px] flex-col gap-0.5 rounded-md border border-stw-border bg-stw-bg-panel p-1 text-[12.5px] shadow-stw-md"
			role="menu"
		>
			{#each hidableColumns as col (col.id)}
				{@const headerLabel =
					typeof col.columnDef.header === 'string' && col.columnDef.header !== ''
						? col.columnDef.header
						: col.id}
				<label
					class="flex cursor-pointer items-center gap-2 rounded px-2 py-1 hover:bg-stw-bg-hover"
				>
					<input
						type="checkbox"
						class="stw-check"
						checked={col.getIsVisible()}
						onchange={(e) => col.toggleVisibility((e.target as HTMLInputElement).checked)}
					/>
					<span>{headerLabel}</span>
				</label>
			{/each}
		</div>
	{/if}
</div>
