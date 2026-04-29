<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { Search, Menu } from 'lucide-svelte';
	import Kbd from '$lib/components/ui/Kbd.svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import Breadcrumb, { type Crumb } from './Breadcrumb.svelte';

	interface Props {
		variant?: 'breadcrumb' | 'search';
		crumbs: Crumb[];
		oncmdk?: () => void;
		onnavigate?: (c: Crumb, i: number) => void;
		onmenu?: () => void;
	}

	let { variant = 'breadcrumb', crumbs, oncmdk, onnavigate, onmenu }: Props = $props();

	let hovered = $state(false);
</script>

{#snippet searchBox()}
	<button
		type="button"
		onclick={oncmdk}
		class="stw-focus stw-topbar-search flex h-[28px] cursor-text items-center gap-2 rounded-[7px] border bg-[var(--stw-bg-sunken)] py-0 pr-2 pl-2.5 text-[13px] text-[var(--stw-fg-mute)] transition-[border-color,background] duration-[120ms] {hovered
			? 'border-[var(--stw-border-strong)]'
			: 'border-[var(--stw-border)]'}"
		class:wide={variant === 'search'}
		onmouseenter={() => (hovered = true)}
		onmouseleave={() => (hovered = false)}
	>
		<Search size={14} strokeWidth={1.7} />
		<span class="flex-1 truncate text-left">Search buckets, objects, users…</span>
		<Kbd>/</Kbd>
	</button>
{/snippet}

{#snippet menuBtn()}
	{#snippet menuIcon()}<Menu size={16} strokeWidth={1.7} />{/snippet}
	<span class="stw-mobile-only">
		<IconButton label="Menu" icon={menuIcon} onclick={onmenu} />
	</span>
{/snippet}

<header
	class="flex h-[51px] flex-shrink-0 items-center gap-3 border-b border-[var(--stw-border)] bg-[var(--stw-bg-panel)] px-3.5"
>
	{@render menuBtn()}
	{#if variant === 'search'}
		{@render searchBox()}
		<div class="stw-desktop-only flex min-w-0 flex-1 justify-start">
			<Breadcrumb {crumbs} {onnavigate} />
		</div>
	{:else}
		<span class="stw-desktop-only flex min-w-0 flex-1">
			<Breadcrumb {crumbs} {onnavigate} />
		</span>
		{@render searchBox()}
	{/if}
</header>
