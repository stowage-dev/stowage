<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import MyS3CredentialsPage from '$lib/components/screens/MyS3CredentialsPage.svelte';
	import type { PageData } from './$types';
	import type { LayoutData } from '../../$types';

	let { data }: { data: PageData & LayoutData } = $props();
</script>

<MyS3CredentialsPage
	initialCreds={data.credentials}
	initialError={data.error}
	backends={data.backends}
/>
