<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import Button from '$lib/components/ui/Button.svelte';
	import Drawer from '$lib/components/ui/Drawer.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import ActionBar from '$lib/components/ui/ActionBar.svelte';
	import { api, ApiException } from '$lib/api';

	interface Props {
		onclose: () => void;
	}

	let { onclose }: Props = $props();

	let username = $state('');
	let email = $state('');
	let password = $state('');
	let role = $state<'admin' | 'editor' | 'viewer'>('viewer');
	let mustChange = $state(true);
	let busy = $state(false);

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy) return;
		busy = true;
		try {
			await api.createUser({
				username: username.trim(),
				email: email.trim() || undefined,
				password,
				role,
				must_change_pw: mustChange
			});
			toast.success('User created');
			await invalidateAll();
			onclose();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not create user.');
		} finally {
			busy = false;
		}
	}
</script>

<Drawer title="Create user" subtitle="Local account · can sign in immediately" {busy} {onclose}>
	<form onsubmit={submit} class="flex flex-col gap-3.5 p-[18px]">
		<FormField label="Login" for="cu-login">
			<input
				id="cu-login"
				class="stw-input"
				placeholder="e.g. jamie"
				bind:value={username}
				required
				autocomplete="off"
			/>
		</FormField>

		<FormField label="Email" for="cu-email" optional>
			<input id="cu-email" class="stw-input" type="email" bind:value={email} />
		</FormField>

		<FormField label="Role" for="cu-role" helper="Admins can manage users and backends.">
			<select id="cu-role" class="stw-input" bind:value={role}>
				<option value="admin">admin</option>
				<option value="editor">editor</option>
				<option value="viewer">viewer</option>
			</select>
		</FormField>

		<FormField label="Initial password" for="cu-pw" helper="≥ 12 characters per policy.">
			<input
				id="cu-pw"
				class="stw-input"
				type="text"
				bind:value={password}
				required
				minlength={12}
				autocomplete="off"
			/>
		</FormField>

		<label class="inline-flex items-center gap-2 text-[13px]">
			<input type="checkbox" bind:checked={mustChange} />
			Require password change on first sign-in
		</label>

		<ActionBar>
			<Button variant="ghost" onclick={onclose} disabled={busy}>Cancel</Button>
			<Button type="submit" variant="primary" disabled={busy}>
				{busy ? 'Creating…' : 'Create user'}
			</Button>
		</ActionBar>
	</form>
</Drawer>
