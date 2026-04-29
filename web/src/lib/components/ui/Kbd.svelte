<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import type { Snippet } from 'svelte';

	let { children }: { children?: Snippet } = $props();
</script>

<span class="stw-kbd"
	>{#if children}{@render children()}{/if}</span
>
