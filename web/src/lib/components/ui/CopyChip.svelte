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
	let hovered = $state(false);

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
	class="stw-focus"
	onmouseenter={() => (hovered = true)}
	onmouseleave={() => (hovered = false)}
	style="display:inline-flex;align-items:center;gap:4px;height:22px;padding:0 6px;font-family:{mono
		? 'var(--stw-font-mono)'
		: 'inherit'};font-size:11.5px;color:{hovered
		? 'var(--stw-fg)'
		: 'var(--stw-fg-mute)'};background:{hovered
		? 'var(--stw-bg-hover)'
		: 'transparent'};border:1px solid var(--stw-border);border-radius:4px;cursor:pointer;transition:background 120ms,color 120ms;"
>
	{#if children}{@render children()}{:else}{value}{/if}
	<span style="color:{copied ? 'var(--stw-ok)' : 'var(--stw-fg-soft)'};display:inline-flex;">
		{#if copied}<Check size={12} />{:else}<Copy size={12} />{/if}
	</span>
</button>
