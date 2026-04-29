<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	type Align = 'start' | 'end' | 'between';

	interface Props {
		align?: Align;
		children: Snippet;
		leading?: Snippet;
	}

	let { align = 'end', children, leading }: Props = $props();

	const justify = $derived(
		align === 'start' ? 'justify-start' : align === 'between' ? 'justify-between' : 'justify-end'
	);
</script>

<div class="flex items-center gap-2 {justify}">
	{#if leading}
		<div class="flex items-center gap-2">{@render leading()}</div>
	{/if}
	<div class="flex items-center gap-2">{@render children()}</div>
</div>
