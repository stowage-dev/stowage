<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		sub?: string | null;
		showBack?: boolean;
		backHref?: string;
		children: Snippet;
		footer?: Snippet;
	}

	let {
		title,
		sub = null,
		showBack = false,
		backHref = '/login',
		children,
		footer
	}: Props = $props();
</script>

<div class="lg-card">
	{#if showBack}
		<a class="lg-back-btn" href={backHref} aria-label="Back to sign in">
			<svg
				width="14"
				height="14"
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="2"
				stroke-linecap="round"
				stroke-linejoin="round"
			>
				<path d="M15 18l-6-6 6-6" />
			</svg>
			Back
		</a>
	{/if}

	<div class="lg-brand-row">
		<div class="lg-brand-mark">s</div>
		<div class="lg-brand-name">stowage</div>
		<div class="lg-brand-meta"><span class="lg-brand-meta-dot"></span>self-hosted</div>
	</div>

	<h1 class="lg-title">{title}</h1>
	{#if sub}<p class="lg-sub">{sub}</p>{/if}

	{@render children()}

	{#if footer}{@render footer()}{/if}
</div>
