<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	type Variant = 'ok' | 'warn' | 'err' | undefined;

	interface Props {
		variant?: Variant;
		children?: Snippet;
		mono?: boolean;
		class?: string;
	}

	let { variant, children, mono = false, class: klass = '' }: Props = $props();

	const cls = $derived(
		`stw-badge ${variant ? 'stw-badge--' + variant : ''} ${mono ? 'font-stw-mono' : ''} ${klass}`
	);
</script>

<span class={cls}>
	{#if children}{@render children()}{/if}
</span>
