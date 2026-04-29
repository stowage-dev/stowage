<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	type Variant = 'ok' | 'warn' | 'err' | 'muted';

	interface Props {
		variant?: Variant;
		label?: string;
	}

	let { variant = 'muted', label }: Props = $props();
</script>

<span class="stw-dot stw-dot--{variant}" role="status" aria-label={label}></span>
