<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later

	interface Item {
		label: string;
		value?: string | number;
		tone?: 'default' | 'ok' | 'warn' | 'err' | 'mute';
	}

	interface Props {
		items: Item[];
		separator?: string;
		mono?: boolean;
	}

	let { items, separator = '·', mono = false }: Props = $props();

	function toneCls(tone: Item['tone']): string {
		switch (tone) {
			case 'ok':
				return 'text-[var(--stw-ok)]';
			case 'warn':
				return 'text-[var(--stw-warn)]';
			case 'err':
				return 'text-[var(--stw-err)]';
			case 'mute':
				return 'text-[var(--stw-fg-soft)]';
			default:
				return 'text-[var(--stw-fg-mute)]';
		}
	}
</script>

<div
	class="flex flex-wrap items-center gap-2 text-[12px] text-[var(--stw-fg-mute)] {mono
		? 'font-mono'
		: ''}"
>
	{#each items as item, i (item.label + i)}
		{#if i > 0}
			<span class="text-[var(--stw-fg-soft)]" aria-hidden="true">{separator}</span>
		{/if}
		<span class={toneCls(item.tone)}>
			{#if item.value !== undefined && item.value !== ''}
				<span class="font-medium">{item.value}</span>
				<span class="ml-1 text-[var(--stw-fg-soft)]">{item.label}</span>
			{:else}
				{item.label}
			{/if}
		</span>
	{/each}
</div>
