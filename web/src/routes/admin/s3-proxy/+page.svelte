<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import AdminS3ProxyPage from '$lib/components/screens/AdminS3ProxyPage.svelte';
	import type { PageData } from './$types';
	import type { LayoutData } from '../../$types';

	let { data }: { data: PageData & LayoutData } = $props();
</script>

<AdminS3ProxyPage
	initialCreds={data.credentials}
	initialAnon={data.bindings}
	initialError={data.error}
	initialDisabled={data.disabled}
	backends={data.backends}
/>
