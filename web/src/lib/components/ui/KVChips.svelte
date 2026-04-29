<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later

	interface Props {
		entries: Record<string, string>;
		maxValueWidth?: string;
	}

	let { entries, maxValueWidth = '240px' }: Props = $props();
</script>

<div class="flex flex-wrap gap-1.5">
	{#each Object.entries(entries) as [k, v] (k)}
		<div
			class="inline-flex max-w-full items-center gap-1 rounded border border-[var(--stw-border)] bg-[var(--stw-bg-sunken)] px-2 py-0.5 font-mono text-[11.5px]"
		>
			<span class="text-[var(--stw-fg-mute)]">{k}</span>
			<span class="text-[var(--stw-fg-soft)]">=</span>
			<span class="truncate text-[var(--stw-fg)]" style="max-width:{maxValueWidth};">{v}</span>
		</div>
	{/each}
</div>
