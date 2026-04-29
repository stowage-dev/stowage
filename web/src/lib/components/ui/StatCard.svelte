<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Props {
		label: string;
		value: string | number;
		sublabel?: string;
		icon?: Snippet;
		tone?: 'default' | 'ok' | 'warn' | 'err';
		mono?: boolean;
	}

	let { label, value, sublabel, icon, tone = 'default', mono = false }: Props = $props();

	const toneCls = $derived(
		tone === 'ok'
			? 'text-[var(--stw-ok)]'
			: tone === 'warn'
				? 'text-[var(--stw-warn)]'
				: tone === 'err'
					? 'text-[var(--stw-err)]'
					: 'text-[var(--stw-fg)]'
	);
</script>

<div
	class="flex flex-col gap-1 rounded-xl border border-[var(--stw-border)] bg-[var(--stw-bg-panel)] p-4 shadow-[var(--stw-shadow-xs)]"
>
	<div class="flex items-center gap-1.5 text-[var(--stw-fg-mute)]">
		{#if icon}
			<span class="inline-flex items-center justify-center">{@render icon()}</span>
		{/if}
		<span class="text-[12px] font-medium tracking-[0.04em] uppercase">{label}</span>
	</div>
	<div class="{toneCls} text-[24px] leading-tight font-semibold {mono ? 'font-mono' : ''}">
		{value}
	</div>
	{#if sublabel}
		<div class="text-[12px] text-[var(--stw-fg-soft)]">{sublabel}</div>
	{/if}
</div>
