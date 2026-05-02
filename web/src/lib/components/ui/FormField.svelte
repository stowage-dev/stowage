<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Props {
		label?: string;
		for?: string;
		helper?: string;
		error?: string;
		optional?: boolean;
		hint?: Snippet;
		children: Snippet;
	}

	let { label, for: forId, helper, error, optional = false, hint, children }: Props = $props();
</script>

<div class="flex flex-col gap-1.5">
	{#if label}
		<label for={forId} class="flex items-center gap-1.5 text-[12px] font-medium text-stw-fg-mute">
			<span>{label}</span>
			{#if optional}
				<span class="font-normal text-stw-fg-soft">(optional)</span>
			{/if}
			{#if hint}
				<span class="ml-auto text-[11.5px] font-normal text-stw-fg-soft">
					{@render hint()}
				</span>
			{/if}
		</label>
	{/if}
	{@render children()}
	{#if error}
		<div class="text-[11.5px] text-stw-err">{error}</div>
	{:else if helper}
		<div class="text-[11.5px] text-stw-fg-soft">{helper}</div>
	{/if}
</div>
