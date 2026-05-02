<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Props {
		used: number;
		soft?: number | null;
		hard?: number | null;
		total?: number | null;
		stats?: Snippet;
	}

	let { used, soft = null, hard = null, total = null, stats }: Props = $props();

	const cap = $derived(total ?? hard ?? 0);
	const pct = $derived(cap > 0 ? Math.min(100, (used * 100) / cap) : 0);
	const overSoft = $derived(soft != null && soft > 0 && used >= soft);
	const danger = $derived(pct >= 90);

	const fillCls = $derived(danger ? 'bg-stw-err' : overSoft ? 'bg-stw-warn' : 'bg-stw-accent-500');
</script>

<div class="flex flex-col gap-1.5">
	<div class="h-2 overflow-hidden rounded-full border border-stw-border bg-stw-bg-sunken">
		{#if cap > 0}
			<div class="h-full transition-[width] duration-200 {fillCls}" style="width:{pct}%;"></div>
		{/if}
	</div>
	{#if stats}
		<div class="flex flex-wrap gap-3 font-mono text-[12px] text-stw-fg-mute">
			{@render stats()}
		</div>
	{/if}
</div>
