<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import AuditPage from '$lib/components/screens/AuditPage.svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
</script>

<AuditPage initialEvents={data.events} initialTotal={data.total} initialError={data.error} />
