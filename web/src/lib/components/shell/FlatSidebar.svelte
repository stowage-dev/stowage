<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import {
		Folder,
		Plus,
		Link as LinkIcon,
		Users,
		Star,
		StarOff,
		Search as SearchIcon,
		Activity
	} from 'lucide-svelte';
	import Tooltip from '$lib/components/ui/Tooltip.svelte';
	import Dot from '$lib/components/ui/Dot.svelte';
	import Chevron from '$lib/components/ui/Chevron.svelte';
	import BackendMark from '$lib/components/ui/BackendMark.svelte';
	import { inferKind, backendHealth } from '$lib/backend-kind';
	import { api, ApiException } from '$lib/api';
	import { bucketState, refreshBuckets } from '$lib/stores/buckets.svelte';
	import type { Backend, Bucket, BucketPin, Route } from '$lib/types';

	interface Props {
		route: Route;
		backends: Backend[];
		pins?: BucketPin[];
		nav: (r: Route) => void;
	}

	let { route, backends, pins = [], nav }: Props = $props();

	const pinSet = $derived(new Set(pins.map((p) => p.backend_id + '/' + p.bucket)));

	function isPinned(backendId: string, bucket: string): boolean {
		return pinSet.has(backendId + '/' + bucket);
	}

	async function togglePin(backendId: string, bucket: string): Promise<void> {
		try {
			if (isPinned(backendId, bucket)) {
				await api.unpinBucket(backendId, bucket);
			} else {
				await api.pinBucket(backendId, bucket);
			}
			await invalidateAll();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Pin update failed.');
		}
	}

	const currentBackendId = $derived(
		'backend' in route && route.backend ? route.backend : backends[0]?.id
	);
	const b = $derived(backends.find((x) => x.id === currentBackendId) ?? backends[0]);
	const bs = $derived(b ? bucketState(b.id) : null);
	const list = $derived.by<Bucket[]>(() => (bs?.status === 'ok' ? bs.buckets : []));
	const kind = $derived(b ? inferKind(b) : 'generic');
	const health = $derived(b ? backendHealth(b) : { state: 'warn' as const });

	const navRowCls =
		'stw-focus flex h-[28px] w-full cursor-pointer items-center gap-2 rounded-[5px] border-0 bg-transparent px-2 text-left text-[13px]';
	const sectionLabelCls =
		'text-[10.5px] font-semibold tracking-[0.08em] uppercase text-[var(--stw-fg-soft)]';
</script>

{#snippet bucketRow(
	active: boolean,
	pinned: boolean,
	onclick: () => void,
	ontogglePin: () => void,
	label: string,
	leadingIcon: import('svelte').Snippet,
	trailing?: import('svelte').Snippet
)}
	<div class="group relative">
		<button
			type="button"
			{onclick}
			class="{navRowCls} mx-1 w-[calc(100%-8px)] {active
				? 'bg-[var(--stw-bg-hover)] text-[var(--stw-fg)]'
				: 'text-[var(--stw-fg-mute)]'}"
		>
			{@render leadingIcon()}
			<span class="min-w-0 flex-1 truncate">{label}</span>
			{#if trailing}{@render trailing()}{/if}
		</button>
		<button
			type="button"
			class="stw-focus absolute top-1/2 right-2 inline-flex h-[22px] w-[22px] -translate-y-1/2 cursor-pointer items-center justify-center rounded border-0 bg-transparent transition-opacity duration-[120ms] hover:bg-[var(--stw-bg-sunken)] hover:text-[var(--stw-fg)] {pinned
				? 'text-[var(--stw-warn)] opacity-100'
				: 'text-[var(--stw-fg-soft)] opacity-0 group-focus-within:opacity-100 group-hover:opacity-100'}"
			aria-label={pinned ? 'Unpin bucket' : 'Pin bucket'}
			title={pinned ? 'Unpin bucket' : 'Pin bucket'}
			onclick={(e) => {
				e.stopPropagation();
				ontogglePin();
			}}
		>
			{#if pinned}
				<StarOff size={12} strokeWidth={1.7} />
			{:else}
				<Star size={12} strokeWidth={1.7} />
			{/if}
		</button>
	</div>
{/snippet}

{#if b}
	<div class="px-3.5 pt-2.5 pb-1">
		<div class="{sectionLabelCls} mb-1.5">Backend</div>
		<div
			class="flex h-[36px] cursor-pointer items-center gap-2 rounded-[7px] border border-[var(--stw-border)] bg-[var(--stw-bg-sunken)] px-2.5"
		>
			<BackendMark {kind} size={18} />
			<div class="min-w-0 flex-1">
				<div class="truncate text-[13px] font-medium">{b.name}</div>
			</div>
			<Dot variant={health.state} />
			<Chevron dir="down" size={12} />
		</div>
	</div>

	<div class="flex items-center justify-between px-2 pt-2.5 pb-1 pl-3.5">
		<span class={sectionLabelCls}>Buckets</span>
		<Tooltip text="Create bucket">
			<span class="inline-flex cursor-pointer text-[var(--stw-fg-soft)]">
				<Plus size={12} strokeWidth={1.7} />
			</span>
		</Tooltip>
	</div>

	{#if bs?.status === 'loading'}
		<div class="px-3.5 py-1.5 text-[12px] text-[var(--stw-fg-soft)]">Loading…</div>
	{:else if bs?.status === 'error'}
		<div
			role="button"
			tabindex="0"
			class="cursor-pointer px-3.5 py-1.5 font-mono text-[12px] break-words text-[var(--stw-err)]"
			title="Click to retry · {bs.message}"
			onclick={() => refreshBuckets(b.id)}
			onkeydown={(e) => {
				if (e.key === 'Enter' || e.key === ' ') {
					e.preventDefault();
					refreshBuckets(b.id);
				}
			}}
		>
			{bs.message}
		</div>
	{/if}
	{#each list as bk (bk.name)}
		{@const active = route.type === 'bucket' && route.bucket === bk.name}
		{#snippet folderIcon()}<Folder size={13} strokeWidth={1.7} />{/snippet}
		{@render bucketRow(
			active,
			isPinned(b.id, bk.name),
			() => nav({ type: 'bucket', backend: b.id, bucket: bk.name, prefix: [] }),
			() => void togglePin(b.id, bk.name),
			bk.name,
			folderIcon
		)}
	{/each}

	{#if pins.length > 0}
		<div class="h-[14px]"></div>
		<div class="{sectionLabelCls} px-3.5 pt-1.5 pb-1">Pinned</div>
		{#each pins as pin (pin.backend_id + '/' + pin.bucket)}
			{@const active =
				route.type === 'bucket' && route.backend === pin.backend_id && route.bucket === pin.bucket}
			{#snippet starIcon()}<Star size={12} strokeWidth={1.7} fill="currentColor" />{/snippet}
			{#snippet pinBackendLabel()}
				<span class="font-mono text-[10.5px] text-[var(--stw-fg-soft)]">{pin.backend_id}</span>
			{/snippet}
			{@render bucketRow(
				active,
				true,
				() => nav({ type: 'bucket', backend: pin.backend_id, bucket: pin.bucket, prefix: [] }),
				() => void togglePin(pin.backend_id, pin.bucket),
				pin.bucket,
				starIcon,
				pinBackendLabel
			)}
		{/each}
	{/if}

	<div class="h-[14px]"></div>
	<div class="px-2">
		{#each [{ icon: SearchIcon, label: 'Search', route: 'search' as const }, { icon: LinkIcon, label: 'Shares', route: 'shares' as const }, { icon: Activity, label: 'Health', route: 'admin-health' as const }, { icon: Users, label: 'Users', route: 'admin-users' as const }] as item (item.label)}
			{@const active = route.type === item.route}
			<button
				type="button"
				onclick={() => nav({ type: item.route })}
				class="{navRowCls} {active
					? 'bg-[var(--stw-bg-hover)] text-[var(--stw-fg)]'
					: 'text-[var(--stw-fg-mute)]'}"
			>
				<item.icon size={14} strokeWidth={1.7} />{item.label}
			</button>
		{/each}
	</div>
{/if}
