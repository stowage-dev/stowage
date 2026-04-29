<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { toast } from 'svelte-sonner';
	import Button from '$lib/components/ui/Button.svelte';
	import Drawer from '$lib/components/ui/Drawer.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import ActionBar from '$lib/components/ui/ActionBar.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import { api, ApiException } from '$lib/api';
	import type { Backend } from '$lib/types';

	interface Props {
		backends: Backend[];
		oncreated: () => void;
		onclose: () => void;
	}

	let { backends, oncreated, onclose }: Props = $props();

	let backendId = $state(backends[0]?.id ?? '');
	let bucket = $state('');
	let rps = $state(20);
	let busy = $state(false);

	async function submit(e: SubmitEvent): Promise<void> {
		e.preventDefault();
		if (busy) return;
		if (!backendId || !bucket.trim()) {
			toast.error('Pick a backend and a bucket.');
			return;
		}
		busy = true;
		try {
			await api.upsertAdminS3Anonymous({
				backend_id: backendId,
				bucket: bucket.trim(),
				mode: 'ReadOnly',
				per_source_ip_rps: rps
			});
			toast.success('Anonymous binding saved');
			oncreated();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not save.');
		} finally {
			busy = false;
		}
	}
</script>

<Drawer
	title="New anonymous binding"
	subtitle="Public read-only access via the proxy"
	{busy}
	{onclose}
>
	<form onsubmit={submit} class="flex flex-col gap-3.5 p-[18px]">
		<Banner variant="warn">
			Anyone on the network can read every object in this bucket without authentication. Confirm the
			cluster-wide kill switch <code>s3_proxy.anonymous_enabled</code> is on before relying on this.
		</Banner>

		<FormField label="Backend" for="anon-backend">
			<select id="anon-backend" class="stw-input" bind:value={backendId} required>
				{#each backends as b (b.id)}
					<option value={b.id}>{b.name}</option>
				{/each}
			</select>
		</FormField>

		<FormField label="Bucket" for="anon-bucket">
			<input
				id="anon-bucket"
				class="stw-input"
				placeholder="downloads-public"
				bind:value={bucket}
				required
				autocomplete="off"
			/>
		</FormField>

		<FormField
			label="Per-source IP rate limit"
			for="anon-rps"
			helper="Requests per second per client IP. 0 disables the per-IP cap."
		>
			<input id="anon-rps" class="stw-input" type="number" min="0" bind:value={rps} required />
		</FormField>

		<ActionBar>
			<Button variant="ghost" onclick={onclose} disabled={busy}>Cancel</Button>
			<Button type="submit" variant="primary" disabled={busy}>
				{busy ? 'Saving…' : 'Save binding'}
			</Button>
		</ActionBar>
	</form>
</Drawer>
