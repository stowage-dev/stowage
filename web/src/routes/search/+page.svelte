<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { Search as SearchIcon, Folder, FileText } from 'lucide-svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import SearchField from '$lib/components/ui/SearchField.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import { api, ApiException } from '$lib/api';
	import { bytes } from '$lib/format';
	import type { SearchResponse } from '$lib/types';

	let q = $state('');
	let result = $state<SearchResponse | null>(null);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let inputEl: HTMLInputElement | null = $state(null);
	let abortCtl: AbortController | null = null;
	let debounce: ReturnType<typeof setTimeout> | null = null;

	function scheduleSearch(): void {
		if (debounce) clearTimeout(debounce);
		const term = q.trim();
		if (term.length < 2) {
			result = null;
			error = null;
			loading = false;
			abortCtl?.abort();
			return;
		}
		debounce = setTimeout(() => void doSearch(term), 200);
	}

	async function doSearch(term: string): Promise<void> {
		abortCtl?.abort();
		abortCtl = new AbortController();
		loading = true;
		error = null;
		try {
			result = await api.search(term, abortCtl.signal);
		} catch (err) {
			if (err instanceof DOMException && err.name === 'AbortError') return;
			error = err instanceof ApiException ? err.message : 'Search failed.';
			result = null;
		} finally {
			loading = false;
		}
	}

	function openBucket(backend: string, bucket: string): void {
		goto(`/b/${encodeURIComponent(backend)}/${encodeURIComponent(bucket)}`);
	}

	function openObject(backend: string, bucket: string, key: string): void {
		const lastSlash = key.lastIndexOf('/');
		if (lastSlash < 0) {
			openBucket(backend, bucket);
			return;
		}
		const prefix = key.slice(0, lastSlash);
		goto(
			`/b/${encodeURIComponent(backend)}/${encodeURIComponent(bucket)}/` +
				prefix.split('/').map(encodeURIComponent).join('/')
		);
	}

	onMount(() => {
		inputEl?.focus();
	});
</script>

<div class="mx-auto flex max-w-[880px] flex-col gap-[18px] stw-page-pad">
	<PageHeader
		title="Search"
		subtitle="Match bucket names and object key prefixes across every configured backend."
	>
		{#snippet icon()}<SearchIcon size={18} strokeWidth={1.7} />{/snippet}
	</PageHeader>

	<SearchField
		bind:value={q}
		bind:ref={inputEl}
		placeholder="Type at least 2 characters…"
		oninput={scheduleSearch}
	/>

	{#if error}
		<Banner variant="err" role="alert">{error}</Banner>
	{:else if loading}
		<EmptyState variant="card" hint="Searching…" />
	{:else if !result || q.trim().length < 2}
		<EmptyState
			variant="card"
			hint="Searches every backend in parallel. Bucket names match anywhere in the name; object keys match by prefix."
		/>
	{:else if result.buckets.length === 0 && result.objects.length === 0}
		<EmptyState variant="card" hint={`No matches for "${result.query}".`} />
	{:else}
		{#if result.truncated}
			<Banner variant="warn">Showing the first hits — refine your query for more.</Banner>
		{/if}

		{#if result.buckets.length > 0}
			<section>
				<h2
					class="m-0 mb-1.5 text-[11px] font-semibold tracking-[0.08em] text-stw-fg-soft uppercase"
				>
					Buckets
				</h2>
				<div class="flex flex-col gap-1">
					{#each result.buckets as b (b.backend_id + '/' + b.bucket)}
						<button
							type="button"
							class="flex cursor-pointer items-center gap-2.5 rounded-lg border border-stw-border bg-stw-bg-panel px-3.5 py-2.5 text-[13px] text-stw-fg focus-ring transition-[background] duration-[120ms] hover:bg-stw-bg-hover"
							onclick={() => openBucket(b.backend_id, b.bucket)}
						>
							<Folder size={14} strokeWidth={1.7} />
							<span class="flex-1 text-left font-mono">{b.bucket}</span>
							<span
								class="rounded border border-stw-border px-2 py-0.5 font-mono text-[11px] text-stw-fg-mute"
							>
								{b.backend_id}
							</span>
						</button>
					{/each}
				</div>
			</section>
		{/if}

		{#if result.objects.length > 0}
			<section>
				<h2
					class="m-0 mb-1.5 text-[11px] font-semibold tracking-[0.08em] text-stw-fg-soft uppercase"
				>
					Objects
				</h2>
				<div class="flex flex-col gap-1">
					{#each result.objects as o (o.backend_id + '/' + o.bucket + '/' + o.key)}
						<button
							type="button"
							class="flex cursor-pointer items-center gap-2.5 rounded-lg border border-stw-border bg-stw-bg-panel px-3.5 py-2.5 text-[13px] text-stw-fg focus-ring transition-[background] duration-[120ms] hover:bg-stw-bg-hover"
							onclick={() => openObject(o.backend_id, o.bucket, o.key)}
						>
							<FileText size={14} strokeWidth={1.7} />
							<span class="min-w-0 flex-1 truncate text-left font-mono" title={o.key}>
								{o.bucket}/{o.key}
							</span>
							<span class="font-mono text-[11.5px] text-stw-fg-mute">
								{bytes(o.size)}
							</span>
							<span
								class="rounded border border-stw-border px-2 py-0.5 font-mono text-[11px] text-stw-fg-mute"
							>
								{o.backend_id}
							</span>
						</button>
					{/each}
				</div>
			</section>
		{/if}
	{/if}
</div>
