<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import Chevron from '$lib/components/ui/Chevron.svelte';

	export interface Crumb {
		label: string;
		mono?: boolean;
		href?: string;
	}

	interface Props {
		crumbs: Crumb[];
		onnavigate?: (c: Crumb, i: number) => void;
	}

	let { crumbs, onnavigate }: Props = $props();
</script>

<nav aria-label="breadcrumb" class="flex min-w-0 flex-1 items-center gap-0.5">
	{#each crumbs as c, i (i)}
		{@const last = i === crumbs.length - 1}
		{#if i > 0}
			<span class="inline-flex items-center px-1 text-stw-fg-soft">
				<Chevron size={10} dir="right" />
			</span>
		{/if}
		<button
			type="button"
			onclick={() => !last && onnavigate?.(c, i)}
			class="inline-flex h-[24px] max-w-[200px] items-center gap-1.5 truncate rounded border-0 bg-transparent px-1.5 text-[13px] focus-ring {last
				? 'cursor-default font-medium text-stw-fg'
				: 'cursor-pointer font-normal text-stw-fg-mute hover:bg-stw-bg-hover hover:text-stw-fg'} {c.mono
				? 'font-mono'
				: ''}"
		>
			{c.label}
		</button>
	{/each}
</nav>
