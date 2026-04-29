<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { Upload, Pause, Play, X } from 'lucide-svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import Chevron from '$lib/components/ui/Chevron.svelte';
	import ObjectIcon from '$lib/components/browser/ObjectIcon.svelte';
	import {
		queue,
		clearQueue,
		pauseUpload,
		resumeUpload,
		cancelUpload
	} from '$lib/stores/uploads.svelte';

	let open = $state(true);

	const active = $derived(
		queue.items.filter((u) => u.status === 'uploading' || u.status === 'paused').length
	);
	const done = $derived(queue.items.filter((u) => u.status === 'done').length);

	function statusLabel(u: (typeof queue.items)[number]): string {
		if (u.status === 'error') return 'failed';
		if (u.status === 'paused') return 'paused';
		if (u.status === 'done') return 'done';
		if (u.status === 'conflict') return 'exists';
		return Math.round(u.progress) + '%';
	}

	function statusToneCls(u: (typeof queue.items)[number]): string {
		if (u.status === 'error') return 'text-[var(--stw-err)]';
		if (u.status === 'conflict') return 'text-[var(--stw-warn)]';
		return 'text-[var(--stw-fg-soft)]';
	}

	function barColor(u: (typeof queue.items)[number]): string {
		if (u.status === 'error') return 'var(--stw-err)';
		if (u.status === 'done') return 'var(--stw-ok)';
		if (u.status === 'paused') return 'var(--stw-fg-soft)';
		if (u.status === 'conflict') return 'var(--stw-warn)';
		return 'var(--stw-accent-600)';
	}
</script>

{#if queue.items.length > 0}
	<div
		class="absolute right-4 bottom-4 z-30 w-[340px] max-w-[calc(100vw-32px)] overflow-hidden rounded-[10px] border border-[var(--stw-border)] bg-[var(--stw-bg-panel)] shadow-[var(--stw-shadow-lg)]"
	>
		<header
			role="button"
			tabindex="0"
			onclick={() => (open = !open)}
			onkeydown={(e) => {
				if (e.key === 'Enter' || e.key === ' ') open = !open;
			}}
			class="flex h-[36px] cursor-pointer items-center gap-2 pr-2.5 pl-3.5 {open
				? 'border-b border-[var(--stw-border)]'
				: ''}"
		>
			<Upload size={14} strokeWidth={1.7} />
			<span class="text-[13px] font-semibold">Uploads</span>
			<span class="text-[11.5px] text-[var(--stw-fg-soft)] tabular-nums">
				{active > 0 ? active + ' in progress' : done + ' complete'}
			</span>
			<span class="flex-1"></span>
			<Chevron size={12} dir={open ? 'down' : 'up'} />
			<button
				type="button"
				onclick={(e) => {
					e.stopPropagation();
					clearQueue();
				}}
				aria-label="Dismiss"
				class="inline-flex h-[22px] w-[22px] cursor-pointer items-center justify-center rounded border-0 bg-transparent text-[var(--stw-fg-mute)] hover:bg-[var(--stw-bg-hover)] hover:text-[var(--stw-fg)]"
			>
				<X size={12} strokeWidth={1.7} />
			</button>
		</header>
		{#if open}
			<div class="stw-scroll max-h-[240px] overflow-auto">
				{#each queue.items as u (u.id)}
					<div
						class="flex flex-col gap-1.5 border-b border-[var(--stw-border)] px-3.5 py-2.5 last:border-b-0"
					>
						<div class="flex items-center gap-2 text-[12.5px]">
							<ObjectIcon kind={u.kind} />
							<span class="min-w-0 flex-1 truncate font-mono text-[12px]">{u.name}</span>
							<span class="text-[11.5px] tabular-nums {statusToneCls(u)}">
								{statusLabel(u)}
							</span>
							{#if u.status === 'uploading'}
								{#snippet pauseIcon()}<Pause size={12} strokeWidth={1.7} />{/snippet}
								<IconButton
									label="Pause"
									size={20}
									icon={pauseIcon}
									onclick={() => pauseUpload(u.id)}
								/>
							{:else if u.status === 'paused'}
								{#snippet playIcon()}<Play size={12} strokeWidth={1.7} />{/snippet}
								<IconButton
									label="Resume"
									size={20}
									icon={playIcon}
									onclick={() => resumeUpload(u.id)}
								/>
							{/if}
							{#if u.status !== 'done'}
								{#snippet xIcon()}<X size={12} strokeWidth={1.7} />{/snippet}
								<IconButton
									label={u.status === 'error' ? 'Dismiss' : 'Cancel'}
									size={20}
									icon={xIcon}
									onclick={() => cancelUpload(u.id)}
								/>
							{/if}
						</div>
						<div class="h-[3px] overflow-hidden rounded-full bg-[var(--stw-bg-hover)]">
							<div
								class="h-full transition-[width] duration-200 ease-out"
								style="width:{u.progress}%;background:{barColor(u)};"
							></div>
						</div>
						{#if u.error}
							<div class="text-[11.5px] text-[var(--stw-err)]">{u.error}</div>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</div>
{/if}
