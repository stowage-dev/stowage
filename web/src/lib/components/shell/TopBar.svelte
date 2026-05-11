<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { Search, Menu } from 'lucide-svelte';
	import Kbd from '$lib/components/ui/Kbd.svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import Breadcrumb, { type Crumb } from './Breadcrumb.svelte';

	interface Props {
		crumbs: Crumb[];
		oncmdk?: () => void;
		onnavigate?: (c: Crumb, i: number) => void;
		onmenu?: () => void;
	}

	let { crumbs, oncmdk, onnavigate, onmenu }: Props = $props();

	let hovered = $state(false);
</script>

{#snippet menuIcon()}<Menu size={16} strokeWidth={1.7} />{/snippet}

<header
	class="flex h-[51px] flex-shrink-0 items-center gap-3 border-b border-stw-border bg-stw-bg-panel px-3.5"
>
	<span class="stw-mobile-only">
		<IconButton label="Menu" icon={menuIcon} onclick={onmenu} />
	</span>
	<button
		type="button"
		onclick={oncmdk}
		class="wide flex h-[28px] stw-topbar-search cursor-text items-center gap-2 rounded-[7px] border bg-stw-bg-sunken py-0 pr-2 pl-2.5 text-[13px] text-stw-fg-mute focus-ring transition-[border-color,background] duration-[120ms] {hovered
			? 'border-stw-border-strong'
			: 'border-stw-border'}"
		onmouseenter={() => (hovered = true)}
		onmouseleave={() => (hovered = false)}
	>
		<Search size={14} strokeWidth={1.7} />
		<span class="flex-1 truncate text-left">Search buckets, objects, users…</span>
		<Kbd>/</Kbd>
	</button>
	<div class="stw-desktop-only flex min-w-0 flex-1 justify-start">
		<Breadcrumb {crumbs} {onnavigate} />
	</div>
</header>
