<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import {
		Search,
		Database,
		Link as LinkIcon,
		Users,
		Activity,
		Upload,
		Moon,
		Folder
	} from 'lucide-svelte';
	import Kbd from '$lib/components/ui/Kbd.svelte';
	import BackendMark from '$lib/components/ui/BackendMark.svelte';
	import { toggleTheme } from '$lib/stores/theme.svelte';
	import { urlForRoute } from '$lib/route';
	import { inferKind } from '$lib/backend-kind';
	import { bucketList } from '$lib/stores/buckets.svelte';
	import type { Backend, BackendKind } from '$lib/types';

	interface Props {
		backends: Backend[];
		onclose: () => void;
	}

	let { backends, onclose }: Props = $props();

	type IconKind =
		| 'database'
		| 'link'
		| 'users'
		| 'activity'
		| 'upload'
		| 'moon'
		| 'folder'
		| 'backend';

	interface Item {
		id: string;
		label: string;
		kind: string;
		icon: IconKind;
		backendKind?: BackendKind;
		run: () => void;
	}

	let q = $state('');
	let idx = $state(0);
	let inputEl: HTMLInputElement | null = $state(null);

	const items = $derived.by<Item[]>(() => {
		const all: Item[] = [
			{
				id: 'go-backends',
				label: 'Go to Backends',
				kind: 'Navigate',
				icon: 'database',
				run: () => goto(urlForRoute({ type: 'backends' }))
			},
			{
				id: 'go-shares',
				label: 'My shares',
				kind: 'Navigate',
				icon: 'link',
				run: () => goto(urlForRoute({ type: 'shares' }))
			},
			{
				id: 'go-users',
				label: 'Admin — Users',
				kind: 'Navigate',
				icon: 'users',
				run: () => goto(urlForRoute({ type: 'admin-users' }))
			},
			{
				id: 'go-audit',
				label: 'Admin — Audit log',
				kind: 'Navigate',
				icon: 'activity',
				run: () => goto(urlForRoute({ type: 'admin-audit' }))
			},
			{
				id: 'action-theme',
				label: 'Toggle theme',
				kind: 'Action',
				icon: 'moon',
				run: () => toggleTheme()
			}
		];
		for (const b of backends) {
			all.push({
				id: 'b-' + b.id,
				label: b.name,
				kind: 'Backend',
				icon: 'backend',
				backendKind: inferKind(b),
				run: () => goto(urlForRoute({ type: 'backend', backend: b.id }))
			});
			const bks = bucketList(b.id);
			if (bks) {
				for (const bk of bks) {
					all.push({
						id: 'bk-' + b.id + '-' + bk.name,
						label: b.name + '/' + bk.name,
						kind: 'Bucket',
						icon: 'folder',
						run: () =>
							goto(urlForRoute({ type: 'bucket', backend: b.id, bucket: bk.name, prefix: [] }))
					});
				}
			}
		}
		const needle = q.trim().toLowerCase();
		return needle ? all.filter((i) => i.label.toLowerCase().includes(needle)) : all;
	});

	$effect(() => {
		void q;
		idx = 0;
	});

	onMount(() => {
		void tick().then(() => inputEl?.focus());
		const onKey = (e: KeyboardEvent): void => {
			if (e.key === 'Escape') {
				onclose();
			} else if (e.key === 'ArrowDown') {
				idx = Math.min(items.length - 1, idx + 1);
				e.preventDefault();
			} else if (e.key === 'ArrowUp') {
				idx = Math.max(0, idx - 1);
				e.preventDefault();
			} else if (e.key === 'Enter') {
				items[idx]?.run();
				onclose();
			}
		};
		window.addEventListener('keydown', onKey);
		return () => window.removeEventListener('keydown', onKey);
	});
</script>

<div
	role="dialog"
	aria-modal="true"
	tabindex="-1"
	onclick={onclose}
	onkeydown={(e) => {
		if (e.key === 'Escape') onclose();
	}}
	class="fixed inset-0 z-[var(--stw-z-modal)] flex items-start justify-center bg-black/35 pt-[100px]"
>
	<div
		onclick={(e) => e.stopPropagation()}
		role="presentation"
		class="w-[560px] max-w-[calc(100vw-24px)] overflow-hidden rounded-[10px] border border-[var(--stw-border)] bg-[var(--stw-bg-panel)] shadow-[var(--stw-shadow-lg)]"
	>
		<div class="flex items-center gap-2.5 border-b border-[var(--stw-border)] px-3.5 py-2.5">
			<Search size={15} strokeWidth={1.7} />
			<input
				bind:this={inputEl}
				bind:value={q}
				placeholder="Type a command, bucket, or user…"
				class="h-[28px] flex-1 border-0 bg-transparent text-[14px] text-[var(--stw-fg)] outline-0"
			/>
			<Kbd>esc</Kbd>
		</div>
		<div class="stw-scroll max-h-[360px] overflow-auto p-1.5">
			{#if items.length === 0}
				<div class="px-3.5 py-5 text-[13px] text-[var(--stw-fg-soft)]">
					No results for "{q}"
				</div>
			{/if}
			{#each items as it, i (it.id)}
				<button
					type="button"
					onclick={() => {
						it.run();
						onclose();
					}}
					onmousemove={() => (idx = i)}
					class="flex h-[34px] w-full cursor-pointer items-center gap-2.5 rounded-md border-0 px-2.5 text-left text-[13px] text-[var(--stw-fg)] {i ===
					idx
						? 'bg-[var(--stw-bg-hover)]'
						: 'bg-transparent'}"
				>
					<span class="inline-flex text-[var(--stw-fg-mute)]">
						{#if it.icon === 'database'}<Database size={14} strokeWidth={1.7} />{/if}
						{#if it.icon === 'link'}<LinkIcon size={14} strokeWidth={1.7} />{/if}
						{#if it.icon === 'users'}<Users size={14} strokeWidth={1.7} />{/if}
						{#if it.icon === 'activity'}<Activity size={14} strokeWidth={1.7} />{/if}
						{#if it.icon === 'upload'}<Upload size={14} strokeWidth={1.7} />{/if}
						{#if it.icon === 'moon'}<Moon size={14} strokeWidth={1.7} />{/if}
						{#if it.icon === 'folder'}<Folder size={14} strokeWidth={1.7} />{/if}
						{#if it.icon === 'backend' && it.backendKind}
							<BackendMark kind={it.backendKind} size={14} />
						{/if}
					</span>
					<span class="flex-1 truncate">{it.label}</span>
					<span
						class="text-[10.5px] font-medium tracking-[0.05em] text-[var(--stw-fg-soft)] uppercase"
					>
						{it.kind}
					</span>
				</button>
			{/each}
		</div>
		<div
			class="flex gap-3 border-t border-[var(--stw-border)] px-3.5 py-2 text-[11px] text-[var(--stw-fg-soft)]"
		>
			<span><Kbd>↑</Kbd> <Kbd>↓</Kbd> navigate</span>
			<span><Kbd>↵</Kbd> select</span>
			<span><Kbd>esc</Kbd> close</span>
		</div>
	</div>
</div>
