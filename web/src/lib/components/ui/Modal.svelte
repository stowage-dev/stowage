<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount } from 'svelte';
	import { X } from 'lucide-svelte';
	import type { Snippet } from 'svelte';

	type Variant = 'center' | 'drawer-right';

	interface Props {
		title: string;
		subtitle?: string;
		subtitleMono?: boolean;
		variant?: Variant;
		maxWidth?: string;
		busy?: boolean;
		closeOnBackdrop?: boolean;
		closeOnEscape?: boolean;
		showClose?: boolean;
		icon?: Snippet;
		header?: Snippet;
		children?: Snippet;
		footer?: Snippet;
		onclose: () => void;
	}

	let {
		title,
		subtitle,
		subtitleMono = false,
		variant = 'center',
		maxWidth,
		busy = false,
		closeOnBackdrop = true,
		closeOnEscape = true,
		showClose = true,
		icon,
		header,
		children,
		footer,
		onclose
	}: Props = $props();

	let dialogEl: HTMLDivElement | null = $state(null);

	onMount(() => {
		dialogEl?.focus();
		const onKey = (e: KeyboardEvent): void => {
			if (busy || !closeOnEscape) return;
			if (e.key === 'Escape') {
				e.preventDefault();
				onclose();
			}
		};
		window.addEventListener('keydown', onKey);
		return () => window.removeEventListener('keydown', onKey);
	});

	const isDrawer = $derived(variant === 'drawer-right');
</script>

<div
	role="presentation"
	onclick={() => {
		if (!busy && closeOnBackdrop) onclose();
	}}
	class="fixed inset-0 z-50 flex animate-[stw-fade-in_120ms_ease-out] bg-black/35 {isDrawer
		? 'justify-end'
		: 'items-center justify-center'}"
>
	<div
		bind:this={dialogEl}
		role="dialog"
		aria-modal="true"
		aria-label={title}
		tabindex="-1"
		onclick={(e) => e.stopPropagation()}
		onkeydown={(e) => e.stopPropagation()}
		style={maxWidth ? `max-width:${maxWidth};` : ''}
		class="flex flex-col overflow-hidden border border-stw-border bg-stw-bg-panel shadow-stw-lg {isDrawer
			? 'h-full w-[420px] max-w-[calc(100vw-24px)] animate-[stw-slide-in-right_180ms_cubic-bezier(0.4,0,0.2,1)] rounded-l-xl border-r-0'
			: 'max-h-[calc(100vh-48px)] w-[440px] max-w-[calc(100vw-24px)] animate-[stw-zoom-in_160ms_cubic-bezier(0.4,0,0.2,1)] rounded-xl'}"
	>
		{#if header}
			{@render header()}
		{:else}
			<header class="flex items-center gap-2.5 border-b border-stw-border px-[18px] py-3.5">
				{#if icon}
					<span class="inline-flex items-center justify-center text-stw-accent-600">
						{@render icon()}
					</span>
				{/if}
				<div class="min-w-0 flex-1">
					<div class="truncate text-[14px] font-semibold text-stw-fg">{title}</div>
					{#if subtitle}
						<div
							class="mt-0.5 truncate text-[11.5px] text-stw-fg-soft {subtitleMono
								? 'font-mono'
								: ''}"
						>
							{subtitle}
						</div>
					{/if}
				</div>
				{#if showClose}
					<button
						type="button"
						onclick={onclose}
						disabled={busy}
						class="inline-flex h-[26px] w-[26px] cursor-pointer items-center justify-center rounded-[5px] border-0 bg-transparent text-stw-fg-mute focus-ring hover:bg-stw-bg-hover disabled:cursor-not-allowed disabled:opacity-50"
						aria-label="Close"
					>
						<X size={14} strokeWidth={1.7} />
					</button>
				{/if}
			</header>
		{/if}

		{#if children}
			<div class="stw-scroll min-h-0 flex-1 overflow-auto">
				{@render children()}
			</div>
		{/if}

		{#if footer}
			<footer class="flex justify-end gap-2 border-t border-stw-border bg-stw-bg-panel px-3.5 py-3">
				{@render footer()}
			</footer>
		{/if}
	</div>
</div>
