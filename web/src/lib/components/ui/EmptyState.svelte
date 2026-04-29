<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	type Variant = 'centered' | 'card';

	interface Props {
		icon?: Snippet;
		title?: string;
		hint?: string;
		action?: Snippet;
		children?: Snippet;
		variant?: Variant;
	}

	let { icon, title, hint, action, children, variant = 'centered' }: Props = $props();
</script>

{#if variant === 'card'}
	<div
		class="flex flex-col items-center gap-2.5 rounded-lg border border-dashed border-[var(--stw-border)] bg-[var(--stw-bg-panel)] p-10 text-[13px] text-[var(--stw-fg-mute)]"
	>
		{#if icon}
			<span class="text-[var(--stw-fg-soft)]">{@render icon()}</span>
		{/if}
		{#if title}
			<div class="text-[14px] font-medium text-[var(--stw-fg)]">{title}</div>
		{/if}
		{#if hint}
			<div class="text-[12.5px] text-[var(--stw-fg-mute)]">{hint}</div>
		{/if}
		{#if children}{@render children()}{/if}
		{#if action}{@render action()}{/if}
	</div>
{:else}
	<div class="flex flex-1 flex-col items-center justify-center gap-2.5 p-10">
		{#if icon}
			<span class="text-[var(--stw-fg-soft)]">{@render icon()}</span>
		{/if}
		{#if title}
			<div class="text-[14px] font-medium">{title}</div>
		{/if}
		{#if hint}
			<div class="text-[12.5px] text-[var(--stw-fg-mute)]">{hint}</div>
		{/if}
		{#if children}{@render children()}{/if}
		{#if action}{@render action()}{/if}
	</div>
{/if}
