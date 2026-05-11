<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import '../app.css';
	import { onMount } from 'svelte';
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import { Toaster, toast } from 'svelte-sonner';

	import Sidebar from '$lib/components/shell/Sidebar.svelte';
	import TopBar from '$lib/components/shell/TopBar.svelte';
	import StaticModeBanner from '$lib/components/shell/StaticModeBanner.svelte';
	import ShareModal from '$lib/components/share/ShareModal.svelte';
	import CommandPalette from '$lib/components/command/CommandPalette.svelte';
	import UploadQueue from '$lib/components/upload/UploadQueue.svelte';

	import { theme, setTheme } from '$lib/stores/theme.svelte';
	import { primeBuckets, reconcileBackends } from '$lib/stores/buckets.svelte';
	import { routeFromPage, urlForRoute } from '$lib/route';
	import {
		overlay,
		openPalette,
		closePalette,
		closeShare,
		dismissBanner,
		toggleSidebar,
		closeSidebar
	} from '$lib/stores/shell.svelte';
	import { api, ApiException } from '$lib/api';
	import type { Crumb } from '$lib/components/shell/Breadcrumb.svelte';
	import type { LayoutData } from './$types';
	import type { Snippet } from 'svelte';

	let { children, data }: { children: Snippet; data: LayoutData } = $props();

	const route = $derived(routeFromPage(page));
	const isLogin = $derived(page.url.pathname === '/login');
	// Public share pages render bare-chrome — no sidebar, topbar, or app
	// shell. Authenticated admins clicking their own share link still see
	// the recipient view.
	const isSharePage = $derived(page.url.pathname.startsWith('/s/'));
	const inShell = $derived(!isLogin && !isSharePage && !!data.me);

	const showStaticBanner = $derived(
		(data.authConfig?.modes.includes('static') ?? false) && route.type !== 'backends'
	);

	const crumbs = $derived.by<Crumb[]>(() => {
		const out: Crumb[] = [{ label: 'Stowage', href: urlForRoute({ type: 'backends' }) }];
		if (route.type === 'backends') out.push({ label: 'Backends' });
		else if (route.type === 'backend') {
			const b = data.backends.find((x) => x.id === route.backend);
			out.push({
				label: b?.name ?? route.backend,
				href: urlForRoute({ type: 'backend', backend: route.backend })
			});
		} else if (route.type === 'bucket') {
			const b = data.backends.find((x) => x.id === route.backend);
			out.push({
				label: b?.name ?? route.backend,
				href: urlForRoute({ type: 'backend', backend: route.backend })
			});
			out.push({
				label: route.bucket,
				href: urlForRoute({
					type: 'bucket',
					backend: route.backend,
					bucket: route.bucket,
					prefix: []
				})
			});
			route.prefix.forEach((p, i) =>
				out.push({
					label: p,
					mono: true,
					href: urlForRoute({
						type: 'bucket',
						backend: route.backend,
						bucket: route.bucket,
						prefix: route.prefix.slice(0, i + 1)
					})
				})
			);
		} else if (route.type === 'shares') out.push({ label: 'My shares' });
		else if (route.type === 'admin-users') {
			out.push({ label: 'Admin' });
			out.push({ label: 'Users' });
		} else if (route.type === 'admin-audit') {
			out.push({ label: 'Admin' });
			out.push({ label: 'Audit log' });
		} else if (route.type === 'admin-dashboard') {
			out.push({ label: 'Admin' });
			out.push({ label: 'Dashboard' });
		} else if (route.type === 'admin-health') {
			out.push({ label: 'Admin' });
			out.push({ label: 'Health' });
		} else if (route.type === 'admin-s3-proxy') {
			out.push({ label: 'Admin' });
			out.push({ label: 'S3 Proxy' });
		} else if (route.type === 'me-s3-credentials') {
			out.push({ label: 'My credentials' });
		} else if (route.type === 'search') {
			out.push({ label: 'Search' });
		}
		return out;
	});

	function navigateCrumb(c: Crumb): void {
		if (c.href) goto(c.href);
	}

	async function logout() {
		try {
			await api.logout();
		} catch (err) {
			if (err instanceof ApiException) toast.error(err.message);
		}
		await invalidateAll();
		goto('/login', { replaceState: true });
	}

	$effect(() => {
		if (typeof document !== 'undefined') {
			document.documentElement.dataset.theme = theme.value;
		}
	});

	// Close the mobile sidebar whenever the route changes.
	$effect(() => {
		void page.url.pathname;
		closeSidebar();
	});

	// Keep the bucket store in sync with the live backend list. `primeBuckets`
	// is idempotent — it only kicks fetches for backends that don't already
	// have a successful result, so this is safe to re-run on every navigation.
	$effect(() => {
		const backends = data.backends ?? [];
		reconcileBackends(backends.map((b) => b.id));
		primeBuckets(backends);
	});

	onMount(() => {
		setTheme(theme.value);

		const onKey = (e: KeyboardEvent): void => {
			const t = e.target as HTMLElement;
			if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA')) return;
			if (e.key === '/') {
				openPalette();
				e.preventDefault();
			} else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
				openPalette();
				e.preventDefault();
			} else if (e.key === 'g') {
				const next = (e2: KeyboardEvent): void => {
					if (e2.key === 'u') goto(urlForRoute({ type: 'admin-users' }));
					if (e2.key === 's') goto(urlForRoute({ type: 'shares' }));
					if (e2.key === 'b') goto(urlForRoute({ type: 'backends' }));
					window.removeEventListener('keydown', next);
				};
				window.addEventListener('keydown', next, { once: true });
			}
		};
		window.addEventListener('keydown', onKey);
		return () => window.removeEventListener('keydown', onKey);
	});
</script>

{#if !inShell}
	{@render children()}
{:else}
	<div class="flex h-screen flex-col overflow-hidden">
		{#if showStaticBanner}
			<StaticModeBanner dismissed={overlay.bannerDismissed} ondismiss={dismissBanner} />
		{/if}

		<div class="flex min-h-0 flex-1">
			<div class="stw-sidebar-slot" class:open={overlay.sidebarOpen}>
				<Sidebar {route} backends={data.backends} pins={data.pins} me={data.me} {logout} />
			</div>
			{#if overlay.sidebarOpen}
				<button
					type="button"
					class="stw-sidebar-backdrop"
					aria-label="Close menu"
					onclick={closeSidebar}
				></button>
			{/if}
			<main class="relative flex min-h-0 min-w-0 flex-1 flex-col">
				<TopBar {crumbs} oncmdk={openPalette} onnavigate={navigateCrumb} onmenu={toggleSidebar} />
				<div class="relative stw-scroll min-h-0 flex-1 overflow-auto">
					{@render children()}
				</div>
				<UploadQueue />
			</main>
		</div>

		{#if overlay.share}
			<ShareModal
				item={overlay.share.item}
				backend={overlay.share.backend}
				bucket={overlay.share.bucket}
				prefix={overlay.share.prefix}
				onclose={closeShare}
			/>
		{/if}

		{#if overlay.palette}
			<CommandPalette backends={data.backends} onclose={closePalette} />
		{/if}
	</div>
{/if}

<Toaster richColors position="bottom-center" />
