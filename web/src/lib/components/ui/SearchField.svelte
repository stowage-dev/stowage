<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount } from 'svelte';
	import { Search, X } from 'lucide-svelte';
	import type { Snippet } from 'svelte';

	interface Props {
		id?: string;
		value: string;
		placeholder?: string;
		width?: string;
		size?: 'sm' | 'md';
		autofocus?: boolean;
		clearable?: boolean;
		ariaLabel?: string;
		ref?: HTMLInputElement | null;
		onkeydown?: (e: KeyboardEvent) => void;
		oninput?: (e: Event) => void;
		trailing?: Snippet;
	}

	let {
		id,
		value = $bindable(),
		placeholder = 'Search',
		width,
		size = 'md',
		autofocus: shouldFocus = false,
		clearable = true,
		ariaLabel,
		ref = $bindable(null),
		onkeydown,
		oninput,
		trailing
	}: Props = $props();

	onMount(() => {
		if (shouldFocus) ref?.focus();
	});

	const heightCls = $derived(size === 'sm' ? 'h-[28px]' : 'h-[32px]');
	const iconTopCls = $derived(size === 'sm' ? 'top-[7px]' : 'top-[9px]');
	const clearBtnTopCls = $derived(size === 'sm' ? 'top-[5px]' : 'top-[7px]');
</script>

<div class="relative inline-flex items-center" style={width ? `width:${width};` : ''}>
	<span class="absolute left-[10px] {iconTopCls} pointer-events-none inline-flex text-stw-fg-soft">
		<Search size={13} strokeWidth={1.7} />
	</span>
	<input
		{id}
		bind:this={ref}
		bind:value
		{placeholder}
		aria-label={ariaLabel ?? placeholder}
		class="stw-input pl-[30px] {trailing || (clearable && value) ? 'pr-[34px]' : ''} {heightCls}"
		{onkeydown}
		{oninput}
	/>
	{#if trailing}
		<span class="absolute right-[8px] {iconTopCls} inline-flex">{@render trailing()}</span>
	{:else if clearable && value}
		<button
			type="button"
			aria-label="Clear search"
			onclick={() => (value = '')}
			class="absolute right-[6px] {clearBtnTopCls} inline-flex h-[20px] w-[20px] cursor-pointer items-center justify-center rounded border-0 bg-transparent text-stw-fg-soft focus-ring hover:bg-stw-bg-hover hover:text-stw-fg"
		>
			<X size={12} strokeWidth={1.8} />
		</button>
	{/if}
</div>
