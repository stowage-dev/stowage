<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { page } from '$app/state';
	import BucketBrowser from '$lib/components/browser/BucketBrowser.svelte';
	import { openShare } from '$lib/stores/shell.svelte';
	import type { BrowserItem } from '$lib/types';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	const backend = $derived(page.data.backends.find((b) => b.id === data.backendId));

	function onshare(it: BrowserItem) {
		if (backend) openShare(it, backend.id, data.bucket, data.prefix);
	}
</script>

{#if backend}
	<BucketBrowser
		{backend}
		bucket={data.bucket}
		prefix={data.prefix}
		listing={data.listing}
		quota={data.quota}
		error={data.error}
		{onshare}
	/>
{:else}
	<div style="padding:40px;color:var(--stw-fg-soft);font-size:13px;">
		Backend not found: <code>{data.backendId}</code>
	</div>
{/if}
