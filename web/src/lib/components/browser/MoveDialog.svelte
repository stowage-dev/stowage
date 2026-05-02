<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { ArrowRight, Cable } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Segmented from '$lib/components/ui/Segmented.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import { api } from '$lib/api';
	import type { Backend, Bucket, PrefixEvent } from '$lib/types';

	interface Props {
		backendId: string;
		srcBucket: string;
		srcPrefix: string;
		keys: string[];
		backends: Backend[];
		defaultOperation?: 'copy' | 'move';
		onclose: () => void;
		oncomplete: () => void;
	}

	let {
		backendId,
		srcBucket,
		srcPrefix,
		keys,
		backends,
		defaultOperation = 'copy',
		onclose,
		oncomplete
	}: Props = $props();

	let operation = $state<'copy' | 'move'>(untrack(() => defaultOperation));
	let dstBackendId = $state(untrack(() => backendId));
	let dstBucket = $state(untrack(() => srcBucket));
	let dstPrefix = $state(untrack(() => srcPrefix));
	let buckets = $state<Bucket[] | null>(null);
	let bucketsError = $state<string | null>(null);
	let running = $state(false);
	let processed = $state(0);
	let failed = $state(0);

	const folderCount = $derived(keys.filter((k) => k.endsWith('/')).length);
	const fileCount = $derived(keys.length - folderCount);
	const normalizedDst = $derived(normalizePrefix(dstPrefix));
	const normalizedSrc = $derived(normalizePrefix(srcPrefix));
	const crossBackend = $derived(dstBackendId !== backendId);
	const sameLocation = $derived(
		!crossBackend && dstBucket === srcBucket && normalizedDst === normalizedSrc
	);

	function normalizePrefix(p: string): string {
		const trimmed = p.trim();
		if (!trimmed) return '';
		return trimmed.endsWith('/') ? trimmed : trimmed + '/';
	}

	async function loadBuckets(forBackend: string): Promise<void> {
		buckets = null;
		bucketsError = null;
		try {
			buckets = await api.listBuckets(forBackend);
			if (buckets && buckets.length > 0 && !buckets.find((b) => b.name === dstBucket)) {
				dstBucket = buckets[0].name;
			}
		} catch (err) {
			bucketsError = err instanceof Error ? err.message : 'Failed to load buckets.';
		}
	}

	onMount(() => {
		void loadBuckets(dstBackendId);
	});

	$effect(() => {
		const target = dstBackendId;
		untrack(() => {
			if (target !== '') void loadBuckets(target);
		});
	});

	async function confirm(): Promise<void> {
		if (running) return;
		if (sameLocation) {
			toast.error('Pick a different destination bucket or prefix.');
			return;
		}

		running = true;
		processed = 0;
		failed = 0;

		const cleanFiles: string[] = [];
		const cleanFolders: string[] = [];
		let totalCopied = 0;
		let totalFailed = 0;
		const dstBucketParam = dstBucket === srcBucket && !crossBackend ? undefined : dstBucket;
		const dstBackendParam = crossBackend ? dstBackendId : undefined;
		const onPrefixEvent = (ev: PrefixEvent): void => {
			if (ev.event === 'object') processed += 1;
			if (ev.event === 'error') {
				processed += 1;
				totalFailed += 1;
				failed = totalFailed;
			}
		};

		for (const relKey of keys) {
			const isFolder = relKey.endsWith('/');
			if (isFolder) {
				try {
					const result = await api.copyPrefix(
						backendId,
						srcBucket,
						srcPrefix + relKey,
						normalizedDst + relKey,
						{
							dstBucket: dstBucketParam,
							dstBackend: dstBackendParam,
							onEvent: onPrefixEvent
						}
					);
					totalCopied += result.copied ?? 0;
					if ((result.failed ?? 0) === 0) cleanFolders.push(relKey);
					else totalFailed = failed;
				} catch (err) {
					totalFailed += 1;
					failed = totalFailed;
					processed += 1;
					console.error('copy-prefix failed', relKey, err);
				}
			} else {
				try {
					await api.copyObject(backendId, srcBucket, srcPrefix + relKey, normalizedDst + relKey, {
						dstBucket: dstBucketParam,
						dstBackend: dstBackendParam
					});
					cleanFiles.push(relKey);
					totalCopied += 1;
				} catch (err) {
					totalFailed += 1;
					failed = totalFailed;
					console.error('copy failed', relKey, err);
				}
				processed += 1;
			}
		}

		let moveCleanupFailed = 0;
		if (operation === 'move' && (cleanFiles.length > 0 || cleanFolders.length > 0)) {
			if (cleanFiles.length > 0) {
				try {
					await api.bulkDelete(
						backendId,
						srcBucket,
						cleanFiles.map((relKey) => ({ key: srcPrefix + relKey }))
					);
				} catch (err) {
					moveCleanupFailed += cleanFiles.length;
					console.error('source delete failed', err);
				}
			}
			for (const relKey of cleanFolders) {
				try {
					await api.deletePrefix(backendId, srcBucket, srcPrefix + relKey);
				} catch (err) {
					moveCleanupFailed += 1;
					console.error('source folder delete failed', relKey, err);
				}
			}
		}

		running = false;
		if (totalFailed > 0) {
			toast.error(
				`${operation === 'move' ? 'Move' : 'Copy'} finished with ${totalFailed} failure${
					totalFailed === 1 ? '' : 's'
				} — sources left intact for failed entries.`
			);
		} else if (moveCleanupFailed > 0) {
			toast.error(
				`Copied ${totalCopied}, but deleting ${moveCleanupFailed} source${
					moveCleanupFailed === 1 ? '' : 's'
				} failed — objects now exist in both locations.`
			);
		} else {
			toast.success(
				operation === 'move'
					? `Moved ${totalCopied} object${totalCopied === 1 ? '' : 's'}.`
					: `Copied ${totalCopied} object${totalCopied === 1 ? '' : 's'}.`
			);
		}
		oncomplete();
		onclose();
	}
</script>

<Modal
	title="{operation === 'move' ? 'Move' : 'Copy'} {keys.length} object{keys.length === 1
		? ''
		: 's'}"
	subtitle="{srcBucket}/{srcPrefix}"
	subtitleMono
	busy={running}
	closeOnBackdrop={false}
	showClose={!running}
	{onclose}
	maxWidth="520px"
>
	<div class="flex flex-col gap-3.5 px-[18px] py-4">
		<div class="flex items-center gap-2.5">
			<Segmented
				value={operation}
				onchange={(v) => (operation = v)}
				size="sm"
				options={[
					{ value: 'move' as const, label: 'Move' },
					{ value: 'copy' as const, label: 'Copy' }
				]}
			/>
			<span class="text-[11.5px] text-stw-fg-soft">
				{operation === 'move'
					? 'Copies to destination, then deletes sources.'
					: 'Copies to destination; sources remain.'}
			</span>
		</div>

		{#if backends.length > 1}
			<FormField label="Destination backend">
				<select bind:value={dstBackendId} disabled={running} class="stw-input font-mono">
					{#each backends as b (b.id)}
						<option value={b.id}>{b.name} ({b.id})</option>
					{/each}
				</select>
			</FormField>
		{/if}

		<FormField label="Destination bucket" error={bucketsError ?? undefined}>
			{#if buckets === null && !bucketsError}
				<div class="text-[12px] text-stw-fg-soft">Loading buckets…</div>
			{:else if buckets}
				<select bind:value={dstBucket} disabled={running} class="stw-input font-mono">
					{#each buckets as b (b.name)}
						<option value={b.name}>{b.name}</option>
					{/each}
				</select>
			{/if}
		</FormField>

		<FormField
			label="Destination prefix"
			helper="Trailing slash is added automatically. Leave blank to use the bucket root."
		>
			<input
				type="text"
				bind:value={dstPrefix}
				disabled={running}
				placeholder="(bucket root)"
				class="stw-input font-mono"
			/>
		</FormField>

		<div
			class="flex items-center gap-2 rounded-md border border-stw-border bg-stw-bg-sunken px-3 py-2.5 font-mono text-[12px]"
		>
			<span class="min-w-0 flex-1 truncate text-stw-fg-soft">
				{backendId}/{srcBucket}/{srcPrefix || '(root)'}
			</span>
			<ArrowRight size={12} strokeWidth={1.7} class="flex-shrink-0 text-stw-fg-mute" />
			<span class="min-w-0 flex-1 truncate">
				{dstBackendId}/{dstBucket}/{normalizedDst || '(root)'}
			</span>
		</div>

		{#if crossBackend}
			{#snippet cableIcon()}
				<Cable size={13} strokeWidth={1.7} />
			{/snippet}
			<Banner icon={cableIcon}>
				Bytes will stream through the proxy from
				<strong>{backendId}</strong>
				to
				<strong>{dstBackendId}</strong>. Slower than a same-backend copy and subject to the
				destination's quota.
			</Banner>
		{/if}

		{#if folderCount > 0}
			<div
				class="rounded-md border border-stw-border bg-stw-bg-sunken px-3 py-2 text-[11.5px] text-stw-fg-soft"
			>
				{#if fileCount > 0}
					{folderCount} folder{folderCount === 1 ? '' : 's'} + {fileCount} file{fileCount === 1
						? ''
						: 's'} — folders are walked recursively.
				{:else}
					{folderCount} folder{folderCount === 1 ? '' : 's'} — copied recursively.
				{/if}
			</div>
		{/if}

		{#if running}
			<div class="text-[12px] text-stw-fg-soft">
				{processed} processed{#if failed > 0}
					· {failed} failed{/if}
			</div>
		{/if}
	</div>

	{#snippet footer()}
		<Button variant="ghost" onclick={onclose}>Cancel</Button>
		<Button
			variant="primary"
			onclick={confirm}
			disabled={running || sameLocation || buckets === null}
		>
			{running ? 'Working…' : operation === 'move' ? 'Move' : 'Copy'}
		</Button>
	{/snippet}
</Modal>
