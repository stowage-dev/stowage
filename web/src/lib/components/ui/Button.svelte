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
		style?: string;
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
		class: klass = '',
		style = ''
	}: Props = $props();

	const variantCls = {
		default: '',
		primary: ' stw-btn--primary',
		ghost: ' stw-btn--ghost',
		danger: ' stw-btn--danger'
	};
	const sizeCls = { sm: ' stw-btn--sm', md: '', lg: ' stw-btn--lg' };

	const cls = $derived('stw-btn' + variantCls[variant] + sizeCls[size] + ' stw-focus ' + klass);
	const styleStr = $derived((disabled ? 'opacity:.5;cursor:not-allowed;' : '') + style);
</script>

<button {type} {onclick} {disabled} {title} class={cls} style={styleStr}>
	{#if icon}{@render icon()}{/if}
	{#if children}{@render children()}{/if}
</button>
