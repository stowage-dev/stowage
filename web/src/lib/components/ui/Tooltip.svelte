<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Props {
		text: string;
		children: Snippet;
	}

	let { text, children }: Props = $props();

	let show = $state(false);
	let triggerEl: HTMLElement | undefined = $state();
	let tooltipEl: HTMLElement | undefined = $state();
	let top = $state(0);
	let left = $state(0);
	let measured = $state(false);

	$effect(() => {
		if (!show) {
			measured = false;
			return;
		}
		if (!triggerEl || !tooltipEl) return;
		const r = triggerEl.getBoundingClientRect();
		const th = tooltipEl.offsetHeight;
		const tw = tooltipEl.offsetWidth;
		const gap = 6;
		const margin = 4;
		const flip = r.top - th - gap < margin;
		top = flip ? r.bottom + gap : r.top - th - gap;
		let l = r.left + r.width / 2 - tw / 2;
		const max = window.innerWidth - tw - margin;
		if (l < margin) l = margin;
		else if (l > max) l = max;
		left = l;
		measured = true;
	});
</script>

<span
	bind:this={triggerEl}
	style="display:inline-flex;"
	onmouseenter={() => (show = true)}
	onmouseleave={() => (show = false)}
	role="presentation"
>
	{@render children()}
</span>
{#if show}
	<span
		bind:this={tooltipEl}
		role="tooltip"
		style="position:fixed;top:{top}px;left:{left}px;padding:5px 8px;white-space:nowrap;background:var(--stw-n-800);color:var(--stw-n-0);font-size:11px;border-radius:5px;box-shadow:var(--stw-shadow-md);pointer-events:none;z-index:1000;opacity:{measured
			? 1
			: 0};"
	>
		{text}
	</span>
{/if}
