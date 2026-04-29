<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { page } from '$app/state';
	import BackendBuckets from '$lib/components/screens/BackendBuckets.svelte';
	import { bucketState } from '$lib/stores/buckets.svelte';

	const backend = $derived(page.data.backends.find((b) => b.id === page.params.backend));
	const bucketsResult = $derived(backend ? bucketState(backend.id) : null);
</script>

{#if backend && bucketsResult}
	<BackendBuckets {backend} {bucketsResult} />
{:else}
	<div style="padding:40px;color:var(--stw-fg-soft);font-size:13px;">
		Backend not found: <code>{page.params.backend}</code>
	</div>
{/if}
