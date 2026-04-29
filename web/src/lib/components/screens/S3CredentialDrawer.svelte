<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { toast } from 'svelte-sonner';
	import { AlertTriangle, Check, Copy } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Drawer from '$lib/components/ui/Drawer.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import ActionBar from '$lib/components/ui/ActionBar.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import { api, ApiException } from '$lib/api';
	import type { Backend, S3Credential } from '$lib/types';

	interface Props {
		mode: 'admin' | 'me';
		backends: Backend[];
		oncreated: (cred: S3Credential) => void;
		onclose: () => void;
	}

	let { mode, backends, oncreated, onclose }: Props = $props();

	let backendId = $state(backends[0]?.id ?? '');
	let bucketsRaw = $state('');
	let description = $state('');
	let expiresMode = $state<'never' | '7d' | '30d' | 'custom'>('never');
	let expiresCustom = $state('');
	let busy = $state(false);
	let created = $state<S3Credential | null>(null);
	let copiedField = $state<'key' | 'secret' | null>(null);

	function copy(text: string, which: 'key' | 'secret'): void {
		navigator.clipboard?.writeText(text);
		copiedField = which;
		setTimeout(() => {
			if (copiedField === which) copiedField = null;
		}, 1400);
	}

	function expiresAt(): string | undefined {
		switch (expiresMode) {
			case 'never':
				return undefined;
			case '7d':
				return new Date(Date.now() + 7 * 86400_000).toISOString();
			case '30d':
				return new Date(Date.now() + 30 * 86400_000).toISOString();
			case 'custom':
				return expiresCustom ? new Date(expiresCustom).toISOString() : undefined;
		}
	}

	async function submit(e: SubmitEvent): Promise<void> {
		e.preventDefault();
		if (busy) return;
		const buckets = bucketsRaw
			.split(/[\s,]+/)
			.map((s) => s.trim())
			.filter(Boolean);
		if (!backendId || buckets.length === 0) {
			toast.error('Pick a backend and at least one bucket.');
			return;
		}
		busy = true;
		try {
			const req = {
				backend_id: backendId,
				buckets,
				description: description.trim() || undefined,
				expires_at: expiresAt()
			};
			const result =
				mode === 'admin'
					? await api.createAdminS3Credential(req)
					: await api.createMyS3Credential(req);
			created = result;
			oncreated(result);
			toast.success('Credential created');
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not create credential.');
		} finally {
			busy = false;
		}
	}
</script>

<Drawer
	title={created ? 'Save these credentials' : 'New S3 credential'}
	subtitle={mode === 'admin' ? 'UI-managed virtual access key' : 'Personal virtual access key'}
	{busy}
	{onclose}
>
	{#if created}
		<div class="flex flex-col gap-4 p-[18px]">
			<Banner variant="warn">
				<AlertTriangle size={14} strokeWidth={1.7} />
				The secret key is shown <strong>once</strong>. Copy it now — there is no way to retrieve it
				later. If you lose it, delete the credential and mint a new one.
			</Banner>

			<FormField label="Access key">
				<div class="flex items-center gap-2">
					<input class="stw-input" readonly value={created.access_key} />
					<button
						type="button"
						class="stw-icon-btn"
						aria-label="Copy access key"
						onclick={() => copy(created!.access_key, 'key')}
					>
						{#if copiedField === 'key'}
							<Check size={14} strokeWidth={1.7} />
						{:else}
							<Copy size={14} strokeWidth={1.7} />
						{/if}
					</button>
				</div>
			</FormField>

			<FormField label="Secret key">
				<div class="flex items-center gap-2">
					<input class="stw-input" readonly value={created.secret_key ?? ''} />
					<button
						type="button"
						class="stw-icon-btn"
						aria-label="Copy secret key"
						onclick={() => copy(created!.secret_key ?? '', 'secret')}
					>
						{#if copiedField === 'secret'}
							<Check size={14} strokeWidth={1.7} />
						{:else}
							<Copy size={14} strokeWidth={1.7} />
						{/if}
					</button>
				</div>
			</FormField>

			<FormField label="Scope">
				<div class="text-[12.5px] text-[var(--stw-fg-mute)]">
					{created.backend_id} · {created.buckets.join(', ')}
				</div>
			</FormField>

			<ActionBar>
				<Button variant="primary" onclick={onclose}>Done</Button>
			</ActionBar>
		</div>
	{:else}
		<form onsubmit={submit} class="flex flex-col gap-3.5 p-[18px]">
			<FormField label="Backend" for="cred-backend">
				<select id="cred-backend" class="stw-input" bind:value={backendId} required>
					{#each backends as b (b.id)}
						<option value={b.id}>{b.name}</option>
					{/each}
				</select>
			</FormField>

			<FormField
				label="Buckets"
				for="cred-buckets"
				helper="One or more bucket names. Separate with spaces or commas."
			>
				<input
					id="cred-buckets"
					class="stw-input"
					placeholder="reports, archive"
					bind:value={bucketsRaw}
					required
					autocomplete="off"
				/>
			</FormField>

			<FormField label="Description" for="cred-desc" optional>
				<input
					id="cred-desc"
					class="stw-input"
					placeholder="e.g. nightly-backup-job"
					bind:value={description}
					autocomplete="off"
				/>
			</FormField>

			<FormField label="Expires" for="cred-expires">
				<select id="cred-expires" class="stw-input" bind:value={expiresMode}>
					<option value="never">Never expires</option>
					<option value="7d">In 7 days</option>
					<option value="30d">In 30 days</option>
					<option value="custom">Pick a date…</option>
				</select>
			</FormField>

			{#if expiresMode === 'custom'}
				<FormField label="Expiry date" for="cred-expires-custom">
					<input
						id="cred-expires-custom"
						class="stw-input"
						type="datetime-local"
						bind:value={expiresCustom}
					/>
				</FormField>
			{/if}

			<ActionBar>
				<Button variant="ghost" onclick={onclose} disabled={busy}>Cancel</Button>
				<Button type="submit" variant="primary" disabled={busy}>
					{busy ? 'Creating…' : 'Create credential'}
				</Button>
			</ActionBar>
		</form>
	{/if}
</Drawer>
