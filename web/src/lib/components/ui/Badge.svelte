<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	type Variant = 'ok' | 'warn' | 'err' | undefined;

	interface Props {
		variant?: Variant;
		children?: Snippet;
		mono?: boolean;
		style?: string;
	}

	let { variant, children, mono = false, style = '' }: Props = $props();

	const cls = $derived('stw-badge' + (variant ? ' stw-badge--' + variant : ''));
	const styleStr = $derived((mono ? 'font-family:var(--stw-font-mono);' : '') + style);
</script>

<span class={cls} style={styleStr}>
	{#if children}{@render children()}{/if}
</span>
