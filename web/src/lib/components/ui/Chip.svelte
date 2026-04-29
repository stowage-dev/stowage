<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Props {
		pressed?: boolean;
		onclick?: (e: MouseEvent) => void;
		children?: Snippet;
		icon?: Snippet;
	}

	let { pressed = false, onclick, children, icon }: Props = $props();
</script>

<button
	type="button"
	class="stw-chip stw-focus"
	aria-pressed={pressed ? 'true' : 'false'}
	{onclick}
>
	{#if icon}{@render icon()}{/if}
	{#if children}{@render children()}{/if}
</button>
