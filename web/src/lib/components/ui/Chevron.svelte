<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { ChevronRight } from 'lucide-svelte';

	type Dir = 'right' | 'down' | 'up' | 'left';

	interface Props {
		size?: number;
		dir?: Dir;
	}

	let { size = 16, dir = 'right' }: Props = $props();

	const rotCls = $derived(
		{ right: 'rotate-0', down: 'rotate-90', up: '-rotate-90', left: 'rotate-180' }[dir]
	);
</script>

<span
	class="inline-flex items-center justify-center transition-transform duration-[120ms] {rotCls}"
>
	<ChevronRight {size} strokeWidth={1.7} />
</span>
