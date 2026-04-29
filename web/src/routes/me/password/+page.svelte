<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { toast } from 'svelte-sonner';
	import Button from '$lib/components/ui/Button.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import SectionCard from '$lib/components/ui/SectionCard.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import ActionBar from '$lib/components/ui/ActionBar.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import { api, ApiException } from '$lib/api';
	import { session } from '$lib/stores/session.svelte';

	const must = $derived(page.url.searchParams.get('must') === '1');

	let current = $state('');
	let next = $state('');
	let confirmPw = $state('');
	let submitting = $state(false);

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (submitting) return;
		if (next !== confirmPw) {
			toast.error('Passwords do not match.');
			return;
		}
		submitting = true;
		try {
			await api.changeOwnPassword(current, next);
			toast.success('Password changed.');
			goto('/');
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not change password.');
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head>
	<title>Change password · stowage</title>
</svelte:head>

<div class="stw-page-pad max-w-[520px]">
	<PageHeader
		title="Change password"
		subtitle={session.me ? `Signed in as ${session.me.username}.` : undefined}
	/>

	{#if must}
		<div class="mb-4">
			<Banner variant="warn" role="alert">You must change your password before continuing.</Banner>
		</div>
	{/if}

	<SectionCard>
		<form onsubmit={submit} class="flex flex-col gap-3.5">
			<FormField label="Current password" for="cur-pw">
				<input
					id="cur-pw"
					class="stw-input"
					type="password"
					autocomplete="current-password"
					bind:value={current}
					required
				/>
			</FormField>
			<FormField
				label="New password"
				for="new-pw"
				helper="Minimum 12 characters per default policy."
			>
				<input
					id="new-pw"
					class="stw-input"
					type="password"
					autocomplete="new-password"
					bind:value={next}
					required
					minlength="12"
				/>
			</FormField>
			<FormField label="Confirm new password" for="confirm-pw">
				<input
					id="confirm-pw"
					class="stw-input"
					type="password"
					autocomplete="new-password"
					bind:value={confirmPw}
					required
					minlength="12"
				/>
			</FormField>
			<ActionBar>
				<Button type="submit" variant="primary" disabled={submitting}>
					{submitting ? 'Saving…' : 'Update password'}
				</Button>
			</ActionBar>
		</form>
	</SectionCard>
</div>
