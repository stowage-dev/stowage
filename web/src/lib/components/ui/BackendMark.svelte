<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { BACKEND_KINDS } from '$lib/backend-kind';
	import type { BackendKind } from '$lib/types';

	interface Props {
		kind: BackendKind;
		size?: number;
	}

	let { kind, size = 18 }: Props = $props();

	const info = $derived(BACKEND_KINDS[kind] ?? BACKEND_KINDS.generic);
	const fontPx = $derived(Math.round(size * 0.58));
</script>

<span
	aria-hidden="true"
	class="inline-flex shrink-0 items-center justify-center rounded-stw-sm font-stw-mono font-semibold text-white"
	style="width:{size}px;height:{size}px;background:{info.color};font-size:{fontPx}px;"
>
	{info.letter}
</span>
