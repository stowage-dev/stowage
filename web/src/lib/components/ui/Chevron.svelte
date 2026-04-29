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

	const rot = $derived({ right: 0, down: 90, up: -90, left: 180 }[dir]);
</script>

<span
	style="display:inline-flex;align-items:center;justify-content:center;transform:rotate({rot}deg);transition:transform 120ms;"
>
	<ChevronRight {size} strokeWidth={1.7} />
</span>
