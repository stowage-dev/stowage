<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { Check, Copy } from 'lucide-svelte';
	import type { Snippet } from 'svelte';

	interface Props {
		value: string;
		label?: string;
		mono?: boolean;
		size?: 'sm' | 'md';
		ariaLabel?: string;
		children?: Snippet;
	}

	let { value, label = 'Copy', mono = true, size = 'md', ariaLabel, children }: Props = $props();

	let copied = $state(false);
	let timer: ReturnType<typeof setTimeout> | null = null;

	function copy(): void {
		if (!value) return;
		navigator.clipboard?.writeText(value);
		copied = true;
		if (timer) clearTimeout(timer);
		timer = setTimeout(() => (copied = false), 1400);
	}

	const padCls = $derived(size === 'sm' ? 'px-2 py-1' : 'px-3 py-2.5');
	const textCls = $derived(size === 'sm' ? 'text-[11.5px]' : 'text-[12.5px]');
</script>

<div
	class="flex items-center gap-2 overflow-hidden rounded-md border border-stw-border bg-stw-bg-sunken {padCls} {textCls} {mono
		? 'font-mono'
		: ''}"
>
	<span class="min-w-0 flex-1 truncate">
		{#if children}{@render children()}{:else}{value}{/if}
	</span>
	<button
		type="button"
		onclick={copy}
		class="inline-flex flex-shrink-0 cursor-pointer items-center gap-1 rounded border border-stw-border bg-transparent px-2 py-0.5 text-[11.5px] focus-ring {copied
			? 'text-stw-ok'
			: 'text-stw-fg-mute'} hover:bg-stw-bg-hover hover:text-stw-fg"
		aria-label={ariaLabel ?? label}
	>
		{#if copied}
			<Check size={12} strokeWidth={1.7} /> Copied
		{:else}
			<Copy size={12} strokeWidth={1.7} /> {label}
		{/if}
	</button>
</div>
