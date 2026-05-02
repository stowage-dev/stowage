<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Props {
		children: Snippet;
		actions?: Snippet;
		minColumn?: string;
	}

	let { children, actions, minColumn = '160px' }: Props = $props();
</script>

<section
	class="grid items-end gap-3 rounded-lg border border-stw-border bg-stw-bg-panel p-3"
	style="grid-template-columns:repeat(auto-fit,minmax({minColumn},1fr));"
>
	{@render children()}
	{#if actions}
		<div class="flex items-center justify-end gap-2">{@render actions()}</div>
	{/if}
</section>
