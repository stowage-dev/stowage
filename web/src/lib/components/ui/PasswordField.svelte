<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { Eye, EyeOff, RefreshCw } from 'lucide-svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import type { HTMLInputAttributes } from 'svelte/elements';

	interface Props {
		id?: string;
		value: string;
		placeholder?: string;
		required?: boolean;
		minlength?: number;
		autocomplete?: HTMLInputAttributes['autocomplete'];
		disabled?: boolean;
		generate?: () => void;
		mono?: boolean;
		ref?: HTMLInputElement | null;
		reveal?: boolean;
	}

	let {
		id,
		value = $bindable(),
		placeholder,
		required = false,
		minlength,
		autocomplete = 'current-password',
		disabled = false,
		generate,
		mono = true,
		ref = $bindable(null),
		reveal = $bindable(false)
	}: Props = $props();
</script>

<div class="flex gap-1.5">
	<input
		{id}
		bind:this={ref}
		class={'stw-input flex-1 ' + (mono ? 'font-mono' : '')}
		type={reveal ? 'text' : 'password'}
		bind:value
		{placeholder}
		{required}
		{minlength}
		{autocomplete}
		{disabled}
	/>
	{#snippet eyeIcon()}
		{#if reveal}
			<EyeOff size={13} strokeWidth={1.7} />
		{:else}
			<Eye size={13} strokeWidth={1.7} />
		{/if}
	{/snippet}
	<IconButton
		label={reveal ? 'Hide password' : 'Show password'}
		size={30}
		icon={eyeIcon}
		onclick={() => (reveal = !reveal)}
	/>
	{#if generate}
		{#snippet refreshIcon()}<RefreshCw size={13} strokeWidth={1.7} />{/snippet}
		<IconButton label="Generate password" size={30} icon={refreshIcon} onclick={generate} />
	{/if}
</div>
