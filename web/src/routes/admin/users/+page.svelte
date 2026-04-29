<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import AdminUsersPage from '$lib/components/screens/AdminUsersPage.svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
</script>

<AdminUsersPage users={data.users} error={data.error} />
