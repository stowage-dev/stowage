<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { invalidateAll } from '$app/navigation';
	import { Activity, Database, AlertTriangle, RotateCw, HardDrive } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import StatCard from '$lib/components/ui/StatCard.svelte';
	import SectionCard from '$lib/components/ui/SectionCard.svelte';
	import { DataTable, createDataTable, type Column } from '$lib/components/ui/table';
	import Banner from '$lib/components/ui/Banner.svelte';
	import { bytes, num } from '$lib/format';
	import { session } from '$lib/stores/session.svelte';
	import type {
		DashboardBackendStorage,
		DashboardErrorEvent,
		DashboardTopBucket
	} from '$lib/types';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	const isAdmin = $derived(session.me?.role === 'admin');

	const peak = $derived(
		Math.max(1, ...(data.dashboard?.requests.hourly.map((h) => h.requests) ?? [1]))
	);

	function hourLabel(unixHour: number): string {
		const d = new Date(unixHour * 3600 * 1000);
		return d.getHours().toString().padStart(2, '0');
	}

	let refreshing = $state(false);
	async function refresh(): Promise<void> {
		refreshing = true;
		try {
			await invalidateAll();
		} finally {
			refreshing = false;
		}
	}

	const storageColumns: Column<DashboardBackendStorage>[] = [
		{ accessorKey: 'backend_id', header: 'Backend', enableSorting: true },
		{ accessorKey: 'buckets', header: 'Buckets', align: 'right', mono: true, enableSorting: true },
		{ accessorKey: 'objects', header: 'Objects', align: 'right', mono: true, enableSorting: true },
		{ accessorKey: 'bytes', header: 'Bytes', align: 'right', mono: true, enableSorting: true }
	];

	const topColumns: Column<DashboardTopBucket>[] = [
		{ accessorKey: 'backend_id', header: 'Backend', enableSorting: true },
		{ accessorKey: 'bucket', header: 'Bucket', mono: true, enableSorting: true },
		{ accessorKey: 'objects', header: 'Objects', align: 'right', mono: true, enableSorting: true },
		{ accessorKey: 'bytes', header: 'Bytes', align: 'right', mono: true, enableSorting: true }
	];

	const errorColumns: Column<DashboardErrorEvent>[] = [
		{ accessorKey: 'when', header: 'When', enableSorting: true },
		{ accessorKey: 'status', header: 'Status', align: 'right', mono: true, enableSorting: true },
		{ accessorKey: 'method', header: 'Method' },
		{ accessorKey: 'path', header: 'Path', mono: true },
		{ accessorKey: 'backend', header: 'Backend' },
		{ accessorKey: 'user_id', header: 'User' }
	];

	const storageTable = createDataTable<DashboardBackendStorage>({
		data: () => data.dashboard?.storage.by_backend ?? [],
		columns: storageColumns
	});
	const topTable = createDataTable<DashboardTopBucket>({
		data: () => data.dashboard?.storage.top_buckets ?? [],
		columns: topColumns,
		initialSorting: [{ id: 'bytes', desc: true }]
	});
	const errorTable = createDataTable<DashboardErrorEvent>({
		data: () => data.dashboard?.requests.recent_errors ?? [],
		columns: errorColumns,
		initialSorting: [{ id: 'when', desc: true }]
	});
</script>

<div class="stw-page-pad mx-auto flex max-w-[1100px] flex-col gap-[18px]">
	<PageHeader
		title="Admin dashboard"
		subtitle="Live request volume from this proxy and storage usage from the quota cache."
	>
		{#snippet actions()}
			{#snippet refreshIcon()}<RotateCw size={12} strokeWidth={1.7} />{/snippet}
			<Button size="sm" icon={refreshIcon} onclick={refresh} disabled={refreshing}>
				{refreshing ? 'Refreshing…' : 'Refresh'}
			</Button>
		{/snippet}
	</PageHeader>

	{#if !isAdmin}
		<Banner variant="err">Admin only.</Banner>
	{:else if data.error}
		<Banner variant="err">{data.error}</Banner>
	{:else if !data.dashboard}
		<Banner variant="err">No dashboard data.</Banner>
	{:else}
		{@const d = data.dashboard}

		<div class="grid gap-3" style="grid-template-columns:repeat(auto-fit,minmax(180px,1fr));">
			{#snippet activityIcon()}<Activity size={14} strokeWidth={1.7} />{/snippet}
			{#snippet hardDriveIcon()}<HardDrive size={14} strokeWidth={1.7} />{/snippet}
			{#snippet databaseIcon()}<Database size={14} strokeWidth={1.7} />{/snippet}
			{#snippet alertIcon()}<AlertTriangle size={14} strokeWidth={1.7} />{/snippet}
			<StatCard
				label="Requests (24h)"
				value={num(d.requests.total_24h)}
				sublabel="{num(d.requests.errors_24h)} server errors"
				icon={activityIcon}
				mono
			/>
			<StatCard
				label="Storage tracked"
				value={bytes(d.storage.by_backend.reduce((a, b) => a + b.bytes, 0))}
				sublabel="{num(
					d.storage.by_backend.reduce((a, b) => a + b.objects, 0)
				)} objects across {d.storage.by_backend.reduce((a, b) => a + b.buckets, 0)} buckets"
				icon={hardDriveIcon}
				mono
			/>
			<StatCard
				label="Backends"
				value={d.storage.by_backend.length}
				sublabel="currently reporting usage"
				icon={databaseIcon}
				mono
			/>
			<StatCard
				label="Recent errors"
				value={num(d.requests.recent_errors.length)}
				sublabel="5xx events in the buffer"
				icon={alertIcon}
				mono
			/>
		</div>

		<SectionCard title="Hourly requests (last 24h)">
			{#snippet icon()}<Activity size={14} strokeWidth={1.7} />{/snippet}
			<div
				class="grid h-[120px] items-end gap-[3px] px-1 pt-2 pb-1"
				role="img"
				aria-label="Hourly request histogram"
				style="grid-template-columns:repeat(24,1fr);"
			>
				{#each d.requests.hourly as h (h.unix_hour)}
					{@const pct = (h.requests / peak) * 100}
					{@const errPct = h.requests > 0 ? (h.errors / h.requests) * 100 : 0}
					<div
						class="flex h-full flex-col items-center justify-end gap-1"
						title="{hourLabel(h.unix_hour)}h · {h.requests} req · {h.errors} err"
					>
						<div
							class="relative min-h-[1px] w-full rounded-t bg-[var(--stw-accent-500)] transition-[height] duration-200"
							style="height:{pct}%;"
						>
							{#if errPct > 0}
								<div
									class="absolute top-0 right-0 left-0 bg-[var(--stw-err)]"
									style="height:{errPct}%;"
								></div>
							{/if}
						</div>
						<span class="font-mono text-[9.5px] text-[var(--stw-fg-soft)]">
							{hourLabel(h.unix_hour)}
						</span>
					</div>
				{/each}
			</div>
			{#if Object.keys(d.requests.by_backend).length > 0}
				<div class="mt-2 flex flex-wrap gap-1.5 border-t border-[var(--stw-border)] pt-2">
					{#each Object.entries(d.requests.by_backend) as [bid, count] (bid)}
						<span
							class="inline-flex items-center gap-1 rounded border border-[var(--stw-border)] bg-[var(--stw-bg-sunken)] px-2 py-0.5 text-[11.5px] text-[var(--stw-fg-mute)]"
						>
							<strong>{bid}</strong> · {num(count as number)} req
						</span>
					{/each}
				</div>
			{/if}
		</SectionCard>

		<SectionCard title="Storage by backend">
			{#snippet icon()}<HardDrive size={14} strokeWidth={1.7} />{/snippet}
			{#if d.storage.cache_note}
				<p class="m-0 mb-2 text-[11.5px] text-[var(--stw-fg-soft)]">{d.storage.cache_note}</p>
			{/if}
			<DataTable table={storageTable.table} density="compact" emptyText="No tracked buckets yet.">
				{#snippet row(r)}
					<td class="px-3">{r.backend_id}</td>
					<td class="px-3 text-right font-mono">{num(r.buckets)}</td>
					<td class="px-3 text-right font-mono">{num(r.objects)}</td>
					<td class="px-3 text-right font-mono">{bytes(r.bytes)}</td>
				{/snippet}
			</DataTable>
		</SectionCard>

		<SectionCard title="Top buckets by size">
			{#snippet icon()}<Database size={14} strokeWidth={1.7} />{/snippet}
			<DataTable
				table={topTable.table}
				density="compact"
				emptyText="No data — set quotas on buckets to populate."
			>
				{#snippet row(r)}
					<td class="px-3">{r.backend_id}</td>
					<td class="px-3 font-mono">{r.bucket}</td>
					<td class="px-3 text-right font-mono">{num(r.objects)}</td>
					<td class="px-3 text-right font-mono">{bytes(r.bytes)}</td>
				{/snippet}
			</DataTable>
		</SectionCard>

		<SectionCard title="Recent errors">
			{#snippet icon()}<AlertTriangle size={14} strokeWidth={1.7} />{/snippet}
			<DataTable table={errorTable.table} density="compact" emptyText="No recent server errors. 🎉">
				{#snippet row(e)}
					<td class="px-3 text-[11.5px] text-[var(--stw-fg-soft)]">
						{new Date(e.when).toLocaleTimeString()}
					</td>
					<td class="px-3 text-right font-mono text-[var(--stw-err)]">{e.status}</td>
					<td class="px-3">{e.method}</td>
					<td class="px-3 font-mono text-[11.5px]">{e.path}</td>
					<td class="px-3">{e.backend ?? '—'}</td>
					<td class="px-3">{e.user_id ?? '—'}</td>
				{/snippet}
			</DataTable>
		</SectionCard>
	{/if}
</div>
