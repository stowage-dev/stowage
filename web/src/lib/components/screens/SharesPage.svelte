<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import {
		Link as LinkIcon,
		Lock,
		RotateCw,
		Trash2,
		ExternalLink,
		Copy,
		Check
	} from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Segmented from '$lib/components/ui/Segmented.svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import SearchField from '$lib/components/ui/SearchField.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import StatLine from '$lib/components/ui/StatLine.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import { DataTable, createDataTable, type Column } from '$lib/components/ui/table';
	import { api, ApiException } from '$lib/api';
	import { num } from '$lib/format';
	import { session } from '$lib/stores/session.svelte';
	import type { Share } from '$lib/types';

	interface Props {
		initialShares: Share[];
		initialError: string | null;
	}

	let { initialShares, initialError }: Props = $props();

	const isAdmin = $derived(session.me?.role === 'admin');

	let shares = $state<Share[]>(untrack(() => initialShares));
	let error = $state<string | null>(untrack(() => initialError));
	let scope = $state<'mine' | 'all'>('mine');
	let q = $state('');
	let busy = $state(false);
	let copiedID = $state<string | null>(null);

	function shareHaystack(s: Share): string {
		return `${s.bucket}/${s.key} ${s.code} ${s.created_by}`.toLowerCase();
	}
	const activeCount = $derived(shares.filter((s) => !s.revoked && !isExpired(s)).length);
	const passwordCount = $derived(shares.filter((s) => s.has_password).length);

	function isExpired(s: Share): boolean {
		if (!s.expires_at) return false;
		return new Date(s.expires_at).getTime() <= Date.now();
	}

	function expiryLabel(s: Share): { text: string; tone: 'normal' | 'warn' | 'soft' } {
		if (s.revoked) return { text: 'Revoked', tone: 'soft' };
		if (!s.expires_at) return { text: 'Never', tone: 'normal' };
		const ms = new Date(s.expires_at).getTime() - Date.now();
		if (ms <= 0) return { text: 'Expired', tone: 'warn' };
		const day = 24 * 60 * 60 * 1000;
		if (ms < 60 * 60 * 1000) {
			const mins = Math.max(1, Math.round(ms / 60000));
			return { text: `in ${mins}m`, tone: 'warn' };
		}
		if (ms < day) return { text: `in ${Math.round(ms / (60 * 60 * 1000))}h`, tone: 'warn' };
		if (ms < 30 * day) return { text: `in ${Math.round(ms / day)}d`, tone: 'normal' };
		return { text: new Date(s.expires_at).toLocaleDateString(), tone: 'normal' };
	}

	async function refresh(nextScope = scope): Promise<void> {
		busy = true;
		error = null;
		try {
			shares = await api.listShares(nextScope);
		} catch (err) {
			error = err instanceof ApiException ? err.message : 'Failed to load shares.';
		} finally {
			busy = false;
		}
	}

	async function setScope(next: 'mine' | 'all'): Promise<void> {
		scope = next;
		await refresh(next);
	}

	async function revoke(s: Share): Promise<void> {
		if (s.revoked) return;
		if (!confirm(`Revoke share for "${s.key}"? Recipients will get 410 Gone.`)) return;
		try {
			await api.revokeShare(s.id);
			toast.success('Share revoked');
			shares = shares.map((x) =>
				x.id === s.id ? { ...x, revoked: true, revoked_at: new Date().toISOString() } : x
			);
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Revoke failed.');
		}
	}

	function copyLink(s: Share): void {
		const url = api.shareURL(s);
		navigator.clipboard?.writeText(url);
		copiedID = s.id;
		setTimeout(() => {
			if (copiedID === s.id) copiedID = null;
		}, 1400);
	}

	onMount(() => {
		void refresh();
	});

	const columns: Column<Share>[] = [
		{ id: 'target', header: 'Target', enableSorting: false },
		{ id: 'link', header: 'Link', enableSorting: false },
		{
			id: 'downloads',
			accessorKey: 'download_count',
			header: 'Downloads',
			align: 'right',
			enableSorting: true
		},
		{
			id: 'expires',
			accessorKey: 'expires_at',
			header: 'Expires',
			align: 'right',
			enableSorting: true
		},
		{
			id: 'created',
			accessorKey: 'created_at',
			header: 'Created',
			align: 'right',
			enableSorting: true
		},
		{ id: 'actions', header: '', align: 'right', enableSorting: false }
	];

	const shareTable = createDataTable<Share>({
		data: () => shares,
		columns,
		initialSorting: [{ id: 'created', desc: true }],
		globalFilterFn: (row, _id, value) => {
			const v = String(value ?? '').toLowerCase();
			return v === '' || shareHaystack(row.original).includes(v);
		}
	});

	$effect(() => {
		shareTable.table.setGlobalFilter(q);
	});
</script>

<div class="stw-page-pad flex flex-col gap-4">
	<PageHeader title="Shares">
		{#snippet meta()}
			<StatLine
				items={[
					{ label: 'total', value: shares.length },
					{ label: 'active', value: activeCount },
					{ label: 'password-protected', value: passwordCount }
				]}
			/>
		{/snippet}
		{#snippet actions()}
			{#if isAdmin}
				<Segmented
					value={scope}
					onchange={(v) => setScope(v)}
					size="sm"
					options={[
						{ value: 'mine' as const, label: 'My shares' },
						{ value: 'all' as const, label: 'All shares' }
					]}
				/>
			{/if}
			{#snippet refreshIcon()}<RotateCw size={12} strokeWidth={1.7} />{/snippet}
			<Button size="sm" icon={refreshIcon} onclick={() => refresh()} disabled={busy}>
				Refresh
			</Button>
		{/snippet}
	</PageHeader>

	<SearchField bind:value={q} placeholder="Filter by bucket, key, code…" width="360px" />

	{#if error}
		<Banner variant="err" role="alert">{error}</Banner>
	{:else if shares.length === 0}
		{#snippet linkIcon()}<LinkIcon size={22} strokeWidth={1.7} />{/snippet}
		<EmptyState variant="card" icon={linkIcon} title="No shares yet.">
			<div>
				Open a file in any bucket and click <strong>Share</strong> to mint a link.
			</div>
		</EmptyState>
	{:else}
		<DataTable
			table={shareTable.table}
			emptyText={q ? `No shares match "${q}".` : 'No shares yet.'}
			rowClass={(r) => (r.original.revoked ? 'opacity-55' : '')}
		>
			{#snippet row(s)}
				{@const exp = expiryLabel(s)}
				{@const url = api.shareURL(s)}
				<td class="min-w-0 px-3">
					<div class="flex min-w-0 flex-col gap-0.5">
						<div class="truncate font-mono" title="{s.backend_id}/{s.bucket}/{s.key}">
							{s.bucket}/{s.key}
						</div>
						<div class="flex items-center gap-1.5 text-[11px] text-[var(--stw-fg-soft)]">
							<span>{s.backend_id}</span>
							{#if s.has_password}
								<span
									class="inline-flex items-center gap-0.5 text-[var(--stw-warn)]"
									title="Password-protected"
								>
									<Lock size={10} strokeWidth={1.7} />
								</span>
							{/if}
							{#if scope === 'all' && s.created_by}
								<span>· {s.created_by}</span>
							{/if}
						</div>
					</div>
				</td>
				<td class="px-3 font-mono text-[11.5px] text-[var(--stw-fg-mute)]">
					<span class="block max-w-[180px] truncate" title={url}>/s/{s.code}</span>
				</td>
				<td class="px-3 text-right font-mono">
					{num(s.download_count)}{#if s.max_downloads}<span class="text-[var(--stw-fg-soft)]"
							>/{s.max_downloads}</span
						>{/if}
				</td>
				<td
					class="px-3 text-right text-[12px] {exp.tone === 'warn'
						? 'text-[var(--stw-warn)]'
						: exp.tone === 'soft'
							? 'text-[var(--stw-fg-soft)]'
							: 'text-[var(--stw-fg-mute)]'}"
				>
					{exp.text}
				</td>
				<td class="px-3 text-right font-mono text-[11.5px] text-[var(--stw-fg-soft)]">
					{new Date(s.created_at).toLocaleDateString()}
				</td>
				<td class="px-3 text-right">
					{#if s.revoked}
						<Badge>revoked</Badge>
					{:else}
						{#snippet copyIcon()}
							{#if copiedID === s.id}
								<Check size={13} strokeWidth={1.7} />
							{:else}
								<Copy size={13} strokeWidth={1.7} />
							{/if}
						{/snippet}
						{#snippet openIcon()}<ExternalLink size={13} strokeWidth={1.7} />{/snippet}
						{#snippet trashIcon()}<Trash2 size={13} strokeWidth={1.7} />{/snippet}
						<span class="inline-flex gap-0.5">
							<IconButton
								icon={copyIcon}
								onclick={() => copyLink(s)}
								title={copiedID === s.id ? 'Copied' : 'Copy link'}
								label={copiedID === s.id ? 'Copied' : 'Copy link'}
							/>
							<IconButton
								icon={openIcon}
								onclick={() => window.open(url, '_blank')}
								title="Open in new tab"
								label="Open in new tab"
							/>
							<IconButton
								icon={trashIcon}
								onclick={() => revoke(s)}
								title="Revoke"
								label="Revoke share"
							/>
						</span>
					{/if}
				</td>
			{/snippet}
		</DataTable>
	{/if}
</div>
