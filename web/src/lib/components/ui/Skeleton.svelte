<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	interface Props {
		w?: string | number;
		h?: string | number;
		style?: string;
	}

	let { w = '100%', h = 10, style = '' }: Props = $props();

	const wStr = $derived(typeof w === 'number' ? w + 'px' : w);
	const hStr = $derived(typeof h === 'number' ? h + 'px' : h);
</script>

<span class="stw-skeleton" style="display:inline-block;width:{wStr};height:{hStr};{style}"></span>
