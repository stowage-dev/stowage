<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { Folder, Image, Video, FileText, File } from 'lucide-svelte';
	import type { ObjectKind } from '$lib/types';

	interface Props {
		kind: ObjectKind;
		size?: number;
	}

	let { kind, size = 15 }: Props = $props();

	const color = $derived(kind === 'folder' ? 'var(--stw-accent-600)' : 'var(--stw-fg-mute)');
</script>

<span style="color:{color};display:inline-flex;align-items:center;">
	{#if kind === 'folder'}
		<Folder {size} strokeWidth={1.7} />
	{:else if kind === 'image'}
		<Image {size} strokeWidth={1.7} />
	{:else if kind === 'video'}
		<Video {size} strokeWidth={1.7} />
	{:else if kind === 'pdf'}
		<FileText {size} strokeWidth={1.7} />
	{:else if kind === 'text'}
		<FileText {size} strokeWidth={1.7} />
	{:else}
		<File {size} strokeWidth={1.7} />
	{/if}
</span>
