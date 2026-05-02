<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { Plus, Trash2, RotateCw, KeyRound } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import Tooltip from '$lib/components/ui/Tooltip.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import SearchField from '$lib/components/ui/SearchField.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import StatLine from '$lib/components/ui/StatLine.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import {
		DataTable,
		DataTablePagination,
		createDataTable,
		type Column
	} from '$lib/components/ui/table';
	import Toggle from '$lib/components/ui/Toggle.svelte';
	import S3CredentialDrawer from './S3CredentialDrawer.svelte';
	import { api, ApiException } from '$lib/api';
	import type { Backend, S3Credential } from '$lib/types';

	interface Props {
		initialCreds: S3Credential[];
		initialError: string | null;
		backends: Backend[];
	}

	let { initialCreds, initialError, backends }: Props = $props();

	let creds = $state<S3Credential[]>(untrack(() => initialCreds));
	let error = $state<string | null>(untrack(() => initialError));
	let q = $state('');
	let busy = $state(false);
	let showCreate = $state(false);

	const activeCount = $derived(
		creds.filter(
			(c) => c.enabled && (!c.expires_at || new Date(c.expires_at).getTime() > Date.now())
		).length
	);
	const expiringSoon = $derived(
		creds.filter((c) => {
			if (!c.expires_at) return false;
			const ms = new Date(c.expires_at).getTime() - Date.now();
			return ms > 0 && ms < 7 * 86400_000;
		}).length
	);

	function credHaystack(c: S3Credential): string {
		return `${c.access_key} ${c.backend_id} ${c.buckets.join(' ')} ${c.description ?? ''}`.toLowerCase();
	}

	async function refresh(): Promise<void> {
		busy = true;
		error = null;
		try {
			creds = await api.listMyS3Credentials();
		} catch (err) {
			error = err instanceof ApiException ? err.message : 'Failed to load.';
		} finally {
			busy = false;
		}
	}

	async function toggleEnabled(c: S3Credential, next: boolean): Promise<void> {
		try {
			const updated = await api.patchMyS3Credential(c.access_key, { enabled: next });
			toast.success(next ? 'Credential enabled' : 'Credential disabled');
			creds = creds.map((x) => (x.access_key === c.access_key ? updated : x));
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Toggle failed.');
		}
	}

	async function destroy(c: S3Credential): Promise<void> {
		if (
			!confirm(`Delete credential ${c.access_key}? Any client using it will get 403 immediately.`)
		)
			return;
		try {
			await api.deleteMyS3Credential(c.access_key);
			toast.success('Credential deleted');
			creds = creds.filter((x) => x.access_key !== c.access_key);
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Delete failed.');
		}
	}

	function expiresLabel(s?: string): { text: string; tone: 'normal' | 'warn' | 'soft' } {
		if (!s) return { text: 'Never', tone: 'soft' };
		const ms = new Date(s).getTime() - Date.now();
		if (ms <= 0) return { text: 'Expired', tone: 'warn' };
		const day = 24 * 60 * 60 * 1000;
		if (ms < 60 * 60 * 1000)
			return { text: `in ${Math.max(1, Math.round(ms / 60000))}m`, tone: 'warn' };
		if (ms < day) return { text: `in ${Math.round(ms / (60 * 60 * 1000))}h`, tone: 'warn' };
		if (ms < 30 * day) return { text: `in ${Math.round(ms / day)}d`, tone: 'normal' };
		return { text: new Date(s).toLocaleDateString(), tone: 'normal' };
	}

	const columns: Column<S3Credential>[] = [
		{ id: 'akid', accessorKey: 'access_key', header: 'Access key', enableSorting: true },
		{ id: 'backend', accessorKey: 'backend_id', header: 'Backend', enableSorting: true },
		{ id: 'buckets', header: 'Buckets', enableSorting: false },
		{ id: 'description', accessorKey: 'description', header: 'Description', enableSorting: true },
		{
			id: 'expires',
			accessorKey: 'expires_at',
			header: 'Expires',
			align: 'right',
			enableSorting: true
		},
		{ id: 'status', accessorKey: 'enabled', header: 'Status', align: 'right', enableSorting: true },
		{ id: 'actions', header: '', align: 'right', enableSorting: false }
	];

	const credTable = createDataTable<S3Credential>({
		data: () => creds,
		columns,
		initialSorting: [{ id: 'akid', desc: false }],
		enablePagination: true,
		pageSize: 50,
		globalFilterFn: (row, _id, value) => {
			const v = String(value ?? '').toLowerCase();
			return v === '' || credHaystack(row.original).includes(v);
		}
	});

	$effect(() => {
		credTable.table.setGlobalFilter(q);
	});

	onMount(() => {
		void refresh();
	});
</script>

<div class="flex flex-col gap-4 stw-page-pad">
	<PageHeader
		title="My S3 credentials"
		subtitle="Personal virtual access keys for the embedded S3 proxy"
	>
		{#snippet meta()}
			<StatLine
				items={[
					{ label: 'total', value: creds.length },
					{ label: 'active', value: activeCount },
					{ label: 'expiring soon', value: expiringSoon }
				]}
			/>
		{/snippet}
		{#snippet actions()}
			{#snippet refreshIcon()}<RotateCw size={12} strokeWidth={1.7} />{/snippet}
			<Button size="sm" icon={refreshIcon} onclick={() => refresh()} disabled={busy}>
				Refresh
			</Button>
			{#snippet plusIcon()}<Plus size={13} strokeWidth={1.7} />{/snippet}
			<Button icon={plusIcon} variant="primary" onclick={() => (showCreate = true)}>
				New credential
			</Button>
		{/snippet}
	</PageHeader>

	<SearchField bind:value={q} placeholder="Filter by key, bucket, description…" width="320px" />

	{#if error}
		<Banner variant="err" role="alert">{error}</Banner>
	{:else if creds.length === 0}
		{#snippet keyIcon()}<KeyRound size={22} strokeWidth={1.7} />{/snippet}
		<EmptyState variant="card" icon={keyIcon} title="No credentials yet.">
			<div>
				Mint a credential to authenticate SDK or CLI traffic against the embedded S3 proxy.
				Operator-provisioned (BucketClaim) credentials never appear here — those are managed by your
				cluster admin.
			</div>
		</EmptyState>
	{:else}
		<DataTable table={credTable.table} emptyText="No credentials match.">
			{#snippet row(c)}
				{@const exp = expiresLabel(c.expires_at)}
				<td class="px-3 font-mono text-[12.5px]">{c.access_key}</td>
				<td class="px-3 font-mono text-[12px] text-stw-fg-mute">{c.backend_id}</td>
				<td class="px-3">
					<div class="flex max-w-[260px] flex-wrap gap-1">
						{#each c.buckets.slice(0, 3) as b (b)}
							<span
								class="rounded bg-stw-bg-hover px-1.5 py-0.5 font-mono text-[11px] text-stw-fg-mute"
								>{b}</span
							>
						{/each}
						{#if c.buckets.length > 3}
							<span class="text-[11px] text-stw-fg-soft">+{c.buckets.length - 3} more</span>
						{/if}
					</div>
				</td>
				<td class="px-3 text-[12px] text-stw-fg-mute">{c.description ?? '—'}</td>
				<td
					class="px-3 text-right font-mono text-[11.5px] {exp.tone === 'warn'
						? 'text-stw-warn'
						: exp.tone === 'soft'
							? 'text-stw-fg-soft'
							: 'text-stw-fg-mute'}"
				>
					{exp.text}
				</td>
				<td class="px-3 text-right">
					<Toggle value={c.enabled} onchange={(v) => toggleEnabled(c, v)} />
				</td>
				<td class="px-3 text-right">
					{#snippet trashIcon()}<Trash2 size={13} strokeWidth={1.7} />{/snippet}
					<Tooltip text="Delete">
						<IconButton label="Delete" size={24} icon={trashIcon} onclick={() => destroy(c)} />
					</Tooltip>
				</td>
			{/snippet}
		</DataTable>
		{#if creds.length > 50}
			<DataTablePagination table={credTable.table} />
		{/if}
	{/if}
</div>

{#if showCreate}
	<S3CredentialDrawer
		mode="me"
		{backends}
		oncreated={() => {
			showCreate = false;
			void refresh();
		}}
		onclose={() => (showCreate = false)}
	/>
{/if}
