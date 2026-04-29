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
</script>

<span
	aria-hidden="true"
	style="display:inline-flex;align-items:center;justify-content:center;width:{size}px;height:{size}px;border-radius:4px;background:{info.color};color:#fff;font-size:{Math.round(
		size * 0.58
	)}px;font-weight:600;font-family:var(--stw-font-mono);flex-shrink:0;">{info.letter}</span
>
