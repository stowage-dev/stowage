<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Props {
		title?: string;
		description?: string;
		icon?: Snippet;
		actions?: Snippet;
		header?: Snippet;
		children?: Snippet;
		footer?: Snippet;
		padded?: boolean;
		class?: string;
	}

	let {
		title,
		description,
		icon,
		actions,
		header,
		children,
		footer,
		padded = true,
		class: klass = ''
	}: Props = $props();

	const showHeader = $derived(!!(header || title || description || icon || actions));
</script>

<section class="rounded-xl border border-stw-border bg-stw-bg-panel shadow-stw-xs {klass}">
	{#if showHeader}
		{#if header}
			{@render header()}
		{:else}
			<header class="flex items-start gap-3 border-b border-stw-border px-4 py-3">
				{#if icon}
					<span class="mt-[2px] inline-flex items-center justify-center text-stw-fg-mute">
						{@render icon()}
					</span>
				{/if}
				<div class="min-w-0 flex-1">
					{#if title}
						<h2 class="m-0 text-[14px] font-semibold text-stw-fg">{title}</h2>
					{/if}
					{#if description}
						<p class="m-0 mt-0.5 text-[12px] leading-[1.45] text-stw-fg-mute">
							{description}
						</p>
					{/if}
				</div>
				{#if actions}
					<div class="flex flex-shrink-0 items-center gap-2">{@render actions()}</div>
				{/if}
			</header>
		{/if}
	{/if}
	{#if children}
		<div class={padded ? 'p-4' : ''}>{@render children()}</div>
	{/if}
	{#if footer}
		<footer
			class="flex items-center gap-2 rounded-b-xl border-t border-stw-border bg-stw-bg-sunken px-4 py-3"
		>
			{@render footer()}
		</footer>
	{/if}
</section>
