<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Props {
		label: string;
		icon: Snippet;
		onclick?: (e: MouseEvent) => void;
		active?: boolean;
		title?: string;
		size?: number;
	}

	let { label, icon, onclick, active = false, title, size = 28 }: Props = $props();

	let hovered = $state(false);

	const bg = $derived(active || hovered ? 'var(--stw-bg-hover)' : 'transparent');
	const color = $derived(active || hovered ? 'var(--stw-fg)' : 'var(--stw-fg-mute)');
	const border = $derived(active ? 'var(--stw-border)' : 'transparent');
</script>

<button
	type="button"
	{onclick}
	title={title || label}
	aria-label={label}
	class="stw-focus"
	style="display:inline-flex;align-items:center;justify-content:center;width:{size}px;height:{size}px;padding:0;background:{bg};border:1px solid {border};border-radius:6px;color:{color};cursor:pointer;transition:background 120ms,color 120ms;"
	onmouseenter={() => (hovered = true)}
	onmouseleave={() => (hovered = false)}
>
	{@render icon()}
</button>
