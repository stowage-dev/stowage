<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';
	import { Check, Copy } from 'lucide-svelte';

	interface Props {
		value: string;
		children?: Snippet;
		mono?: boolean;
	}

	let { value, children, mono = true }: Props = $props();

	let copied = $state(false);

	function copy(e: MouseEvent): void {
		e.stopPropagation();
		navigator.clipboard?.writeText(value);
		copied = true;
		setTimeout(() => (copied = false), 1400);
	}
</script>

<button
	type="button"
	onclick={copy}
	class="inline-flex h-[22px] cursor-pointer items-center gap-1 rounded-stw-sm border border-stw-border bg-transparent px-1.5 text-[11.5px] text-stw-fg-mute focus-ring transition-colors duration-[120ms] hover:bg-stw-bg-hover hover:text-stw-fg {mono
		? 'font-stw-mono'
		: ''}"
>
	{#if children}{@render children()}{:else}{value}{/if}
	<span class="inline-flex {copied ? 'text-stw-ok' : 'text-stw-fg-soft'}">
		{#if copied}<Check size={12} />{:else}<Copy size={12} />{/if}
	</span>
</button>
