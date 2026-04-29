<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { toast } from 'svelte-sonner';
	import { CheckCircle2, XCircle } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Drawer from '$lib/components/ui/Drawer.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import ActionBar from '$lib/components/ui/ActionBar.svelte';
	import { api, ApiException } from '$lib/api';
	import { messageFor } from '$lib/i18n';
	import type {
		CreateEndpointRequest,
		Endpoint,
		PatchEndpointRequest,
		TestEndpointResult
	} from '$lib/types';

	interface Props {
		mode: 'create' | 'edit';
		endpoint?: Endpoint;
		onclose: () => void;
		onsaved: () => void | Promise<void>;
	}

	let { mode, endpoint, onclose, onsaved }: Props = $props();

	const initialEndpoint = endpoint;
	const editing = mode === 'edit' && !!initialEndpoint;

	let id = $state(initialEndpoint?.id ?? '');
	let name = $state(initialEndpoint?.name ?? '');
	let endpointURL = $state(initialEndpoint?.endpoint ?? '');
	let region = $state(initialEndpoint?.region ?? '');
	let pathStyle = $state(initialEndpoint?.path_style ?? true);
	let accessKey = $state(initialEndpoint?.access_key ?? '');
	let secretKey = $state('');
	let enabled = $state(initialEndpoint?.enabled ?? true);
	let replaceSecret = $state(!editing);

	let busy = $state(false);
	let testResult = $state<TestEndpointResult | null>(null);
	let testing = $state(false);

	async function runTest() {
		if (!accessKey || (!secretKey && !editing)) {
			toast.error('Access key and secret key are required to test.');
			return;
		}
		if (editing && !replaceSecret) {
			toast.error('Toggle "Replace secret" first; tests use what you typed.');
			return;
		}
		testing = true;
		testResult = null;
		try {
			testResult = await api.testEndpoint({
				type: 's3v4',
				endpoint: endpointURL,
				region,
				path_style: pathStyle,
				access_key: accessKey,
				secret_key: secretKey
			});
		} catch (err) {
			toast.error(err instanceof ApiException ? messageFor(err.code, err.message) : 'Test failed.');
		} finally {
			testing = false;
		}
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy) return;
		busy = true;
		try {
			if (editing && initialEndpoint) {
				const patch: PatchEndpointRequest = {
					name: name === initialEndpoint.name ? undefined : name,
					endpoint: endpointURL === initialEndpoint.endpoint ? undefined : endpointURL,
					region: region === initialEndpoint.region ? undefined : region,
					path_style: pathStyle === initialEndpoint.path_style ? undefined : pathStyle,
					access_key: accessKey === (initialEndpoint.access_key ?? '') ? undefined : accessKey,
					enabled: enabled === initialEndpoint.enabled ? undefined : enabled
				};
				if (replaceSecret) patch.secret_key = secretKey;
				await api.patchEndpoint(initialEndpoint.id, patch);
				toast.success(`Updated ${initialEndpoint.id}`);
			} else {
				const req: CreateEndpointRequest = {
					id: id.trim().toLowerCase(),
					name: name || undefined,
					endpoint: endpointURL,
					region,
					path_style: pathStyle,
					access_key: accessKey,
					secret_key: secretKey,
					enabled
				};
				await api.createEndpoint(req);
				toast.success(`Created ${req.id}`);
			}
			await onsaved();
		} catch (err) {
			toast.error(err instanceof ApiException ? messageFor(err.code, err.message) : 'Save failed.');
		} finally {
			busy = false;
		}
	}
</script>

<Drawer
	title={editing ? `Edit ${initialEndpoint?.id}` : 'Add endpoint'}
	subtitle="S3-compatible backend (AWS, MinIO, Garage, R2, …)"
	maxWidth="480px"
	{onclose}
>
	<form onsubmit={submit} class="flex flex-col gap-3.5 p-[18px]">
		{#if !editing}
			<FormField
				label="ID"
				for="ep-id"
				helper={`Stable slug (lowercase, digits, "-" or "_"). ` +
					'Used in URLs and audit logs; cannot change later.'}
			>
				<input
					id="ep-id"
					class="stw-input"
					placeholder="e.g. minio-prod"
					bind:value={id}
					required
					pattern="^[a-z0-9][a-z0-9_-]{'{0,63}'}$"
					autocomplete="off"
				/>
			</FormField>
		{/if}

		<FormField label="Display name" for="ep-name">
			<input id="ep-name" class="stw-input" placeholder="MinIO production" bind:value={name} />
		</FormField>

		<FormField label="Endpoint URL" for="ep-url">
			<input
				id="ep-url"
				class="stw-input"
				type="url"
				placeholder="https://s3.example.com:9000"
				bind:value={endpointURL}
				required
			/>
			{#snippet hint()}{/snippet}
		</FormField>
		<div class="-mt-2 text-[11.5px] text-[var(--stw-fg-soft)]">
			Append <code>:PORT</code> for nonstandard ports (e.g. <code>:9000</code> for MinIO,
			<code>:3900</code> for Garage). No path, query, or fragment.
		</div>

		<div class="flex gap-3">
			<div class="flex-1">
				<FormField label="Region" for="ep-region">
					<input id="ep-region" class="stw-input" placeholder="us-east-1" bind:value={region} />
				</FormField>
			</div>
			<label class="inline-flex h-[30px] items-center gap-2 self-end text-[13px]">
				<input type="checkbox" bind:checked={pathStyle} />
				Path-style addressing
			</label>
		</div>

		<FormField label="Access key" for="ep-ak">
			<input id="ep-ak" class="stw-input" bind:value={accessKey} required autocomplete="off" />
		</FormField>

		<FormField label="Secret key" for="ep-sk">
			{#snippet hint()}
				{#if editing}
					<span>· stored secret kept unless replaced</span>
				{/if}
			{/snippet}
			{#if editing}
				<label class="inline-flex items-center gap-2 text-[13px] text-[var(--stw-fg-mute)]">
					<input type="checkbox" bind:checked={replaceSecret} />
					Replace secret
				</label>
			{/if}
			<input
				id="ep-sk"
				class="stw-input"
				type="password"
				bind:value={secretKey}
				required={!editing || replaceSecret}
				disabled={editing && !replaceSecret}
				autocomplete="off"
			/>
		</FormField>

		<label class="inline-flex items-center gap-2 text-[13px]">
			<input type="checkbox" bind:checked={enabled} />
			Enabled (registered as live in the proxy)
		</label>

		<div class="flex items-center gap-3">
			<Button variant="ghost" disabled={testing} onclick={runTest}>
				{testing ? 'Testing…' : 'Test connection'}
			</Button>
			{#if testResult}
				{#if testResult.healthy}
					<span class="inline-flex items-center gap-1.5 text-[12.5px] text-[var(--stw-ok)]">
						<CheckCircle2 size={14} strokeWidth={1.8} />
						Reachable · {testResult.latency_ms} ms
					</span>
				{:else}
					<span class="inline-flex items-center gap-1.5 text-[12.5px] text-[var(--stw-err)]">
						<XCircle size={14} strokeWidth={1.8} />
						{testResult.error ?? 'Unreachable'}
					</span>
				{/if}
			{/if}
		</div>

		<ActionBar>
			<Button variant="ghost" onclick={onclose}>Cancel</Button>
			<Button type="submit" variant="primary" disabled={busy}>
				{busy ? (editing ? 'Saving…' : 'Creating…') : editing ? 'Save' : 'Add endpoint'}
			</Button>
		</ActionBar>
	</form>
</Drawer>
