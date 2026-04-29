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
	class="stw-focus"
	style="position:relative;width:32px;height:18px;border:0;border-radius:999px;padding:0;background:{value
		? 'var(--stw-accent-600)'
		: 'var(--stw-n-200)'};transition:background 120ms;cursor:pointer;"
>
	<i
		style="position:absolute;top:2px;left:2px;width:14px;height:14px;border-radius:50%;background:#fff;transform:{value
			? 'translateX(14px)'
			: 'none'};transition:transform 120ms;box-shadow:0 1px 2px rgba(0,0,0,.25);"
	></i>
</button>
