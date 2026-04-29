<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import Modal from '$lib/components/ui/Modal.svelte';
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		subtitle?: string;
		maxWidth?: string;
		busy?: boolean;
		closeOnBackdrop?: boolean;
		closeOnEscape?: boolean;
		showClose?: boolean;
		header?: Snippet;
		children?: Snippet;
		footer?: Snippet;
		onclose: () => void;
	}

	let {
		title,
		subtitle,
		maxWidth,
		busy = false,
		closeOnBackdrop = true,
		closeOnEscape = true,
		showClose = true,
		header,
		children,
		footer,
		onclose
	}: Props = $props();
</script>

<Modal
	{title}
	{subtitle}
	variant="drawer-right"
	{maxWidth}
	{busy}
	{closeOnBackdrop}
	{closeOnEscape}
	{showClose}
	{header}
	{footer}
	{onclose}
>
	{#if children}{@render children()}{/if}
</Modal>
