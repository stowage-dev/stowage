<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';
	import Button from '$lib/components/ui/Button.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import PasswordField from '$lib/components/ui/PasswordField.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import ActionBar from '$lib/components/ui/ActionBar.svelte';
	import { api, ApiException } from '$lib/api';
	import { session } from '$lib/stores/session.svelte';
	import type { User } from '$lib/types';

	interface Props {
		user: User;
		onclose: () => void;
		ondone?: () => void;
	}

	let { user, onclose, ondone }: Props = $props();

	const isSelf = $derived(session.me?.id === user.id);

	let password = $state('');
	let reveal = $state(false);
	let busy = $state(false);
	let inputEl: HTMLInputElement | null = $state(null);

	onMount(() => inputEl?.focus());

	function generate() {
		const buf = new Uint8Array(18);
		crypto.getRandomValues(buf);
		let s = '';
		for (const b of buf) s += String.fromCharCode(b);
		password = btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '').slice(0, 18);
		reveal = true;
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy) return;
		busy = true;
		try {
			await api.resetUserPassword(user.id, password);
			toast.success(`Password reset for ${user.username}`);
			ondone?.();
			onclose();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Reset failed.');
		} finally {
			busy = false;
		}
	}
</script>

<Modal title="Reset password" subtitle={user.username} {busy} {onclose} maxWidth="460px">
	<form onsubmit={submit} class="flex flex-col gap-3.5 px-[18px] py-4">
		<FormField label="New password" for="rp-pw" helper="Minimum 12 characters per default policy.">
			<PasswordField
				id="rp-pw"
				bind:value={password}
				bind:reveal
				bind:ref={inputEl}
				required
				minlength={12}
				autocomplete="new-password"
				{generate}
			/>
		</FormField>

		<Banner variant="info">
			{#if isSelf}
				Your other sessions will be signed out. This session stays active and you won't be asked to
				change the password again.
			{:else}
				The user will be required to change this password on next sign-in, and all of their existing
				sessions will be revoked.
			{/if}
		</Banner>

		<ActionBar>
			<Button variant="ghost" onclick={onclose} disabled={busy}>Cancel</Button>
			<Button type="submit" variant="primary" disabled={busy || password.length === 0}>
				{busy ? 'Resetting…' : 'Reset password'}
			</Button>
		</ActionBar>
	</form>
</Modal>
