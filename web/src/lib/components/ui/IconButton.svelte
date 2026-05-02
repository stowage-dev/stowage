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

	const base =
		'focus-ring inline-flex cursor-pointer items-center justify-center rounded-stw-md p-0 ' +
		'border transition-[background,color] duration-[120ms]';
	const stateCls = $derived(
		active
			? 'border-stw-border bg-stw-bg-hover text-stw-fg'
			: 'border-transparent bg-transparent text-stw-fg-mute hover:border-transparent hover:bg-stw-bg-hover hover:text-stw-fg'
	);
</script>

<button
	type="button"
	{onclick}
	title={title || label}
	aria-label={label}
	class="{base} {stateCls}"
	style="width:{size}px;height:{size}px;"
>
	{@render icon()}
</button>
