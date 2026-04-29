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
				class="sb-icon-btn stw-focus"
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
					class="sb-row stw-focus"
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
									class="sb-row stw-focus"
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
									class="sb-trail-btn pin active stw-focus"
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
							class="sb-row sb-tree-row stw-focus"
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
								<span class="sb-status-dot ok" aria-label="Healthy"></span>
							{:else if health.state === 'warn'}
								<span class="sb-status-dot warn" aria-label="Degraded"></span>
							{:else}
								<Tooltip text={health.message ?? 'Unhealthy'}>
									<span class="sb-status-dot err" aria-label="Unhealthy"></span>
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
												class="sb-row sb-bucket-row stw-focus"
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
												class="sb-trail-btn pin stw-focus"
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
													class="sb-trail-btn settings stw-focus"
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
										<div class="sb-empty nested">No buckets</div>
									{/if}
								{:else if bs.status === 'error'}
									<div
										role="button"
										tabindex="0"
										class="sb-error nested sb-error-clickable"
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
									<div class="sb-empty nested">Loading…</div>
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
						class="sb-row stw-focus"
						class:active={route.type === 'shares'}
						onclick={() => nav({ type: 'shares' })}
					>
						<span class="sb-row-ico"><Link size={15} strokeWidth={1.7} /></span>
						<span class="sb-row-label">Shares</span>
					</button>
					<button
						type="button"
						class="sb-row stw-focus"
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
							class="sb-row stw-focus"
							class:active={route.type === 'admin-users'}
							onclick={() => nav({ type: 'admin-users' })}
						>
							<span class="sb-row-ico"><Users size={15} strokeWidth={1.7} /></span>
							<span class="sb-row-label">Users</span>
						</button>
						<button
							type="button"
							class="sb-row stw-focus"
							class:active={route.type === 'admin-audit'}
							onclick={() => nav({ type: 'admin-audit' })}
						>
							<span class="sb-row-ico"><Activity size={15} strokeWidth={1.7} /></span>
							<span class="sb-row-label">Audit log</span>
						</button>
						<button
							type="button"
							class="sb-row stw-focus"
							class:active={route.type === 'backends'}
							onclick={() => nav({ type: 'backends' })}
						>
							<span class="sb-row-ico"><Database size={15} strokeWidth={1.7} /></span>
							<span class="sb-row-label">Backends</span>
						</button>
						<button
							type="button"
							class="sb-row stw-focus"
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
			<button type="button" class="sb-icon-btn stw-focus" aria-label="Sign out" onclick={logout}>
				<LogOut size={14} strokeWidth={1.7} />
			</button>
		</Tooltip>
	</div>
</aside>

<style>
	/* ── Sidebar tokens (Variant A · Refined) ─────────────────────────────
	   Scoped to the sidebar root so the design language stays self-contained
	   and doesn't leak into the rest of the app. Light values are the default;
	   the dark override hangs off the existing data-theme attribute. */
	.sb-sidebar {
		--sb-bg: #ffffff;
		--sb-bg-soft: #fafaf9;
		--sb-bg-hover: #f4f4f3;
		--sb-bg-active: #f0efed;

		--sb-border: #e7e5e0;
		--sb-border-strong: #d9d6cf;

		--sb-fg: #1c1b1a;
		--sb-fg-muted: #6b6862;
		--sb-fg-faint: #9a958c;

		--sb-accent: #c96442;
		--sb-accent-soft: rgba(201, 100, 66, 0.1);

		--sb-ok: #4a8f5a;
		--sb-warn: #c4923a;
		--sb-err: #c0524a;
	}

	:global(:root[data-theme='dark']) .sb-sidebar {
		--sb-bg: #131313;
		--sb-bg-soft: #181817;
		--sb-bg-hover: #1f1f1e;
		--sb-bg-active: #262624;

		--sb-border: #262624;
		--sb-border-strong: #34332f;

		--sb-fg: #ececea;
		--sb-fg-muted: #8b8780;
		--sb-fg-faint: #5d5a55;

		--sb-accent: #e08267;
		--sb-accent-soft: rgba(224, 130, 103, 0.14);

		--sb-ok: #6fb27e;
		--sb-warn: #d6a85c;
		--sb-err: #d77468;
	}

	/* ── Shell ────────────────────────────────────────────────────────── */
	.sb-sidebar {
		width: 248px;
		flex-shrink: 0;
		height: 100%;
		display: flex;
		flex-direction: column;
		background: var(--sb-bg);
		color: var(--sb-fg);
		font-size: 13px;
		box-shadow: inset -1px 0 0 var(--sb-border);
		-webkit-font-smoothing: antialiased;
	}

	/* ── Header ───────────────────────────────────────────────────────── */
	.sb-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 14px 14px 12px;
		border-bottom: 1px solid var(--sb-border);
	}
	.sb-brand {
		display: flex;
		align-items: center;
		gap: 9px;
	}
	.sb-brand-logo {
		width: 22px;
		height: 22px;
		border-radius: 5px;
		background: #1c1b1a;
		color: #fff;
		font-weight: 700;
		font-size: 12px;
		letter-spacing: -0.02em;
		display: flex;
		align-items: center;
		justify-content: center;
		font-family: var(--stw-font-mono);
	}
	:global(:root[data-theme='dark']) .sb-brand-logo {
		background: #ececea;
		color: #131313;
	}
	.sb-brand-name {
		font-weight: 600;
		font-size: 13.5px;
		letter-spacing: -0.01em;
	}

	/* ── Body ─────────────────────────────────────────────────────────── */
	.sb-body {
		flex: 1;
		overflow-y: auto;
		padding: 6px 8px 8px;
		display: flex;
		flex-direction: column;
	}

	/* ── Row primitive ────────────────────────────────────────────────── */
	.sb-row {
		display: flex;
		align-items: center;
		gap: 9px;
		height: 30px;
		padding: 0 8px;
		border: 0;
		border-radius: 6px;
		background: transparent;
		color: var(--sb-fg);
		cursor: pointer;
		text-align: left;
		width: 100%;
		font: inherit;
		transition:
			background 120ms ease,
			color 120ms ease;
	}
	.sb-row:hover:not(.active) {
		background: var(--sb-bg-hover);
	}
	.sb-row.active {
		background: var(--sb-bg-active);
	}
	.sb-row-ico {
		width: 15px;
		height: 15px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		color: var(--sb-fg-muted);
		flex-shrink: 0;
	}
	.sb-row.active .sb-row-ico {
		color: var(--sb-fg);
	}
	.sb-row-label {
		font-size: 13px;
		font-weight: 450;
		letter-spacing: -0.005em;
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.sb-row.active .sb-row-label {
		font-weight: 550;
	}
	.sb-pinned-ico {
		color: var(--sb-accent);
	}

	/* ── Section header ───────────────────────────────────────────────── */
	.sb-sec {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 14px 8px 4px 8px;
		border: 0;
		background: transparent;
		cursor: pointer;
		font: inherit;
		text-align: left;
		width: 100%;
		color: inherit;
	}
	.sb-sec-left {
		display: flex;
		align-items: center;
		gap: 4px;
	}
	.sb-sidebar :global(.sb-sec-chev) {
		width: 12px;
		height: 12px;
		color: var(--sb-fg-faint);
		transition: transform 150ms ease;
		transform: rotate(90deg);
	}
	.sb-sec.collapsed :global(.sb-sec-chev) {
		transform: rotate(0deg);
	}
	.sb-sec-title {
		font-size: 11px;
		font-weight: 600;
		letter-spacing: 0.02em;
		color: var(--sb-fg-muted);
		text-transform: none;
	}
	/* ── Indent rail ──────────────────────────────────────────────────── */
	.sb-sec-body {
		display: flex;
		flex-direction: column;
		gap: 1px;
		padding: 2px 0 0;
		margin-left: 14px;
		padding-left: 10px;
		border-left: 1px solid var(--sb-border);
	}

	.sb-empty {
		padding: 6px 8px;
		font-size: 12px;
		color: var(--sb-fg-faint);
	}
	.sb-empty.nested {
		padding-left: 14px;
	}
	.sb-error {
		padding: 6px 8px;
		font-size: 12px;
		color: var(--sb-err);
		font-family: var(--stw-font-mono);
		word-break: break-word;
	}
	.sb-error.nested {
		padding-left: 14px;
	}
	.sb-error-clickable {
		cursor: pointer;
	}
	.sb-error-clickable:hover {
		background: var(--sb-bg-hover);
		border-radius: 4px;
	}

	/* ── Backend tree row ─────────────────────────────────────────────── */
	.sb-tree-toggle {
		width: 14px;
		height: 14px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		color: var(--sb-fg-faint);
		flex-shrink: 0;
		transition: transform 150ms ease;
		transform: rotate(90deg);
	}
	.sb-tree-row.collapsed .sb-tree-toggle {
		transform: rotate(0deg);
	}
	.sb-backend-mark {
		width: 18px;
		height: 18px;
		border-radius: 4px;
		background: var(--sb-accent-soft);
		color: var(--sb-accent);
		font-size: 10px;
		font-weight: 700;
		font-family: var(--stw-font-mono);
		display: inline-flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
	}
	.sb-status-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		flex-shrink: 0;
		margin-left: auto;
	}
	.sb-status-dot.ok {
		background: var(--sb-ok);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--sb-ok) 18%, transparent);
	}
	.sb-status-dot.warn {
		background: var(--sb-warn);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--sb-warn) 18%, transparent);
	}
	.sb-status-dot.err {
		background: var(--sb-err);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--sb-err) 18%, transparent);
	}

	/* ── Bucket rail (second level) ───────────────────────────────────── */
	.sb-tree-children {
		position: relative;
		margin-left: 17px;
		padding-left: 8px;
		border-left: 1px solid var(--sb-border);
		display: flex;
		flex-direction: column;
		gap: 1px;
	}
	.sb-bucket-row {
		padding-left: 8px;
	}
	.sb-bucket-row .sb-row-ico {
		color: var(--sb-fg-faint);
	}
	.sb-bucket-row.active .sb-row-ico {
		color: var(--sb-fg);
	}

	/* ── Trailing buttons (pin, settings) ─────────────────────────────── */
	.sb-row-wrap {
		position: relative;
	}
	.sb-trail-btn {
		position: absolute;
		top: 50%;
		transform: translateY(-50%);
		width: 22px;
		height: 22px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		background: transparent;
		border: 0;
		border-radius: 4px;
		color: var(--sb-fg-faint);
		cursor: pointer;
		opacity: 0;
		transition:
			opacity 120ms,
			background 120ms,
			color 120ms;
	}
	.sb-trail-btn.pin {
		right: 32px;
	}
	.sb-trail-btn.settings {
		right: 8px;
	}
	.sb-trail-btn.pin.active {
		right: 8px;
		opacity: 1;
		color: var(--sb-accent);
	}
	.sb-row-wrap:hover .sb-trail-btn,
	.sb-row-wrap:focus-within .sb-trail-btn {
		opacity: 1;
	}
	.sb-trail-btn:hover {
		background: var(--sb-bg-hover);
		color: var(--sb-fg);
	}

	/* ── Icon buttons (theme toggle, sign out) ────────────────────────── */
	.sb-icon-btn {
		width: 24px;
		height: 24px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		background: transparent;
		border: 0;
		border-radius: 5px;
		cursor: pointer;
		color: var(--sb-fg-muted);
		transition:
			background 120ms,
			color 120ms;
	}
	.sb-icon-btn:hover {
		background: var(--sb-bg-hover);
		color: var(--sb-fg);
	}

	/* ── Footer ───────────────────────────────────────────────────────── */
	.sb-foot {
		border-top: 1px solid var(--sb-border);
		padding: 8px;
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.sb-avatar {
		width: 26px;
		height: 26px;
		border-radius: 6px;
		background: linear-gradient(135deg, var(--sb-accent), #8a3f29);
		color: #fff;
		font-weight: 600;
		font-size: 11px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
	}
	.sb-who {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-width: 0;
	}
	.sb-who-name {
		font-size: 12.5px;
		font-weight: 550;
		color: var(--sb-fg);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.sb-who-sub {
		font-size: 11px;
		color: var(--sb-fg-muted);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
</style>
