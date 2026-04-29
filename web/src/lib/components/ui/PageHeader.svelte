<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		subtitle?: string;
		icon?: Snippet;
		meta?: Snippet;
		actions?: Snippet;
		size?: 'sm' | 'md' | 'lg';
	}

	let { title, subtitle, icon, meta, actions, size = 'md' }: Props = $props();

	const titleSize = $derived(
		size === 'lg' ? 'text-[24px]' : size === 'sm' ? 'text-[16px]' : 'text-[20px]'
	);
</script>

<header class="mb-4 flex items-start gap-3">
	{#if icon}
		<span class="mt-[2px] inline-flex items-center justify-center text-[var(--stw-fg-mute)]">
			{@render icon()}
		</span>
	{/if}
	<div class="min-w-0 flex-1">
		<h1 class="{titleSize} m-0 truncate leading-tight font-semibold text-[var(--stw-fg)]">
			{title}
		</h1>
		{#if subtitle}
			<p class="m-0 mt-1 text-[13px] leading-[1.45] text-[var(--stw-fg-mute)]">{subtitle}</p>
		{/if}
		{#if meta}
			<div class="mt-1 flex flex-wrap items-center gap-2 text-[12px] text-[var(--stw-fg-soft)]">
				{@render meta()}
			</div>
		{/if}
	</div>
	{#if actions}
		<div class="flex flex-shrink-0 items-center gap-2">{@render actions()}</div>
	{/if}
</header>
