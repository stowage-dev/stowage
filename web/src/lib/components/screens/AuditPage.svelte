<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { Activity, Download, RotateCw, Search } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import FilterBar from '$lib/components/ui/FilterBar.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import DataTable from '$lib/components/ui/DataTable.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import { api, ApiException } from '$lib/api';
	import type { AuditEvent, AuditFilter } from '$lib/types';

	interface Props {
		initialEvents: AuditEvent[];
		initialTotal: number;
		initialError: string | null;
	}

	let { initialEvents, initialTotal, initialError }: Props = $props();

	let events = $state<AuditEvent[]>(untrack(() => initialEvents));
	let total = $state(untrack(() => initialTotal));
	let error = $state<string | null>(untrack(() => initialError));

	let userQ = $state('');
	let actionQ = $state('');
	let backendQ = $state('');
	let bucketQ = $state('');
	let statusQ = $state<'' | 'ok' | 'error' | 'denied'>('');
	let fromQ = $state('');
	let toQ = $state('');
	let limit = $state(200);

	let busy = $state(false);

	function currentFilter(): AuditFilter {
		const f: AuditFilter = { limit };
		if (userQ.trim()) f.user = userQ.trim();
		if (actionQ.trim()) f.action = actionQ.trim();
		if (backendQ.trim()) f.backend = backendQ.trim();
		if (bucketQ.trim()) f.bucket = bucketQ.trim();
		if (statusQ) f.status = statusQ;
		if (fromQ) f.from = new Date(fromQ).toISOString();
		if (toQ) f.to = new Date(toQ).toISOString();
		return f;
	}

	async function reload(): Promise<void> {
		busy = true;
		error = null;
		try {
			const res = await api.listAudit(currentFilter());
			events = res.events;
			total = res.total;
		} catch (err) {
			error = err instanceof ApiException ? err.message : 'Failed to load audit log.';
		} finally {
			busy = false;
		}
	}

	function clearFilters(): void {
		userQ = '';
		actionQ = '';
		backendQ = '';
		bucketQ = '';
		statusQ = '';
		fromQ = '';
		toQ = '';
		void reload();
	}

	function downloadCSV(): void {
		try {
			window.location.href = api.auditCSVURL(currentFilter());
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Could not download CSV.');
		}
	}

	function statusToneCls(s: string): string {
		if (s === 'error') return 'text-stw-err';
		if (s === 'denied') return 'text-stw-warn';
		return 'text-stw-ok';
	}

	function prettyDetail(s?: string): string {
		if (!s) return '';
		try {
			const obj = JSON.parse(s) as Record<string, unknown>;
			return Object.entries(obj)
				.map(([k, v]) => `${k}=${JSON.stringify(v)}`)
				.join(' ');
		} catch {
			return s;
		}
	}

	const columns = [
		{ key: 'time', label: 'Time' },
		{ key: 'action', label: 'Action' },
		{ key: 'status', label: 'Status' },
		{ key: 'user', label: 'User' },
		{ key: 'target', label: 'Target' },
		{ key: 'detail', label: 'Detail' },
		{ key: 'ip', label: 'IP' }
	];
</script>

<div class="flex flex-col gap-4 stw-page-pad">
	<PageHeader
		title="Audit log"
		subtitle="{total.toLocaleString('en-US')} events total · showing {events.length} most recent"
	>
		{#snippet icon()}
			<Activity size={18} strokeWidth={1.7} />
		{/snippet}
		{#snippet actions()}
			{#snippet refreshIcon()}<RotateCw size={12} strokeWidth={1.7} />{/snippet}
			{#snippet downloadIcon()}<Download size={12} strokeWidth={1.7} />{/snippet}
			<Button size="sm" icon={refreshIcon} onclick={reload} disabled={busy}>
				{busy ? 'Refreshing…' : 'Refresh'}
			</Button>
			<Button size="sm" icon={downloadIcon} onclick={downloadCSV}>CSV</Button>
		{/snippet}
	</PageHeader>

	<FilterBar minColumn="140px">
		<FormField label="User ID">
			<input class="stw-input font-mono" bind:value={userQ} placeholder="ULID" />
		</FormField>
		<FormField label="Action">
			<input
				class="stw-input font-mono"
				bind:value={actionQ}
				placeholder="share. or object.delete"
				title="Prefix match: share. matches all share events"
			/>
		</FormField>
		<FormField label="Backend">
			<input class="stw-input font-mono" bind:value={backendQ} placeholder="prod-garage" />
		</FormField>
		<FormField label="Bucket">
			<input class="stw-input font-mono" bind:value={bucketQ} placeholder="docs" />
		</FormField>
		<FormField label="Status">
			<select class="stw-input font-mono" bind:value={statusQ}>
				<option value="">any</option>
				<option value="ok">ok</option>
				<option value="error">error</option>
				<option value="denied">denied</option>
			</select>
		</FormField>
		<FormField label="From">
			<input class="stw-input font-mono" type="datetime-local" bind:value={fromQ} />
		</FormField>
		<FormField label="To">
			<input class="stw-input font-mono" type="datetime-local" bind:value={toQ} />
		</FormField>
		<FormField label="Limit">
			<input class="stw-input font-mono" type="number" min="1" max="1000" bind:value={limit} />
		</FormField>

		{#snippet actions()}
			{#snippet searchIcon()}<Search size={12} strokeWidth={1.7} />{/snippet}
			<Button variant="primary" size="sm" icon={searchIcon} onclick={reload} disabled={busy}>
				Apply
			</Button>
			<Button variant="ghost" size="sm" onclick={clearFilters}>Clear</Button>
		{/snippet}
	</FilterBar>

	{#if error}
		<Banner variant="err" role="alert">{error}</Banner>
	{:else if events.length === 0}
		{#snippet auditEmptyIcon()}<Activity size={20} strokeWidth={1.5} />{/snippet}
		<EmptyState variant="card" icon={auditEmptyIcon} hint="No events match these filters." />
	{:else}
		<DataTable {columns} rows={events} rowHeight={36}>
			{#snippet row(e)}
				<td class="px-3 py-2 align-top font-mono text-[11.5px] whitespace-nowrap text-stw-fg-mute">
					{new Date(e.timestamp).toLocaleString()}
				</td>
				<td class="px-3 py-2 align-top font-mono text-[12.5px] whitespace-nowrap text-stw-fg">
					{e.action}
				</td>
				<td class="px-3 py-2 align-top">
					<span class={statusToneCls(e.status)}>{e.status}</span>
				</td>
				<td class="px-3 py-2 align-top">{e.user_id ?? '—'}</td>
				<td class="max-w-[280px] truncate px-3 py-2 align-top font-mono text-[11.5px]">
					{#if e.backend}
						<span class="text-stw-fg-soft">{e.backend}</span>
					{/if}
					{#if e.bucket}
						<span>/{e.bucket}</span>
					{/if}
					{#if e.key}
						<span class="text-stw-fg-mute">/{e.key}</span>
					{/if}
					{#if !e.backend && !e.bucket && !e.key}—{/if}
				</td>
				<td
					class="max-w-[320px] truncate px-3 py-2 align-top font-mono text-[11px] text-stw-fg-soft"
					title={e.detail ?? ''}
				>
					{prettyDetail(e.detail)}
				</td>
				<td class="px-3 py-2 align-top font-mono text-[11.5px] text-stw-fg-mute">
					{e.ip ?? '—'}
				</td>
			{/snippet}
		</DataTable>
	{/if}
</div>
