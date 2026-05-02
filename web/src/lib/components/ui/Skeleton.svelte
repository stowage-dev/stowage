<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	interface Props {
		w?: string | number;
		h?: string | number;
		class?: string;
	}

	let { w = '100%', h = 10, class: klass = '' }: Props = $props();

	const wStr = $derived(typeof w === 'number' ? w + 'px' : w);
	const hStr = $derived(typeof h === 'number' ? h + 'px' : h);
</script>

<span class="inline-block stw-skeleton {klass}" style="width:{wStr};height:{hStr};"></span>
