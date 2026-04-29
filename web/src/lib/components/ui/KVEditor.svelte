<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { Plus, X } from 'lucide-svelte';

	export type KVRow = { k: string; v: string };

	interface Props {
		rows: KVRow[];
		keyPlaceholder?: string;
		valuePlaceholder?: string;
		addLabel?: string;
		disabled?: boolean;
	}

	let {
		rows = $bindable(),
		keyPlaceholder = 'key',
		valuePlaceholder = 'value',
		addLabel,
		disabled = false
	}: Props = $props();

	const computedAddLabel = $derived(addLabel ?? `Add ${keyPlaceholder.toLowerCase()}`);

	function removeRow(i: number): void {
		rows.splice(i, 1);
	}

	function addRow(): void {
		rows.push({ k: '', v: '' });
	}
</script>

<div class="flex flex-col gap-1.5">
	{#each rows as _row, i (i)}
		<div class="flex items-center gap-1.5">
			<input
				class="stw-input h-[28px] min-w-0 flex-1 font-mono text-[12px]"
				placeholder={keyPlaceholder}
				bind:value={rows[i].k}
				{disabled}
			/>
			<input
				class="stw-input h-[28px] min-w-0 flex-[2] font-mono text-[12px]"
				placeholder={valuePlaceholder}
				bind:value={rows[i].v}
				{disabled}
			/>
			<button
				type="button"
				onclick={() => removeRow(i)}
				aria-label="Remove row"
				{disabled}
				class="stw-focus inline-flex h-[26px] w-[26px] flex-shrink-0 cursor-pointer items-center justify-center rounded-[5px] border-0 bg-transparent text-[var(--stw-fg-mute)] hover:bg-[var(--stw-bg-hover)] disabled:cursor-not-allowed disabled:opacity-50"
			>
				<X size={12} strokeWidth={1.7} />
			</button>
		</div>
	{/each}
	<button
		type="button"
		onclick={addRow}
		{disabled}
		class="stw-focus inline-flex cursor-pointer items-center gap-1 self-start rounded-[5px] border border-dashed border-[var(--stw-border)] bg-transparent px-2 py-1 text-[11.5px] text-[var(--stw-fg-mute)] hover:bg-[var(--stw-bg-hover)] disabled:cursor-not-allowed disabled:opacity-50"
	>
		<Plus size={11} strokeWidth={1.7} />
		{computedAddLabel}
	</button>
</div>
