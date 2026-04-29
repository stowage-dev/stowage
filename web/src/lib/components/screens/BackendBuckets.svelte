<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { goto, invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { Plus, Folder, Trash2 } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import Tooltip from '$lib/components/ui/Tooltip.svelte';
	import ConfirmDialog from '$lib/components/ui/ConfirmDialog.svelte';
	import BackendMark from '$lib/components/ui/BackendMark.svelte';
	import Chevron from '$lib/components/ui/Chevron.svelte';
	import Dot from '$lib/components/ui/Dot.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import StatLine from '$lib/components/ui/StatLine.svelte';
	import DataTable from '$lib/components/ui/DataTable.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import { urlForRoute } from '$lib/route';
	import { inferKind, backendHealth } from '$lib/backend-kind';
	import { api, ApiException } from '$lib/api';
	import { bytes as fmtBytes } from '$lib/format';
	import { session } from '$lib/stores/session.svelte';
	import { refreshBuckets, type BucketState } from '$lib/stores/buckets.svelte';
	import type { Backend } from '$lib/types';

	interface Props {
		backend: Backend;
		bucketsResult: BucketState;
	}

	let { backend: b, bucketsResult }: Props = $props();

	const kind = $derived(inferKind(b));
	const health = $derived(backendHealth(b));
	const isAdmin = $derived(session.me?.role === 'admin');
	const list = $derived(bucketsResult.status === 'ok' ? bucketsResult.buckets : []);
	const err = $derived(bucketsResult.status === 'error' ? bucketsResult.message : null);
	const loading = $derived(bucketsResult.status === 'loading');

	let creating = $state(false);
	let newName = $state('');
	let busy = $state(false);
	let confirmDeleteBucket = $state<{ name: string; busy: boolean } | null>(null);

	async function create(e: SubmitEvent) {
		e.preventDefault();
		if (busy) return;
		busy = true;
		try {
			await api.createBucket(b.id, newName.trim());
			toast.success(`Bucket ${newName} created`);
			creating = false;
			newName = '';
			refreshBuckets(b.id);
			await invalidateAll();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not create bucket.');
		} finally {
			busy = false;
		}
	}

	function destroy(name: string, e: MouseEvent) {
		e.stopPropagation();
		confirmDeleteBucket = { name, busy: false };
	}

	async function runDeleteBucket(): Promise<void> {
		if (!confirmDeleteBucket) return;
		const name = confirmDeleteBucket.name;
		confirmDeleteBucket = { ...confirmDeleteBucket, busy: true };
		try {
			await api.deleteBucket(b.id, name);
			toast.success(`Bucket ${name} deleted`);
			refreshBuckets(b.id);
			await invalidateAll();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not delete bucket.');
		} finally {
			confirmDeleteBucket = null;
		}
	}

	const columns = [
		{ key: 'name', label: 'Name' },
		{ key: 'size', label: 'Size' },
		{ key: 'created', label: 'Created' },
		{ key: 'actions', label: '', align: 'right' as const }
	];
</script>

<div class="stw-page-pad">
	<PageHeader title={b.name}>
		{#snippet icon()}
			<BackendMark {kind} size={22} />
		{/snippet}
		{#snippet meta()}
			<Dot variant={health.state} label={health.state} />
			<StatLine
				items={[
					{ label: 'buckets', value: list.length },
					{ label: b.id },
					...(b.last_probe_at
						? [{ label: `last probe ${new Date(b.last_probe_at).toLocaleTimeString()}` }]
						: [])
				]}
			/>
		{/snippet}
		{#snippet actions()}
			{#if isAdmin}
				{#snippet plusIcon()}<Plus size={13} strokeWidth={1.7} />{/snippet}
				<Button variant="primary" icon={plusIcon} onclick={() => (creating = true)}>
					Create bucket
				</Button>
			{/if}
		{/snippet}
	</PageHeader>

	{#if creating}
		<form
			onsubmit={create}
			class="mb-4 flex items-center gap-2.5 rounded-lg border border-[var(--stw-border)] bg-[var(--stw-bg-panel)] p-3.5"
		>
			<input
				class="stw-input flex-1 font-mono"
				placeholder="bucket-name"
				bind:value={newName}
				required
				minlength="3"
				maxlength="63"
				pattern="[a-z0-9][a-z0-9.\-]+[a-z0-9]"
			/>
			<Button type="submit" variant="primary" disabled={busy}>
				{busy ? 'Creating…' : 'Create'}
			</Button>
			<Button
				variant="ghost"
				onclick={() => {
					creating = false;
					newName = '';
				}}
			>
				Cancel
			</Button>
		</form>
	{/if}

	<DataTable {columns} rows={list} emptyText={loading ? 'Loading…' : 'No buckets yet.'}>
		{#snippet row(bk)}
			<td
				class="cursor-pointer px-3 hover:bg-[var(--stw-bg-hover)]"
				onclick={() =>
					goto(urlForRoute({ type: 'bucket', backend: b.id, bucket: bk.name, prefix: [] }))}
			>
				<span class="inline-flex items-center gap-2">
					<Folder size={14} strokeWidth={1.7} />
					<span class="font-medium">{bk.name}</span>
				</span>
			</td>
			<td class="px-3 font-mono text-[12px] text-[var(--stw-fg-mute)]">
				{#if !bk.size_tracked}
					<span title="Size tracking is off for this bucket">—</span>
				{:else if typeof bk.size_bytes === 'number'}
					<Tooltip
						text={bk.computed_at
							? `as of ${new Date(bk.computed_at).toLocaleString()} · ${bk.object_count ?? 0} objects`
							: ''}
					>
						{fmtBytes(bk.size_bytes)}
					</Tooltip>
				{:else}
					<span title="Not yet computed — refresh after the next scan">…</span>
				{/if}
			</td>
			<td class="px-3 font-mono text-[12px] text-[var(--stw-fg-mute)]">
				{bk.created_at ? new Date(bk.created_at).toLocaleString() : '—'}
			</td>
			<td class="px-3 text-right text-[var(--stw-fg-soft)]">
				<span class="inline-flex items-center gap-1.5">
					{#if isAdmin}
						{#snippet trashIcon()}<Trash2 size={13} strokeWidth={1.7} />{/snippet}
						<Tooltip text="Delete bucket">
							<IconButton
								label="Delete"
								size={24}
								icon={trashIcon}
								onclick={(e) => destroy(bk.name, e)}
							/>
						</Tooltip>
					{/if}
					<Chevron size={12} dir="right" />
				</span>
			</td>
		{/snippet}
	</DataTable>

	{#if err}
		<div class="mt-4">
			<Banner variant="err" title="Backend unreachable">
				<span class="font-mono text-[12.5px]">{err}</span>
				{#snippet actions()}
					<Button variant="ghost" size="sm" onclick={() => refreshBuckets(b.id)}>Retry</Button>
				{/snippet}
			</Banner>
		</div>
	{/if}

	{#if confirmDeleteBucket}
		<ConfirmDialog
			title={`Delete bucket "${confirmDeleteBucket.name}"?`}
			description="The bucket and all its contents will be permanently removed. This cannot be undone."
			variant="danger"
			confirmLabel="Delete"
			busy={confirmDeleteBucket.busy}
			onconfirm={runDeleteBucket}
			oncancel={() => (confirmDeleteBucket = null)}
		/>
	{/if}
</div>
