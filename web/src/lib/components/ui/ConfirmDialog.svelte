<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount } from 'svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import type { Snippet } from 'svelte';

	type Variant = 'default' | 'danger';

	interface Props {
		title: string;
		description?: string;
		body?: Snippet;
		confirmLabel?: string;
		cancelLabel?: string;
		variant?: Variant;
		busy?: boolean;
		onconfirm: () => void | Promise<void>;
		oncancel: () => void;
	}

	let {
		title,
		description,
		body,
		confirmLabel = 'Confirm',
		cancelLabel = 'Cancel',
		variant = 'default',
		busy = false,
		onconfirm,
		oncancel
	}: Props = $props();

	onMount(() => {
		const onKey = (e: KeyboardEvent): void => {
			if (busy) return;
			if (e.key === 'Enter' && (e.target as HTMLElement)?.tagName !== 'TEXTAREA') {
				e.preventDefault();
				void onconfirm();
			}
		};
		window.addEventListener('keydown', onKey);
		return () => window.removeEventListener('keydown', onKey);
	});
</script>

<Modal {title} {busy} onclose={oncancel} maxWidth="440px">
	<div class="px-[18px] py-4 text-[13px] leading-[1.5] text-stw-fg-mute">
		{#if body}
			{@render body()}
		{:else if description}
			{description}
		{/if}
	</div>

	{#snippet footer()}
		<Button variant="ghost" onclick={oncancel} disabled={busy}>
			{cancelLabel}
		</Button>
		<Button
			variant={variant === 'danger' ? 'danger' : 'primary'}
			onclick={() => void onconfirm()}
			disabled={busy}
		>
			{busy ? 'Working…' : confirmLabel}
		</Button>
	{/snippet}
</Modal>
