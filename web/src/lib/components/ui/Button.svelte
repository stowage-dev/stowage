<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	type Variant = 'default' | 'primary' | 'ghost' | 'danger';
	type Size = 'sm' | 'md' | 'lg';

	interface Props {
		variant?: Variant;
		size?: Size;
		icon?: Snippet;
		children?: Snippet;
		onclick?: (e: MouseEvent) => void;
		disabled?: boolean;
		title?: string;
		type?: 'button' | 'submit' | 'reset';
		class?: string;
	}

	let {
		variant = 'default',
		size = 'md',
		icon,
		children,
		onclick,
		disabled = false,
		title,
		type = 'button',
		class: klass = ''
	}: Props = $props();

	const variantCls: Record<Variant, string> = {
		default: '',
		primary: 'stw-btn--primary',
		ghost: 'stw-btn--ghost',
		danger: 'stw-btn--danger'
	};
	const sizeCls: Record<Size, string> = {
		sm: 'stw-btn--sm',
		md: '',
		lg: 'stw-btn--lg'
	};

	const cls = $derived(
		`stw-btn ${variantCls[variant]} ${sizeCls[size]} disabled:cursor-not-allowed disabled:opacity-50 ${klass}`
	);
</script>

<button {type} {onclick} {disabled} {title} class={cls}>
	{#if icon}{@render icon()}{/if}
	{#if children}{@render children()}{/if}
</button>
