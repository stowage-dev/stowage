<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { Share2, Lock, Clock } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Segmented from '$lib/components/ui/Segmented.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import PasswordField from '$lib/components/ui/PasswordField.svelte';
	import CopyField from '$lib/components/ui/CopyField.svelte';
	import { api, ApiException } from '$lib/api';
	import type { BrowserItem, Share } from '$lib/types';

	interface Props {
		item: BrowserItem;
		onclose: () => void;
		backend: string;
		bucket: string;
		prefix: string[];
	}

	let { item, onclose, backend, bucket, prefix }: Props = $props();

	const fullKey = $derived([...prefix, item.key].join('/'));

	type ExpiryPreset = '1h' | '1d' | '7d' | '30d' | 'never' | 'custom';

	let expiryPreset = $state<ExpiryPreset>('7d');
	let customExpiry = $state<string>('');
	let password = $state('');
	let maxDownloads = $state<number | null>(null);
	let disposition = $state<'attachment' | 'inline'>('attachment');
	let advanced = $state(false);

	let submitting = $state(false);
	let result = $state<Share | null>(null);

	const fullURL = $derived(result ? api.shareURL(result) : '');

	const presetMs: Record<ExpiryPreset, number | null> = {
		'1h': 60 * 60 * 1000,
		'1d': 24 * 60 * 60 * 1000,
		'7d': 7 * 24 * 60 * 60 * 1000,
		'30d': 30 * 24 * 60 * 60 * 1000,
		never: null,
		custom: 0
	};

	function expiryISO(): string | undefined {
		if (expiryPreset === 'never') return undefined;
		if (expiryPreset === 'custom') {
			const t = customExpiry.trim();
			if (!t) return undefined;
			return new Date(t).toISOString();
		}
		const ms = presetMs[expiryPreset];
		if (ms == null) return undefined;
		return new Date(Date.now() + ms).toISOString();
	}

	async function submit(): Promise<void> {
		if (submitting) return;
		if (expiryPreset === 'custom' && !customExpiry.trim()) {
			toast.error('Pick a custom expiry date or choose a preset.');
			return;
		}
		submitting = true;
		try {
			const created = await api.createShare({
				backend_id: backend,
				bucket,
				key: fullKey,
				expires_at: expiryISO(),
				password: password || undefined,
				max_downloads: maxDownloads && maxDownloads > 0 ? maxDownloads : undefined,
				disposition
			});
			result = created;
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Failed to create share.');
		} finally {
			submitting = false;
		}
	}

	function shareAnother(): void {
		untrack(() => {
			result = null;
			password = '';
			maxDownloads = null;
			expiryPreset = '7d';
			customExpiry = '';
			disposition = 'attachment';
			advanced = false;
		});
	}
</script>

<Modal
	title={result ? 'Share link ready' : 'Create share link'}
	subtitle="{backend}/{bucket}/{fullKey}"
	subtitleMono
	busy={submitting}
	{onclose}
	maxWidth="480px"
>
	{#snippet icon()}
		<Share2 size={16} strokeWidth={1.7} />
	{/snippet}

	{#if !result}
		<div class="flex flex-col gap-3.5 px-[18px] py-4">
			<div class="flex flex-col gap-1.5">
				<span class="inline-flex items-center gap-1.5 text-[12px] font-medium text-stw-fg-mute">
					<Clock size={12} strokeWidth={1.7} /> Expires
				</span>
				<Segmented
					value={expiryPreset}
					onchange={(v) => (expiryPreset = v)}
					size="sm"
					options={[
						{ value: '1h' as const, label: '1h' },
						{ value: '1d' as const, label: '1 day' },
						{ value: '7d' as const, label: '7 days' },
						{ value: '30d' as const, label: '30 days' },
						{ value: 'never' as const, label: 'Never' },
						{ value: 'custom' as const, label: 'Custom' }
					]}
				/>
				{#if expiryPreset === 'custom'}
					<input type="datetime-local" class="stw-input font-mono" bind:value={customExpiry} />
				{/if}
			</div>

			<div class="flex flex-col gap-1.5">
				<span class="inline-flex items-center gap-1.5 text-[12px] font-medium text-stw-fg-mute">
					<Lock size={12} strokeWidth={1.7} /> Password
					<span class="font-normal text-stw-fg-soft">(optional)</span>
				</span>
				<PasswordField
					bind:value={password}
					placeholder="Leave blank for no password"
					autocomplete="new-password"
					mono={false}
				/>
			</div>

			<FormField label="Download limit" optional>
				<input
					class="stw-input font-mono"
					type="number"
					min="1"
					placeholder="Unlimited"
					bind:value={maxDownloads}
				/>
			</FormField>

			<button
				type="button"
				onclick={() => (advanced = !advanced)}
				class="cursor-pointer self-start border-0 bg-transparent p-0 text-[11.5px] text-stw-fg-soft focus-ring hover:text-stw-fg"
			>
				{advanced ? '− Hide advanced' : '+ Advanced'}
			</button>

			{#if advanced}
				<FormField label="Browser behaviour">
					<Segmented
						value={disposition}
						onchange={(v) => (disposition = v)}
						size="sm"
						options={[
							{ value: 'attachment' as const, label: 'Force download' },
							{ value: 'inline' as const, label: 'Show in browser' }
						]}
					/>
				</FormField>
			{/if}
		</div>
	{:else}
		<div class="flex flex-col gap-3 px-[18px] py-4">
			<CopyField value={fullURL} ariaLabel="Copy link" />

			<div class="flex flex-col gap-1 text-[12px] text-stw-fg-mute">
				{#if result.expires_at}
					<div>
						Expires
						<span class="text-stw-fg">{new Date(result.expires_at).toLocaleString()}</span>
					</div>
				{:else}
					<div>No expiry — revoke manually when done.</div>
				{/if}
				{#if result.has_password}
					<div>Password-protected. Recipient sees a password form before download.</div>
				{/if}
				{#if result.max_downloads}
					<div>
						Limited to
						<span class="text-stw-fg">{result.max_downloads}</span>
						download{result.max_downloads === 1 ? '' : 's'}.
					</div>
				{/if}
				{#if result.disposition === 'inline'}
					<div>Opens in the recipient's browser instead of downloading.</div>
				{/if}
			</div>
		</div>
	{/if}

	{#snippet footer()}
		{#if !result}
			<Button variant="ghost" onclick={onclose} disabled={submitting}>Cancel</Button>
			<Button variant="primary" onclick={submit} disabled={submitting}>
				{submitting ? 'Creating…' : 'Create link'}
			</Button>
		{:else}
			<Button variant="ghost" onclick={shareAnother}>Create another</Button>
			<Button variant="primary" onclick={onclose}>Done</Button>
		{/if}
	{/snippet}
</Modal>
