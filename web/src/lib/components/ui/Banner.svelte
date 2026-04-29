<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { X, AlertTriangle, AlertOctagon, Info, CheckCircle2 } from 'lucide-svelte';
	import type { Snippet } from 'svelte';

	type Variant = 'info' | 'ok' | 'warn' | 'err';

	interface Props {
		variant?: Variant;
		title?: string;
		dismissible?: boolean;
		ondismiss?: () => void;
		icon?: Snippet;
		children?: Snippet;
		actions?: Snippet;
		role?: 'alert' | 'status' | 'note';
	}

	let {
		variant = 'info',
		title,
		dismissible = false,
		ondismiss,
		icon,
		children,
		actions,
		role = 'note'
	}: Props = $props();

	const tokenBase = $derived(
		variant === 'err'
			? '--stw-err'
			: variant === 'warn'
				? '--stw-warn'
				: variant === 'ok'
					? '--stw-ok'
					: '--stw-accent-500'
	);

	const bgStyle = $derived(
		`background:color-mix(in oklch, var(${tokenBase}) 10%, var(--stw-bg-panel));` +
			`border:1px solid color-mix(in oklch, var(${tokenBase}) 35%, var(--stw-border));` +
			`color:var(${tokenBase === '--stw-accent-500' ? '--stw-fg' : tokenBase});`
	);
</script>

<div
	{role}
	class="flex items-start gap-2.5 rounded-lg px-3.5 py-3 text-[13px] leading-[1.5]"
	style={bgStyle}
>
	<span class="mt-[1px] inline-flex flex-shrink-0 items-center justify-center">
		{#if icon}
			{@render icon()}
		{:else if variant === 'err'}
			<AlertOctagon size={15} strokeWidth={1.8} />
		{:else if variant === 'warn'}
			<AlertTriangle size={15} strokeWidth={1.8} />
		{:else if variant === 'ok'}
			<CheckCircle2 size={15} strokeWidth={1.8} />
		{:else}
			<Info size={15} strokeWidth={1.8} />
		{/if}
	</span>
	<div class="min-w-0 flex-1">
		{#if title}
			<div class="text-[13px] font-semibold">{title}</div>
		{/if}
		{#if children}
			<div class={title ? 'mt-0.5' : ''}>{@render children()}</div>
		{/if}
		{#if actions}
			<div class="mt-2 flex items-center gap-2">{@render actions()}</div>
		{/if}
	</div>
	{#if dismissible && ondismiss}
		<button
			type="button"
			onclick={ondismiss}
			aria-label="Dismiss"
			class="stw-focus inline-flex h-[22px] w-[22px] cursor-pointer items-center justify-center rounded border-0 bg-transparent text-current opacity-70 hover:opacity-100"
		>
			<X size={13} strokeWidth={1.8} />
		</button>
	{/if}
</div>
