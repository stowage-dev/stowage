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

	const h = $derived(size === 'sm' ? 24 : 28);
</script>

<div
	role="radiogroup"
	style="display:inline-flex;
        padding:2px;
        gap:0;
        background:var(--stw-bg-sunken);
        border:1px solid var(--stw-border);
        border-radius:7px;
        height:{h}px;"
>
	{#each options as opt (opt.value)}
		{@const active = opt.value === value}
		<button
			type="button"
			role="radio"
			aria-checked={active}
			onclick={() => onchange(opt.value)}
			class="stw-focus"
			style="display:inline-flex;
                align-items:center;
                gap:4px;padding:0 10px;
                height:{h - 5}px;
                font-size:12px;
                font-weight:500;
                background:{active ? 'var(--stw-bg-panel)' : 'transparent'};color:{active
				? 'var(--stw-fg)'
				: 'var(--stw-fg-mute)'};border:0;border-radius:5px;box-shadow:{active
				? 'var(--stw-shadow-xs)'
				: 'none'};cursor:pointer;"
		>
			{#if opt.icon}{@render opt.icon()}{/if}
			{opt.label}
		</button>
	{/each}
</div>
