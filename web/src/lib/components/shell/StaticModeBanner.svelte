<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { TriangleAlert, X } from 'lucide-svelte';

	interface Props {
		dismissed: boolean;
		ondismiss: () => void;
	}

	let { dismissed, ondismiss }: Props = $props();
</script>

{#if !dismissed}
	<div
		role="alert"
		class="flex items-center gap-2.5 px-3.5 py-2 text-[12.5px] font-medium"
		style="background:color-mix(in oklch, var(--stw-warn) 12%, var(--stw-bg-panel));border-bottom:1px solid color-mix(in oklch, var(--stw-warn) 35%, var(--stw-border));color:color-mix(in oklch, var(--stw-warn) 75%, var(--stw-fg));"
	>
		<TriangleAlert size={14} strokeWidth={1.7} />
		<span>Static admin enabled — disable in production.</span>
		<span class="flex-1"></span>
		<button
			type="button"
			onclick={ondismiss}
			class="stw-focus cursor-pointer rounded border-0 bg-transparent p-1 text-current opacity-70 hover:opacity-100"
			aria-label="Dismiss"
		>
			<X size={14} strokeWidth={1.7} />
		</button>
	</div>
{/if}
