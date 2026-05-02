<script lang="ts" generics="T extends string">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Option {
		value: T;
		label: string;
		icon?: Snippet;
	}

	interface Props {
		value: T;
		onchange: (v: T) => void;
		options: Option[];
		size?: 'sm' | 'md';
	}

	let { value, onchange, options, size = 'md' }: Props = $props();

	const groupCls = $derived(
		size === 'sm'
			? 'inline-flex h-6 gap-0 rounded-[7px] border border-stw-border bg-stw-bg-sunken p-0.5'
			: 'inline-flex h-7 gap-0 rounded-[7px] border border-stw-border bg-stw-bg-sunken p-0.5'
	);
	const itemHCls = $derived(size === 'sm' ? 'h-[19px]' : 'h-[23px]');
</script>

<div role="radiogroup" class={groupCls}>
	{#each options as opt (opt.value)}
		{@const active = opt.value === value}
		<button
			type="button"
			role="radio"
			aria-checked={active}
			onclick={() => onchange(opt.value)}
			class="inline-flex focus-ring {itemHCls} cursor-pointer items-center gap-1 rounded-[5px] border-0 px-2.5 text-[12px] font-medium {active
				? 'bg-stw-bg-panel text-stw-fg shadow-stw-xs'
				: 'bg-transparent text-stw-fg-mute'}"
		>
			{#if opt.icon}{@render opt.icon()}{/if}
			{opt.label}
		</button>
	{/each}
</div>
