<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	interface Props {
		value: boolean;
		onchange: (v: boolean) => void;
	}

	let { value, onchange }: Props = $props();
</script>

<button
	type="button"
	role="switch"
	aria-checked={value}
	aria-label="Toggle"
	onclick={() => onchange(!value)}
	class="relative h-[18px] w-8 cursor-pointer rounded-full border-0 p-0 focus-ring transition-colors duration-[120ms] {value
		? 'bg-stw-accent-600'
		: 'bg-stw-n-200'}"
>
	<i
		class="absolute top-0.5 left-0.5 h-3.5 w-3.5 rounded-full bg-white shadow-[0_1px_2px_rgba(0,0,0,0.25)] transition-transform duration-[120ms] {value
			? 'translate-x-3.5'
			: ''}"
	></i>
</button>
