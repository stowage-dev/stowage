<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { goto, invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import {
		Sun,
		Moon,
		Folder,
		Link,
		Users,
		Activity,
		Database,
		Gauge,
		KeyRound,
		ServerCog,
		LogOut,
		Settings,
		Star,
		StarOff,
		ChevronRight
	} from 'lucide-svelte';
	import Tooltip from '$lib/components/ui/Tooltip.svelte';
	import FlatSidebar from './FlatSidebar.svelte';
	import { theme, toggleTheme } from '$lib/stores/theme.svelte';
	import { tweaks } from '$lib/stores/tweaks.svelte';
	import { urlForRoute } from '$lib/route';
	import { inferKind, backendHealth, BACKEND_KINDS } from '$lib/backend-kind';
	import {
		sectionCollapsed,
		toggleSection,
		backendCollapsed,
		toggleBackend,
		expandBackend
	} from '$lib/stores/sidebar.svelte';
	import { api, ApiException } from '$lib/api';
	import { bucketState, refreshBuckets } from '$lib/stores/buckets.svelte';
	import type { Backend, BucketPin, Me, Route } from '$lib/types';

	interface Props {
		route: Route;
		backends: Backend[];
		pins?: BucketPin[];
		me: Me | null;
		logout: () => void;
	}

	let { route, backends, pins = [], me, logout }: Props = $props();

	const pinKey = (b: string, k: string): string => b + '/' + k;
	const pinSet = $derived(new Set(pins.map((p) => pinKey(p.backend_id, p.bucket))));

	function isPinned(backendId: string, bucket: string): boolean {
		return pinSet.has(pinKey(backendId, bucket));
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

	function isOnBackend(id: string): boolean {
		return route.type === 'backend' && route.backend === id;
	}
	function isOnBucket(backend: string, bucket: string): boolean {
		return route.type === 'bucket' && route.backend === backend && route.bucket === bucket;
	}

	function nav(r: Route): void {
		goto(urlForRoute(r));
	}

	const initials = $derived((me?.username ?? '?').slice(0, 2).toUpperCase());
	const isAdmin = $derived(me?.role === 'admin');

	// Sidebar reads each backend's bucket list from the reactive store so a
	// single slow/dead backend stays scoped to its own row instead of blocking
	// the whole sidebar.

	// Auto-expand the backend that contains the active route so the active
	// bucket row stays visible after a hard reload.
	$effect(() => {
		if (route.type === 'bucket') expandBackend(route.backend);
	});
</script>

{#snippet sectionHeader(name: 'pinned' | 'backends' | 'workspace' | 'admin', label: string)}
	{@const collapsed = sectionCollapsed[name]}
	<div
		role="button"
		tabindex="0"
		class="sb-sec"
		class:collapsed
		aria-expanded={!collapsed}
		onclick={() => toggleSection(name)}
		onkeydown={(e) => {
			if (e.key === 'Enter' || e.key === ' ') {
				e.preventDefault();
				toggleSection(name);
			}
		}}
	>
		<span class="sb-sec-left">
			<ChevronRight class="sb-sec-chev" size={12} strokeWidth={2} />
			<span class="sb-sec-title">{label}</span>
		</span>
	</div>
{/snippet}

<aside class="sb-sidebar">
	<!-- Header -->
	<div class="sb-head">
		<span class="sb-brand">
			<span class="sb-brand-logo" aria-hidden="true">s</span>
			<span class="sb-brand-name">stowage</span>
		</span>
		<Tooltip text={theme.value === 'dark' ? 'Switch to light' : 'Switch to dark'}>
			<button
				type="button"
				onclick={toggleTheme}
				class="sb-icon-btn focus-ring"
				aria-label="Toggle theme"
			>
				{#if theme.value === 'dark'}
					<Sun size={14} strokeWidth={1.7} />
				{:else}
					<Moon size={14} strokeWidth={1.7} />
				{/if}
			</button>
		</Tooltip>
	</div>

	<!-- Body -->
	<div class="sb-body stw-scroll">
		{#if tweaks.sidebarStyle === 'flat'}
			<FlatSidebar {route} {backends} {pins} {nav} />
		{:else}
			{#if isAdmin}
				<button
					type="button"
					class="sb-row focus-ring"
					class:active={route.type === 'admin-dashboard'}
					onclick={() => nav({ type: 'admin-dashboard' })}
				>
					<span class="sb-row-ico"><Gauge size={15} strokeWidth={1.7} /></span>
					<span class="sb-row-label">Dashboard</span>
				</button>
			{/if}

			{#if pins.length > 0}
				{@render sectionHeader('pinned', 'Pinned')}
				{#if !sectionCollapsed.pinned}
					<div class="sb-sec-body">
						{#each pins as pin (pin.backend_id + '/' + pin.bucket)}
							{@const active = isOnBucket(pin.backend_id, pin.bucket)}
							<div class="sb-row-wrap">
								<button
									type="button"
									class="sb-row focus-ring"
									class:active
									onclick={() =>
										nav({
											type: 'bucket',
											backend: pin.backend_id,
											bucket: pin.bucket,
											prefix: []
										})}
								>
									<span class="sb-row-ico sb-pinned-ico">
										<Star size={12} strokeWidth={1.7} fill="currentColor" />
									</span>
									<span class="sb-row-label">{pin.bucket}</span>
								</button>
								<button
									type="button"
									class="pin active sb-trail-btn focus-ring"
									aria-label="Unpin bucket"
									title="Unpin bucket"
									onclick={(e) => {
										e.stopPropagation();
										void togglePin(pin.backend_id, pin.bucket);
									}}
								>
									<StarOff size={12} strokeWidth={1.7} />
								</button>
							</div>
						{/each}
					</div>
				{/if}
			{/if}

			{@render sectionHeader('backends', 'Backends')}
			{#if !sectionCollapsed.backends}
				<div class="sb-sec-body">
					{#if backends.length === 0}
						<div class="sb-empty">No backends configured.</div>
					{/if}
					{#each backends as b (b.id)}
						{@const open = !backendCollapsed[b.id]}
						{@const kind = inferKind(b)}
						{@const kindInfo = BACKEND_KINDS[kind] ?? BACKEND_KINDS.generic}
						{@const health = backendHealth(b)}
						<button
							type="button"
							class="sb-tree-row sb-row focus-ring"
							class:active={isOnBackend(b.id)}
							class:collapsed={!open}
							onclick={() => {
								toggleBackend(b.id);
								nav({ type: 'backend', backend: b.id });
							}}
						>
							<span class="sb-tree-toggle">
								<ChevronRight size={12} strokeWidth={2} />
							</span>
							<span class="sb-backend-mark" aria-hidden="true">{kindInfo.letter}</span>
							<span class="sb-row-label">{b.name}</span>
							{#if health.state === 'ok'}
								<span class="ok sb-status-dot" aria-label="Healthy"></span>
							{:else if health.state === 'warn'}
								<span class="warn sb-status-dot" aria-label="Degraded"></span>
							{:else}
								<Tooltip text={health.message ?? 'Unhealthy'}>
									<span class="err sb-status-dot" aria-label="Unhealthy"></span>
								</Tooltip>
							{/if}
						</button>
						{#if open}
							{@const bs = bucketState(b.id)}
							<div class="sb-tree-children">
								{#if bs.status === 'ok'}
									{#each bs.buckets as bk (bk.name)}
										{@const active = isOnBucket(b.id, bk.name)}
										<div class="sb-row-wrap">
											<button
												type="button"
												class="sb-row sb-bucket-row focus-ring"
												class:active
												onclick={() =>
													nav({
														type: 'bucket',
														backend: b.id,
														bucket: bk.name,
														prefix: []
													})}
											>
												<span class="sb-row-ico"><Folder size={13} strokeWidth={1.7} /></span>
												<span class="sb-row-label">{bk.name}</span>
											</button>
											<button
												type="button"
												class="pin sb-trail-btn focus-ring"
												class:active={isPinned(b.id, bk.name)}
												aria-label={isPinned(b.id, bk.name) ? 'Unpin bucket' : 'Pin bucket'}
												title={isPinned(b.id, bk.name) ? 'Unpin bucket' : 'Pin bucket'}
												onclick={(e) => {
													e.stopPropagation();
													void togglePin(b.id, bk.name);
												}}
											>
												{#if isPinned(b.id, bk.name)}
													<Star size={12} strokeWidth={1.7} fill="currentColor" />
												{:else}
													<Star size={12} strokeWidth={1.7} />
												{/if}
											</button>
											{#if isAdmin}
												<button
													type="button"
													class="settings sb-trail-btn focus-ring"
													aria-label="Bucket settings"
													title="Bucket settings"
													onclick={(e) => {
														e.stopPropagation();
														goto(
															`/b/${encodeURIComponent(b.id)}/${encodeURIComponent(bk.name)}/settings`
														);
													}}
												>
													<Settings size={12} strokeWidth={1.7} />
												</button>
											{/if}
										</div>
									{/each}
									{#if bs.buckets.length === 0}
										<div class="nested sb-empty">No buckets</div>
									{/if}
								{:else if bs.status === 'error'}
									<div
										role="button"
										tabindex="0"
										class="nested clickable sb-error"
										title="Click to retry · {bs.message}"
										onclick={(e) => {
											e.stopPropagation();
											refreshBuckets(b.id);
										}}
										onkeydown={(e) => {
											if (e.key === 'Enter' || e.key === ' ') {
												e.preventDefault();
												refreshBuckets(b.id);
											}
										}}
									>
										{bs.message}
									</div>
								{:else}
									<div class="nested sb-empty">Loading…</div>
								{/if}
							</div>
						{/if}
					{/each}
				</div>
			{/if}

			{@render sectionHeader('workspace', 'Workspace')}
			{#if !sectionCollapsed.workspace}
				<div class="sb-sec-body">
					<button
						type="button"
						class="sb-row focus-ring"
						class:active={route.type === 'shares'}
						onclick={() => nav({ type: 'shares' })}
					>
						<span class="sb-row-ico"><Link size={15} strokeWidth={1.7} /></span>
						<span class="sb-row-label">Shares</span>
					</button>
					<button
						type="button"
						class="sb-row focus-ring"
						class:active={route.type === 'me-s3-credentials'}
						onclick={() => nav({ type: 'me-s3-credentials' })}
					>
						<span class="sb-row-ico"><KeyRound size={15} strokeWidth={1.7} /></span>
						<span class="sb-row-label">My credentials</span>
					</button>
				</div>
			{/if}

			{#if isAdmin}
				{@render sectionHeader('admin', 'Admin')}
				{#if !sectionCollapsed.admin}
					<div class="sb-sec-body">
						<button
							type="button"
							class="sb-row focus-ring"
							class:active={route.type === 'admin-users'}
							onclick={() => nav({ type: 'admin-users' })}
						>
							<span class="sb-row-ico"><Users size={15} strokeWidth={1.7} /></span>
							<span class="sb-row-label">Users</span>
						</button>
						<button
							type="button"
							class="sb-row focus-ring"
							class:active={route.type === 'admin-audit'}
							onclick={() => nav({ type: 'admin-audit' })}
						>
							<span class="sb-row-ico"><Activity size={15} strokeWidth={1.7} /></span>
							<span class="sb-row-label">Audit log</span>
						</button>
						<button
							type="button"
							class="sb-row focus-ring"
							class:active={route.type === 'backends'}
							onclick={() => nav({ type: 'backends' })}
						>
							<span class="sb-row-ico"><Database size={15} strokeWidth={1.7} /></span>
							<span class="sb-row-label">Backends</span>
						</button>
						<button
							type="button"
							class="sb-row focus-ring"
							class:active={route.type === 'admin-s3-proxy'}
							onclick={() => nav({ type: 'admin-s3-proxy' })}
						>
							<span class="sb-row-ico"><ServerCog size={15} strokeWidth={1.7} /></span>
							<span class="sb-row-label">S3 Proxy</span>
						</button>
					</div>
				{/if}
			{/if}
		{/if}
	</div>

	<!-- Footer -->
	<div class="sb-foot">
		<span class="sb-avatar" aria-hidden="true">{initials}</span>
		<div class="sb-who">
			<div class="sb-who-name">{me?.username ?? '—'}</div>
			<div class="sb-who-sub">{me?.identity_source ?? ''}</div>
		</div>
		<Tooltip text="Sign out">
			<button type="button" class="sb-icon-btn focus-ring" aria-label="Sign out" onclick={logout}>
				<LogOut size={14} strokeWidth={1.7} />
			</button>
		</Tooltip>
	</div>
</aside>
