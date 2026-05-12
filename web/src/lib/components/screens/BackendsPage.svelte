<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { goto, invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { Plus, Pencil, Trash2, Lock } from 'lucide-svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Dot from '$lib/components/ui/Dot.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import Tooltip from '$lib/components/ui/Tooltip.svelte';
	import BackendMark from '$lib/components/ui/BackendMark.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import StatLine from '$lib/components/ui/StatLine.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import EndpointFormPanel from './EndpointFormPanel.svelte';
	import { urlForRoute } from '$lib/route';
	import { inferKind, BACKEND_KINDS, backendHealth, type BackendHealth } from '$lib/backend-kind';
	import { api, ApiException } from '$lib/api';
	import { messageFor } from '$lib/i18n';
	import { session } from '$lib/stores/session.svelte';
	import { bucketList } from '$lib/stores/buckets.svelte';
	import type { Backend, Endpoint } from '$lib/types';
	import { SvelteMap } from 'svelte/reactivity';

	interface Props {
		backends: Backend[];
		admin: { endpoints: Endpoint[]; error: string | null } | null;
	}

	let { backends, admin }: Props = $props();

	const isAdmin = $derived(session.me?.role === 'admin');
	const yamlMessage = messageFor('yaml_managed');

	type Row = {
		id: string;
		name: string;
		backend?: Backend;
		endpoint?: Endpoint;
	};

	const rows = $derived.by<Row[]>(() => {
		const m = new SvelteMap<string, Row>();
		for (const b of backends) m.set(b.id, { id: b.id, name: b.name, backend: b });
		if (admin) {
			for (const e of admin.endpoints) {
				const r = m.get(e.id);
				if (r) r.endpoint = e;
				else m.set(e.id, { id: e.id, name: e.name || e.id, endpoint: e });
			}
		}
		return Array.from(m.values());
	});

	const healthyCount = $derived(rows.filter((r) => rowHealth(r).state === 'ok').length);
	const yamlCount = $derived(rows.filter((r) => r.endpoint?.source === 'config').length);

	function rowKind(r: Row) {
		return inferKind(r.backend ?? { id: r.id, name: r.name });
	}

	function rowHealth(r: Row): BackendHealth {
		if (r.backend) return backendHealth(r.backend);
		const e = r.endpoint;
		if (!e) return { state: 'warn', message: 'Health unknown' };
		if (!e.enabled) return { state: 'warn', message: 'Disabled' };
		if (e.healthy) return { state: 'ok' };
		if (e.last_error) return { state: 'err', message: e.last_error };
		return { state: 'warn', message: 'Health unknown' };
	}

	function bucketCount(r: Row): number | null {
		if (!r.backend) return null;
		return bucketList(r.backend.id)?.length ?? null;
	}

	function lastProbeAt(r: Row): string | null {
		const ts = r.backend?.last_probe_at ?? r.endpoint?.last_probe_at;
		return ts ? new Date(ts).toLocaleTimeString() : null;
	}

	function open(r: Row) {
		if (!r.backend) return;
		goto(urlForRoute({ type: 'backend', backend: r.backend.id }));
	}

	function onCardKey(r: Row, e: KeyboardEvent) {
		if (!r.backend) return;
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			open(r);
		}
	}

	let panelMode = $state<'closed' | 'create' | { mode: 'edit'; endpoint: Endpoint }>('closed');

	function startEdit(r: Row, ev: MouseEvent) {
		ev.stopPropagation();
		const e = r.endpoint;
		if (!e) return;
		if (e.source === 'config') {
			toast.error(yamlMessage);
			return;
		}
		panelMode = { mode: 'edit', endpoint: e };
	}

	async function destroy(r: Row, ev: MouseEvent) {
		ev.stopPropagation();
		const e = r.endpoint;
		if (!e) return;
		if (e.source === 'config') {
			toast.error(yamlMessage);
			return;
		}
		if (!confirm(`Delete backend "${e.id}"? This cannot be undone.`)) return;
		try {
			await api.deleteEndpoint(e.id);
			toast.success(`Deleted ${e.id}`);
			await invalidateAll();
		} catch (err) {
			toast.error(
				err instanceof ApiException ? messageFor(err.code, err.message) : 'Delete failed.'
			);
		}
	}

	async function onSaved() {
		panelMode = 'closed';
		await invalidateAll();
	}
</script>

<div class="stw-page-pad">
	<PageHeader title="Backends">
		{#snippet meta()}
			<StatLine
				items={[
					{ label: 'configured', value: rows.length },
					{ label: 'healthy', value: healthyCount },
					...(isAdmin && yamlCount > 0 ? [{ label: 'from config.yaml', value: yamlCount }] : [])
				]}
			/>
		{/snippet}
		{#snippet actions()}
			{#if isAdmin}
				{#snippet plusIcon()}<Plus size={14} strokeWidth={1.7} />{/snippet}
				<Button variant="primary" icon={plusIcon} onclick={() => (panelMode = 'create')}>
					Add backend
				</Button>
			{/if}
		{/snippet}
	</PageHeader>

	{#if isAdmin && admin?.error}
		<div class="mb-4">
			<Banner variant="err" role="alert" title="Could not load admin metadata">
				<span class="font-mono">{admin.error}</span>
			</Banner>
		</div>
	{/if}

	{#if rows.length === 0}
		<EmptyState variant="card">
			{#if isAdmin}
				No backends yet. Click "Add backend" to register an S3-compatible endpoint, or add one to
				<code>backends:</code> in the server config.
			{:else}
				No backends configured. Add one to <code>backends:</code> in the server config.
			{/if}
		</EmptyState>
	{:else}
		<div class="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-3">
			{#each rows as r (r.id)}
				{@const kind = rowKind(r)}
				{@const info = BACKEND_KINDS[kind]}
				{@const health = rowHealth(r)}
				{@const bc = bucketCount(r)}
				{@const ep = r.endpoint}
				{@const yaml = ep?.source === 'config'}
				{@const clickable = !!r.backend}
				{@const probe = lastProbeAt(r)}
				<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
				<div
					role={clickable ? 'button' : undefined}
					tabindex={clickable ? 0 : undefined}
					onclick={clickable ? () => open(r) : undefined}
					onkeydown={clickable ? (e) => onCardKey(r, e) : undefined}
					class="relative flex flex-col gap-2.5 rounded-lg border border-stw-border bg-stw-bg-panel p-3.5 text-left focus-ring transition-[border-color,box-shadow] duration-[120ms] {clickable
						? 'cursor-pointer hover:border-stw-border-strong hover:shadow-stw-sm'
						: ''}"
				>
					<div class="flex items-center gap-2.5">
						<BackendMark {kind} size={28} />
						<div class="min-w-0 flex-1">
							<div class="text-[14px] font-semibold tracking-[-0.01em]">
								{r.name}
								<span class="relative left-[5px]">
									<Dot variant={health.state} label={health.state} />
								</span>
							</div>
							<div class="font-mono text-[11.5px] text-stw-fg-soft">
								{info.label.toLowerCase()} · {r.id}
							</div>
						</div>
					</div>

					{#if isAdmin && (ep?.endpoint || ep?.region)}
						<div
							class="truncate font-mono text-[11.5px] text-stw-fg-mute"
							title={ep?.endpoint ?? ''}
						>
							{ep?.endpoint || '—'}{ep?.region ? ' · ' + ep.region : ''}
						</div>
					{/if}

					<div class="flex flex-wrap gap-1.5">
						{#if r.backend?.capabilities.versioning}
							<Badge>versioning</Badge>
						{/if}
						{#if r.backend?.capabilities.lifecycle}
							<Badge>lifecycle</Badge>
						{/if}
						{#if r.backend?.capabilities.admin_api}
							<Badge>admin:{r.backend.capabilities.admin_api}</Badge>
						{/if}
						{#if r.backend?.capabilities.cors}
							<Badge>cors</Badge>
						{/if}
						{#if isAdmin && yaml}
							<Tooltip text={yamlMessage}>
								<Badge variant="warn"><Lock size={10} strokeWidth={1.8} /> config</Badge>
							</Tooltip>
						{/if}
						{#if isAdmin && ep && !ep.enabled}
							<Badge>disabled</Badge>
						{/if}
					</div>

					<div class="flex justify-between text-[12px] text-stw-fg-mute tabular-nums">
						<span>{bc != null ? bc + ' buckets' : '—'}</span>
						<span>{probe ?? '—'}</span>
					</div>

					{#if r.backend && !r.backend.healthy && r.backend.last_error}
						<div class="font-mono text-[11.5px] text-stw-err">
							{r.backend.last_error}
						</div>
					{/if}

					{#if isAdmin && ep}
						<div class="absolute top-2 right-2 inline-flex gap-0.5 rounded-md bg-stw-bg-panel">
							{#if yaml}
								<Tooltip text={yamlMessage}>
									<span
										aria-disabled="true"
										class="inline-flex h-[24px] w-[24px] items-center justify-center text-stw-fg-soft opacity-45"
									>
										<Lock size={12} strokeWidth={1.8} />
									</span>
								</Tooltip>
							{:else}
								{#snippet pencilIcon()}<Pencil size={13} strokeWidth={1.7} />{/snippet}
								<Tooltip text="Edit">
									<IconButton
										label="Edit"
										size={24}
										icon={pencilIcon}
										onclick={(e) => startEdit(r, e)}
									/>
								</Tooltip>
								{#snippet trashIcon()}<Trash2 size={13} strokeWidth={1.7} />{/snippet}
								<Tooltip text="Delete">
									<IconButton
										label="Delete"
										size={24}
										icon={trashIcon}
										onclick={(e) => destroy(r, e)}
									/>
								</Tooltip>
							{/if}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>

{#if panelMode === 'create'}
	<EndpointFormPanel mode="create" onclose={() => (panelMode = 'closed')} onsaved={onSaved} />
{:else if typeof panelMode === 'object' && panelMode.mode === 'edit'}
	<EndpointFormPanel
		mode="edit"
		endpoint={panelMode.endpoint}
		onclose={() => (panelMode = 'closed')}
		onsaved={onSaved}
	/>
{/if}
