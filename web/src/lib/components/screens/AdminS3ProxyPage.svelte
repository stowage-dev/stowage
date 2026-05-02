<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import {
		Plus,
		Trash2,
		RotateCw,
		KeyRound,
		Globe,
		Cloud,
		ServerCog,
		PowerOff
	} from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import Tooltip from '$lib/components/ui/Tooltip.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import SearchField from '$lib/components/ui/SearchField.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import StatLine from '$lib/components/ui/StatLine.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import Segmented from '$lib/components/ui/Segmented.svelte';
	import DataTable from '$lib/components/ui/DataTable.svelte';
	import Toggle from '$lib/components/ui/Toggle.svelte';
	import S3CredentialDrawer from './S3CredentialDrawer.svelte';
	import S3AnonymousDrawer from './S3AnonymousDrawer.svelte';
	import { api, ApiException } from '$lib/api';
	import type { Backend, S3AnonymousBinding, S3CredentialView } from '$lib/types';

	interface Props {
		initialCreds: S3CredentialView[];
		initialAnon: S3AnonymousBinding[];
		initialError: string | null;
		initialDisabled?: boolean;
		backends: Backend[];
	}

	let {
		initialCreds,
		initialAnon,
		initialError,
		initialDisabled = false,
		backends
	}: Props = $props();

	type Tab = 'credentials' | 'anonymous';

	let creds = $state<S3CredentialView[]>(untrack(() => initialCreds));
	let bindings = $state<S3AnonymousBinding[]>(untrack(() => initialAnon));
	let error = $state<string | null>(untrack(() => initialError));
	let disabled = $state<boolean>(untrack(() => initialDisabled));
	let tab = $state<Tab>('credentials');
	let q = $state('');
	let busy = $state(false);
	let showCreateCred = $state(false);
	let showCreateAnon = $state(false);

	const operatorCreds = $derived(creds.filter((c) => c.source === 'kubernetes'));
	const sqliteCreds = $derived(creds.filter((c) => c.source === 'sqlite'));

	const filteredCreds = $derived(
		creds.filter((c) => {
			if (q === '') return true;
			const hay =
				`${c.access_key} ${c.backend_id} ${c.buckets.join(' ')} ${c.description ?? ''} ${c.claim_namespace ?? ''} ${c.claim_name ?? ''}`.toLowerCase();
			return hay.includes(q.toLowerCase());
		})
	);
	const filteredAnon = $derived(
		bindings.filter((b) => {
			if (q === '') return true;
			const hay = `${b.backend_id} ${b.bucket}`.toLowerCase();
			return hay.includes(q.toLowerCase());
		})
	);

	async function refresh(): Promise<void> {
		busy = true;
		error = null;
		try {
			const [c, b] = await Promise.all([api.listS3ProxyCredentials(), api.listS3ProxyAnonymous()]);
			creds = c;
			bindings = b;
			disabled = false;
		} catch (err) {
			if (err instanceof ApiException && err.code === 's3_proxy_disabled') {
				disabled = true;
			} else {
				error = err instanceof ApiException ? err.message : 'Failed to load.';
			}
		} finally {
			busy = false;
		}
	}

	async function toggleEnabled(c: S3CredentialView, next: boolean): Promise<void> {
		if (c.source !== 'sqlite') return;
		try {
			await api.patchAdminS3Credential(c.access_key, { enabled: next });
			toast.success(next ? 'Credential enabled' : 'Credential disabled');
			await refresh();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Toggle failed.');
		}
	}

	async function destroyCred(c: S3CredentialView): Promise<void> {
		if (c.source !== 'sqlite') return;
		if (!confirm(`Delete credential ${c.access_key}? Tenants using it will get 403 immediately.`))
			return;
		try {
			await api.deleteAdminS3Credential(c.access_key);
			toast.success('Credential deleted');
			creds = creds.filter((x) => x.access_key !== c.access_key);
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Delete failed.');
		}
	}

	async function destroyBinding(b: S3AnonymousBinding): Promise<void> {
		if (b.source === 'kubernetes') return;
		if (!confirm(`Remove anonymous binding for ${b.backend_id}/${b.bucket}?`)) return;
		try {
			await api.deleteAdminS3Anonymous(b.backend_id, b.bucket);
			toast.success('Binding removed');
			bindings = bindings.filter((x) => !(x.backend_id === b.backend_id && x.bucket === b.bucket));
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Delete failed.');
		}
	}

	function expiresLabel(s?: string): string {
		if (!s) return '—';
		const ms = new Date(s).getTime() - Date.now();
		if (ms <= 0) return 'expired';
		const day = 24 * 60 * 60 * 1000;
		if (ms < 60 * 60 * 1000) return `in ${Math.max(1, Math.round(ms / 60000))}m`;
		if (ms < day) return `in ${Math.round(ms / (60 * 60 * 1000))}h`;
		if (ms < 30 * day) return `in ${Math.round(ms / day)}d`;
		return new Date(s).toLocaleDateString();
	}

	const credColumns = [
		{ key: 'akid', label: 'Access key' },
		{ key: 'source', label: 'Source' },
		{ key: 'backend', label: 'Backend' },
		{ key: 'buckets', label: 'Buckets' },
		{ key: 'owner', label: 'Owner' },
		{ key: 'expires', label: 'Expires', align: 'right' as const },
		{ key: 'status', label: 'Status', align: 'right' as const },
		{ key: 'actions', label: '', align: 'right' as const }
	];

	const anonColumns = [
		{ key: 'target', label: 'Target' },
		{ key: 'source', label: 'Source' },
		{ key: 'mode', label: 'Mode' },
		{ key: 'rps', label: 'Per-IP RPS', align: 'right' as const },
		{ key: 'created', label: 'Created', align: 'right' as const },
		{ key: 'actions', label: '', align: 'right' as const }
	];

	onMount(() => {
		if (!disabled) void refresh();
	});
</script>

<div class="flex flex-col gap-4 stw-page-pad">
	<PageHeader title="S3 Proxy">
		{#snippet meta()}
			<StatLine
				items={[
					{ label: 'credentials', value: creds.length },
					{ label: 'operator', value: operatorCreds.length },
					{ label: 'ui-managed', value: sqliteCreds.length },
					{ label: 'anonymous', value: bindings.length }
				]}
			/>
		{/snippet}
		{#snippet actions()}
			{#if !disabled}
				{#snippet refreshIcon()}<RotateCw size={12} strokeWidth={1.7} />{/snippet}
				<Button size="sm" icon={refreshIcon} onclick={() => refresh()} disabled={busy}>
					Refresh
				</Button>
				{#snippet plusIcon()}<Plus size={13} strokeWidth={1.7} />{/snippet}
				{#if tab === 'credentials'}
					<Button icon={plusIcon} variant="primary" onclick={() => (showCreateCred = true)}>
						New credential
					</Button>
				{:else}
					<Button icon={plusIcon} variant="primary" onclick={() => (showCreateAnon = true)}>
						New binding
					</Button>
				{/if}
			{/if}
		{/snippet}
	</PageHeader>

	{#if disabled}
		{#snippet powerIcon()}<PowerOff size={22} strokeWidth={1.7} />{/snippet}
		<EmptyState variant="card" icon={powerIcon} title="The S3 proxy is disabled.">
			<div class="max-w-[640px] text-center">
				The embedded SigV4 proxy is the master switch for virtual credentials and anonymous bucket
				bindings. While it's off, the dashboard can't list, mint, or revoke either — and tenant SDKs
				hitting the proxy endpoint will get connection failures.
			</div>
			<div class="max-w-[640px] text-center">
				Set <code>s3_proxy.enabled: true</code> (and a <code>s3_proxy.listen</code>
				address) in your stowage config, then restart the server. See
				<code>docs/reference/config.md</code> for the full surface.
			</div>
		</EmptyState>
	{:else}
		<div class="flex flex-wrap items-center gap-3">
			<Segmented
				value={tab}
				onchange={(v) => (tab = v)}
				options={[
					{ value: 'credentials' as const, label: 'Virtual credentials' },
					{ value: 'anonymous' as const, label: 'Anonymous bindings' }
				]}
			/>
			<SearchField
				bind:value={q}
				placeholder={tab === 'credentials'
					? 'Filter by key, bucket, claim…'
					: 'Filter by backend or bucket…'}
				width="320px"
				size="sm"
			/>
		</div>

		{#if error}
			<Banner variant="err" role="alert">{error}</Banner>
		{/if}

		{#if tab === 'credentials'}
			{#if creds.length === 0 && !error}
				{#snippet keyIcon()}<KeyRound size={22} strokeWidth={1.7} />{/snippet}
				<EmptyState variant="card" icon={keyIcon} title="No virtual credentials yet.">
					<div>
						Mint a credential to hand a tenant SDK its own scoped access. Operator-provisioned
						credentials appear here automatically once <code>BucketClaim</code>s are reconciled.
					</div>
				</EmptyState>
			{:else}
				<DataTable columns={credColumns} rows={filteredCreds} emptyText="No credentials match.">
					{#snippet row(c)}
						<td class="px-3 font-mono text-[12.5px]">{c.access_key}</td>
						<td class="px-3">
							{#if c.source === 'kubernetes'}
								<Tooltip
									text={c.claim_namespace
										? `${c.claim_namespace}/${c.claim_name ?? '?'}`
										: 'BucketClaim Secret'}
								>
									<Badge variant="ok">
										<Cloud size={10} strokeWidth={1.7} /> operator
									</Badge>
								</Tooltip>
							{:else}
								<Badge>
									<ServerCog size={10} strokeWidth={1.7} /> ui
								</Badge>
							{/if}
						</td>
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
						<td class="px-3 text-[12px] text-stw-fg-mute">
							{#if c.source === 'kubernetes'}
								<span class="font-mono text-[11.5px]"
									>{c.claim_namespace ?? '?'}/{c.claim_name ?? '?'}</span
								>
							{:else if c.user_id}
								<span class="font-mono text-[11.5px]">{c.user_id}</span>
							{:else}
								—
							{/if}
						</td>
						<td class="px-3 text-right font-mono text-[11.5px] text-stw-fg-mute">
							{expiresLabel(c.expires_at)}
						</td>
						<td class="px-3 text-right">
							{#if c.source === 'sqlite'}
								<Toggle value={c.enabled} onchange={(v) => toggleEnabled(c, v)} />
							{:else if c.enabled}
								<Badge variant="ok">active</Badge>
							{:else}
								<Badge>disabled</Badge>
							{/if}
						</td>
						<td class="px-3 text-right">
							{#if c.source === 'sqlite'}
								{#snippet trashIcon()}<Trash2 size={13} strokeWidth={1.7} />{/snippet}
								<Tooltip text="Delete">
									<IconButton
										label="Delete"
										size={24}
										icon={trashIcon}
										onclick={() => destroyCred(c)}
									/>
								</Tooltip>
							{:else}
								<span class="text-[11px] text-stw-fg-soft">read-only</span>
							{/if}
						</td>
					{/snippet}
				</DataTable>
			{/if}
		{:else if bindings.length === 0 && !error}
			{#snippet globeIcon()}<Globe size={22} strokeWidth={1.7} />{/snippet}
			<EmptyState variant="card" icon={globeIcon} title="No anonymous bindings.">
				<div>
					Public read-only access for a single bucket can be exposed through the proxy without a
					credential. Operator-managed bindings appear here automatically.
				</div>
			</EmptyState>
		{:else}
			<DataTable columns={anonColumns} rows={filteredAnon} emptyText="No bindings match.">
				{#snippet row(b)}
					<td class="px-3 font-mono text-[12.5px]">{b.backend_id}/{b.bucket}</td>
					<td class="px-3">
						{#if b.source === 'kubernetes'}
							<Badge variant="ok"><Cloud size={10} strokeWidth={1.7} /> operator</Badge>
						{:else}
							<Badge><ServerCog size={10} strokeWidth={1.7} /> ui</Badge>
						{/if}
					</td>
					<td class="px-3"><Badge>{b.mode}</Badge></td>
					<td class="px-3 text-right font-mono text-[12px] text-stw-fg-mute">
						{b.per_source_ip_rps}
					</td>
					<td class="px-3 text-right font-mono text-[11.5px] text-stw-fg-soft">
						{b.created_at ? new Date(b.created_at).toLocaleDateString() : '—'}
					</td>
					<td class="px-3 text-right">
						{#if b.source !== 'kubernetes'}
							{#snippet trashIcon()}<Trash2 size={13} strokeWidth={1.7} />{/snippet}
							<Tooltip text="Remove">
								<IconButton
									label="Remove"
									size={24}
									icon={trashIcon}
									onclick={() => destroyBinding(b)}
								/>
							</Tooltip>
						{:else}
							<span class="text-[11px] text-stw-fg-soft">read-only</span>
						{/if}
					</td>
				{/snippet}
			</DataTable>
		{/if}
	{/if}
</div>

{#if showCreateCred}
	<S3CredentialDrawer
		mode="admin"
		{backends}
		oncreated={() => {
			showCreateCred = false;
			void refresh();
		}}
		onclose={() => (showCreateCred = false)}
	/>
{/if}

{#if showCreateAnon}
	<S3AnonymousDrawer
		{backends}
		oncreated={() => {
			showCreateAnon = false;
			void refresh();
		}}
		onclose={() => (showCreateAnon = false)}
	/>
{/if}
