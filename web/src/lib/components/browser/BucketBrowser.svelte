<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { goto, invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import {
		Folder,
		Info,
		Plus,
		Upload,
		Download,
		Trash2,
		Search,
		FolderInput,
		RotateCw,
		X
	} from 'lucide-svelte';
	import { untrack } from 'svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Tooltip from '$lib/components/ui/Tooltip.svelte';
	import BackendMark from '$lib/components/ui/BackendMark.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import SearchField from '$lib/components/ui/SearchField.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import ObjectTable from './ObjectTable.svelte';
	import DetailDrawer from './DetailDrawer.svelte';
	import MoveDialog from './MoveDialog.svelte';
	import ConfirmDialog from '$lib/components/ui/ConfirmDialog.svelte';
	import { page } from '$app/state';
	import { api, ApiException } from '$lib/api';
	import { bytes } from '$lib/format';
	import {
		queue,
		resolveAllConflicts,
		uploadFiles,
		type UploadEntry
	} from '$lib/stores/uploads.svelte';
	import { inferKind } from '$lib/backend-kind';
	import { toBrowserItems } from '$lib/browser-items';
	import { session } from '$lib/stores/session.svelte';
	import { bucketList } from '$lib/stores/buckets.svelte';
	import type { Backend, BrowserItem, Bucket, BucketQuota, ListObjectsResult } from '$lib/types';

	// Per-folder size cache persists across navigations within the same session.
	const folderSizeCache = new Map<string, number>();

	interface Props {
		backend: Backend;
		bucket: string;
		prefix: string[];
		listing: ListObjectsResult | null;
		quota?: BucketQuota | null;
		error: string | null;
		onshare: (item: BrowserItem) => void;
	}

	let { backend, bucket, prefix, listing, quota, error, onshare }: Props = $props();
	// Pull the current bucket row out of the live bucket store so size-tracking
	// state (and any other Bucket fields surfaced here later) lights up as soon
	// as the per-backend list resolves, without blocking the page render.
	const bucketRow = $derived.by<Bucket | null>(() => {
		const list = bucketList(backend.id);
		return list?.find((b) => b.name === bucket) ?? null;
	});
	const sizeTracked = $derived(bucketRow?.size_tracked ?? true);

	const allItems = $derived(listing ? toBrowserItems(listing) : []);
	const kind = $derived(inferKind(backend));
	const isAdmin = $derived(session.me?.role === 'admin');

	// Quota banner: only show something when usage is past the soft mark
	// or close to the hard cap. Hidden in the no-data-yet case.
	const quotaBanner = $derived.by<{
		tone: 'warn' | 'danger';
		title: string;
		hint: string;
	} | null>(() => {
		if (!quota || !quota.has_usage || !quota.configured) return null;
		const used = quota.usage_bytes ?? 0;
		const soft = quota.soft_bytes ?? 0;
		const hard = quota.hard_bytes ?? 0;
		if (hard > 0 && used >= hard) {
			return {
				tone: 'danger',
				title: 'Hard quota reached.',
				hint: `${bytes(used)} of ${bytes(hard)} — uploads are blocked until objects are removed or the quota is raised.`
			};
		}
		if (hard > 0 && used >= hard * 0.9) {
			return {
				tone: 'danger',
				title: `Approaching hard quota — ${Math.round((used * 100) / hard)}% used.`,
				hint: `${bytes(used)} of ${bytes(hard)}.`
			};
		}
		if (soft > 0 && used >= soft) {
			return {
				tone: 'warn',
				title: 'Soft quota exceeded.',
				hint: `${bytes(used)} used; soft cap is ${bytes(soft)}${hard > 0 ? `, hard cap ${bytes(hard)}` : ''}.`
			};
		}
		return null;
	});
	const canWrite = $derived(
		isAdmin || session.me?.role === 'editor' || session.me?.role === 'user'
	);

	let selected = $state<string[]>([]);
	let detail = $state<BrowserItem | null>(null);
	let dragOver = $state(false);
	let creatingFolder = $state(false);
	let folderName = $state('');
	let fileInput: HTMLInputElement | null = $state(null);
	let folderInput: HTMLInputElement | null = $state(null);
	let filter = $state('');
	let moveDialog = $state<{ keys: string[]; defaultOperation: 'copy' | 'move' } | null>(null);
	let refreshing = $state(false);

	// Per-folder recursive byte totals, computed lazily after each listing.
	// 'loading' while in flight; 'error' when the walk failed; absent until a
	// fetch is kicked off. Keys are bucket-root-relative (matches BrowserItem.key).
	type FolderSize = number | 'loading' | 'error';
	let folderSizes = $state<Record<string, FolderSize>>({});
	// Generation counter so stale workers from a previous listing don't write
	// into the current map after the user navigates away.
	let sizesGen = 0;

	// Confirmation dialogs. `confirmDelete` covers single + bulk delete; the
	// onconfirm closure captures the work to run. `pendingConflicts` is
	// derived from the upload queue so the overwrite dialog appears on its
	// own when 412s come back from the server.
	let confirmDelete = $state<{
		title: string;
		description: string;
		busy: boolean;
		onconfirm: () => Promise<void>;
	} | null>(null);
	const pendingConflicts = $derived(queue.items.filter((u) => u.status === 'conflict'));

	// Client-side name substring filter of the current listing (spec Phase 4).
	// Case-insensitive match on displayName. Server-side prefix search stays
	// as ListObjects' prefix parameter.
	const items = $derived.by(() => {
		const q = filter.trim().toLowerCase();
		if (!q) return allItems;
		return allItems.filter((x) => x.displayName.toLowerCase().includes(q));
	});
	const totalSize = $derived(items.reduce((a, x) => a + (x.size ?? 0), 0));

	$effect(() => {
		// Re-run on backend/bucket/prefix change.
		void backend.id;
		void bucket;
		void prefix.join('/');
		selected = [];
		detail = null;
		filter = '';
	});

	$effect(() => {
		// Reset folder sizes whenever the listing changes. Individual sizes are
		// fetched lazily via onFolderVisible as rows scroll into the viewport.
		void listing;
		void sizeTracked;
		untrack(() => {
			folderSizes = {};
			sizesGen++;
		});
	});

	async function loadOneFolderSize(key: string): Promise<void> {
		if (!sizeTracked) return;
		if (folderSizes[key] !== undefined) return;
		const cached = folderSizeCache.get(key);
		if (cached !== undefined) {
			folderSizes = { ...folderSizes, [key]: cached };
			return;
		}
		const gen = sizesGen;
		folderSizes = { ...folderSizes, [key]: 'loading' };
		try {
			const r = await api.prefixSize(backend.id, bucket, s3Prefix() + key);
			if (gen !== sizesGen) return;
			folderSizeCache.set(key, r.bytes);
			folderSizes = { ...folderSizes, [key]: r.bytes };
		} catch (err) {
			if (gen !== sizesGen) return;
			folderSizes = { ...folderSizes, [key]: 'error' };
			console.warn('prefix-size failed', key, err);
		}
	}

	async function refreshAll(): Promise<void> {
		if (refreshing) return;
		refreshing = true;
		try {
			// Best-effort quota recompute. Backends without quotas configured
			// return 501 — that's expected, not an error worth surfacing.
			try {
				await api.recomputeBucketQuota(backend.id, bucket);
			} catch (err) {
				if (!(err instanceof ApiException) || err.status !== 501) {
					console.warn('quota recompute failed', err);
				}
			}
			await invalidateAll();
			toast.success('Refreshed');
		} finally {
			refreshing = false;
		}
	}

	function s3Prefix(): string {
		return prefix.length ? prefix.join('/') + '/' : '';
	}

	function openItem(it: BrowserItem): void {
		if (it.kind === 'folder') {
			const folderName = it.key.endsWith('/') ? it.key.slice(0, -1) : it.key;
			goto(
				`/b/${encodeURIComponent(backend.id)}/${encodeURIComponent(bucket)}` +
					(prefix.length ? '/' + prefix.map(encodeURIComponent).join('/') : '') +
					'/' +
					encodeURIComponent(folderName)
			);
		} else {
			detail = it;
		}
	}

	async function onDrop(e: DragEvent) {
		e.preventDefault();
		dragOver = false;
		if (!canWrite) return;
		const dt = e.dataTransfer;
		if (!dt) return;

		// `webkitGetAsEntry` is the only way to recover folder structure from a
		// drop — `dataTransfer.files` flattens directories to a single 0-byte
		// entry that XHR can't read. Pull the entries synchronously while the
		// drop event is still active, then walk them async.
		const roots: FileSystemEntry[] = [];
		if (dt.items && dt.items.length > 0) {
			for (let i = 0; i < dt.items.length; i++) {
				const it = dt.items[i];
				if (it.kind !== 'file') continue;
				const fe = it.webkitGetAsEntry?.();
				if (fe) roots.push(fe);
			}
		}

		const entries: UploadEntry[] = [];
		if (roots.length > 0) {
			for (const root of roots) {
				await collectEntries(root, '', entries);
			}
		} else if (dt.files) {
			for (const f of Array.from(dt.files)) {
				entries.push({ file: f, relativePath: f.name });
			}
		}
		if (!entries.length) return;
		await runUploads(entries);
	}

	async function collectEntries(
		entry: FileSystemEntry,
		prefix: string,
		out: UploadEntry[]
	): Promise<void> {
		if (entry.isFile) {
			const fileEntry = entry as FileSystemFileEntry;
			const file = await new Promise<File>((resolve, reject) => fileEntry.file(resolve, reject));
			out.push({ file, relativePath: prefix + entry.name });
			return;
		}
		if (!entry.isDirectory) return;
		const dirEntry = entry as FileSystemDirectoryEntry;
		const reader = dirEntry.createReader();
		const subPrefix = prefix + entry.name + '/';
		// readEntries returns at most ~100 children per call; loop until empty.
		while (true) {
			const batch = await new Promise<FileSystemEntry[]>((resolve, reject) =>
				reader.readEntries(resolve, reject)
			);
			if (batch.length === 0) break;
			for (const child of batch) {
				await collectEntries(child, subPrefix, out);
			}
		}
	}

	function onPickFiles() {
		fileInput?.click();
	}

	function onPickFolder() {
		folderInput?.click();
	}

	async function onFileChange(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const files = input.files ? Array.from(input.files) : [];
		input.value = '';
		if (!files.length) return;
		await runUploads(files.map((f) => ({ file: f, relativePath: f.name })));
	}

	async function onFolderChange(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const files = input.files ? Array.from(input.files) : [];
		input.value = '';
		if (!files.length) return;
		await runUploads(files.map((f) => ({ file: f, relativePath: f.webkitRelativePath || f.name })));
	}

	async function runUploads(entries: UploadEntry[]) {
		await uploadFiles(api, backend.id, bucket, s3Prefix(), entries);
		await invalidateAll();
	}

	async function deleteSelected() {
		if (selected.length === 0) return;
		const count = selected.length;
		const targets = selected.slice();
		const folders = targets.filter((k) => k.endsWith('/'));
		const files = targets.filter((k) => !k.endsWith('/'));
		const description =
			folders.length > 0
				? `Folders are deleted recursively — every object inside will be removed. This cannot be undone.`
				: 'This cannot be undone.';
		confirmDelete = {
			title: `Delete ${count} item${count === 1 ? '' : 's'}?`,
			description,
			busy: false,
			onconfirm: async () => {
				if (!confirmDelete) return;
				confirmDelete = { ...confirmDelete, busy: true };
				try {
					let deleted = 0;
					let failed = 0;
					if (files.length > 0) {
						const res = await api.bulkDelete(
							backend.id,
							bucket,
							files.map((k) => ({ key: s3Prefix() + k }))
						);
						deleted += res.deleted?.length ?? 0;
						failed += res.errors?.length ?? 0;
					}
					for (const folder of folders) {
						try {
							const done = await api.deletePrefix(backend.id, bucket, s3Prefix() + folder);
							deleted += done.deleted ?? 0;
							failed += done.failed ?? 0;
						} catch (err) {
							failed += 1;
							console.error('delete-prefix failed', folder, err);
						}
					}
					toast[failed === 0 ? 'success' : 'error'](
						`Deleted ${deleted}` + (failed > 0 ? `, ${failed} failed` : '')
					);
					selected = [];
					await invalidateAll();
				} catch (err) {
					toast.error(err instanceof ApiException ? err.message : 'Delete failed.');
				} finally {
					confirmDelete = null;
				}
			}
		};
	}

	function openMove(keys: string[], op: 'copy' | 'move') {
		if (keys.length === 0) {
			toast.error('Select at least one item.');
			return;
		}
		moveDialog = { keys: keys.slice(), defaultOperation: op };
	}

	async function onMoveComplete() {
		selected = [];
		detail = null;
		await invalidateAll();
	}

	async function renameOne(it: BrowserItem) {
		const isFolder = it.kind === 'folder';
		const next = window.prompt('Rename to:', it.displayName);
		if (next === null) return;
		const trimmed = next.trim();
		if (!trimmed || trimmed === it.displayName) return;
		if (trimmed.includes('/') || trimmed.includes('\0')) {
			toast.error("Name can't contain '/' or null bytes.");
			return;
		}
		try {
			if (isFolder) {
				const srcPrefixKey = s3Prefix() + it.key; // ends with /
				const dstPrefixKey = s3Prefix() + trimmed + '/';
				const result = await api.copyPrefix(backend.id, bucket, srcPrefixKey, dstPrefixKey);
				if ((result.failed ?? 0) > 0) {
					toast.error(`Copied ${result.copied ?? 0} but ${result.failed} failed — source kept.`);
					return;
				}
				await api.deletePrefix(backend.id, bucket, srcPrefixKey);
			} else {
				const srcKey = s3Prefix() + it.key;
				const dstKey = s3Prefix() + trimmed;
				await api.renameObject(backend.id, bucket, srcKey, dstKey);
			}
			toast.success(`Renamed to "${trimmed}"`);
			detail = null;
			selected = selected.filter((k) => k !== it.key);
			await invalidateAll();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Rename failed.');
		}
	}

	async function deleteOne(it: BrowserItem) {
		const isFolder = it.kind === 'folder';
		confirmDelete = {
			title: `Delete "${it.displayName}"?`,
			description: isFolder
				? 'Folders are deleted recursively — every object inside will be removed. This cannot be undone.'
				: 'This cannot be undone.',
			busy: false,
			onconfirm: async () => {
				if (!confirmDelete) return;
				confirmDelete = { ...confirmDelete, busy: true };
				try {
					if (isFolder) {
						const result = await api.deletePrefix(backend.id, bucket, s3Prefix() + it.key);
						if ((result.failed ?? 0) > 0) {
							toast.error(`Deleted ${result.deleted ?? 0}, ${result.failed} failed.`);
						} else {
							toast.success(
								`Deleted ${result.deleted ?? 0} object${(result.deleted ?? 0) === 1 ? '' : 's'}.`
							);
						}
					} else {
						await api.deleteObject(backend.id, bucket, s3Prefix() + it.key);
						toast.success('Deleted');
					}
					detail = null;
					selected = selected.filter((k) => k !== it.key);
					await invalidateAll();
				} catch (err) {
					toast.error(err instanceof ApiException ? err.message : 'Delete failed.');
				} finally {
					confirmDelete = null;
				}
			}
		};
	}

	async function submitFolder(e: SubmitEvent) {
		e.preventDefault();
		const trimmed = folderName.trim();
		if (!trimmed) return;
		try {
			await api.createFolder(backend.id, bucket, s3Prefix() + trimmed);
			toast.success(`Folder "${trimmed}" created`);
			creatingFolder = false;
			folderName = '';
			await invalidateAll();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not create folder.');
		}
	}

	function downloadOne(it: BrowserItem) {
		const url = api.objectURL(backend.id, bucket, s3Prefix() + it.key);
		window.location.href = url;
	}

	function downloadSelected() {
		if (selected.length === 0) return;
		// Single, non-folder → plain GetObject download.
		if (selected.length === 1 && !selected[0].endsWith('/')) {
			const it = items.find((x) => x.key === selected[0]);
			if (it) downloadOne(it);
			return;
		}
		// Otherwise stream a zip. Folder keys are passed through as
		// trailing-slash prefixes; the server expands them recursively.
		const keys = selected.map((k) => s3Prefix() + k);
		window.location.href = api.zipDownloadURL(backend.id, bucket, keys);
	}
</script>

<input bind:this={fileInput} type="file" multiple hidden onchange={onFileChange} />
<input
	bind:this={folderInput}
	type="file"
	multiple
	hidden
	onchange={onFolderChange}
	{...{ webkitdirectory: true, directory: true }}
/>

<div
	class="relative flex h-full min-h-0 flex-col"
	ondragenter={(e) => {
		if (canWrite) {
			e.preventDefault();
			dragOver = true;
		}
	}}
	ondragover={(e) => {
		if (canWrite) e.preventDefault();
	}}
	ondragleave={(e) => {
		if (e.target === e.currentTarget) dragOver = false;
	}}
	ondrop={onDrop}
	role="region"
	aria-label="Object listing"
>
	<!-- Toolbar -->
	<div
		class="flex min-h-[44px] flex-shrink-0 flex-wrap items-center gap-2.5 border-b border-stw-border bg-stw-bg-panel px-3.5 py-1.5"
	>
		<div class="flex items-center gap-1.5 text-[12.5px] text-stw-fg-mute">
			<BackendMark {kind} size={14} />
			<span class="font-mono">{items.length}</span>
			<span>{filter ? 'matches' : 'items'} ·</span>
			<span class="font-mono">{bytes(totalSize)}</span>
			{#if filter && items.length !== allItems.length}
				<span class="text-[11.5px] text-stw-fg-soft">of {allItems.length}</span>
			{/if}
			{#if !backend.capabilities.versioning}
				<span class="px-1 text-stw-fg-soft">·</span>
				<Tooltip text="This backend doesn't expose versioning">
					<span class="inline-flex items-center gap-1 text-[11.5px] text-stw-fg-soft">
						<Info size={11} strokeWidth={1.7} /> versioning unavailable
					</span>
				</Tooltip>
			{/if}
			{#if listing?.is_truncated}
				<span class="text-[11.5px] text-stw-warn">· listing truncated</span>
			{/if}
		</div>
		<span class="flex-1"></span>
		{#if selected.length > 0}
			<div class="flex items-center gap-2 text-[12.5px] text-stw-fg">
				<span class="font-medium">{selected.length} selected</span>
				{#snippet downloadIcon()}<Download size={12} strokeWidth={1.7} />{/snippet}
				{#snippet trashIcon()}<Trash2 size={12} strokeWidth={1.7} />{/snippet}
				{#snippet moveIcon()}<FolderInput size={12} strokeWidth={1.7} />{/snippet}
				<Button size="sm" icon={downloadIcon} onclick={downloadSelected}>
					{selected.length > 1 || selected.some((k) => k.endsWith('/'))
						? 'Download .zip'
						: 'Download'}
				</Button>
				{#if canWrite}
					<Button size="sm" icon={moveIcon} onclick={() => openMove(selected, 'copy')}>
						Move / Copy
					</Button>
					<Button size="sm" variant="danger" icon={trashIcon} onclick={deleteSelected}>
						Delete
					</Button>
				{/if}
				<button
					type="button"
					onclick={() => (selected = [])}
					class="cursor-pointer border-0 bg-transparent p-1 text-stw-fg-mute hover:text-stw-fg"
					aria-label="Clear selection"
				>
					<X size={13} strokeWidth={1.7} />
				</button>
			</div>
		{:else}
			<SearchField
				bind:value={filter}
				placeholder="Filter in this view"
				size="sm"
				width="180px"
				onkeydown={(e) => {
					if (e.key === 'Escape') filter = '';
				}}
			/>
			{#snippet refreshIcon()}<RotateCw size={12} strokeWidth={1.7} />{/snippet}
			<Tooltip text="Refetch listing and rescan bucket usage">
				<Button size="sm" icon={refreshIcon} onclick={refreshAll} disabled={refreshing}>
					{refreshing ? 'Refreshing…' : 'Refresh'}
				</Button>
			</Tooltip>
			{#if canWrite}
				{#snippet plusIcon()}<Plus size={12} strokeWidth={1.7} />{/snippet}
				<Button size="sm" icon={plusIcon} onclick={() => (creatingFolder = true)}>
					New folder
				</Button>
				{#snippet uploadIcon()}<Upload size={12} strokeWidth={1.7} />{/snippet}
				{#snippet folderUploadIcon()}<Folder size={12} strokeWidth={1.7} />{/snippet}
				<Button size="sm" icon={folderUploadIcon} onclick={onPickFolder}>Upload folder</Button>
				<Button size="sm" variant="primary" icon={uploadIcon} onclick={onPickFiles}>Upload</Button>
			{/if}
		{/if}
	</div>

	{#if quotaBanner}
		<div class="flex-shrink-0 border-b border-stw-border">
			<Banner
				variant={quotaBanner.tone === 'danger' ? 'err' : 'warn'}
				title={quotaBanner.title}
				role="status"
			>
				{quotaBanner.hint}
				{#snippet actions()}
					{#if isAdmin}
						<button
							type="button"
							onclick={() => goto(`/b/${backend.id}/${bucket}/settings`)}
							class="cursor-pointer rounded-[5px] border border-stw-border bg-transparent px-2 py-1 text-[11.5px] text-stw-fg focus-ring hover:bg-stw-bg-hover"
						>
							Manage quota
						</button>
					{/if}
				{/snippet}
			</Banner>
		</div>
	{/if}

	{#if creatingFolder}
		<form
			onsubmit={submitFolder}
			class="flex items-center gap-2 border-b border-stw-border bg-stw-bg-sunken px-3.5 py-2.5"
		>
			<input
				class="stw-input h-[30px] flex-1 font-mono text-[13px]"
				placeholder="folder-name"
				bind:value={folderName}
				required
			/>
			<Button type="submit" variant="primary" size="sm">Create</Button>
			<Button
				size="sm"
				variant="ghost"
				onclick={() => {
					creatingFolder = false;
					folderName = '';
				}}
			>
				Cancel
			</Button>
		</form>
	{/if}

	<!-- Body -->
	<div class="relative flex min-h-0 flex-1">
		{#if error}
			<EmptyState title="Couldn't list objects." hint={error} />
		{:else if items.length === 0 && filter}
			{#snippet searchIcon()}<Search size={22} strokeWidth={1.7} />{/snippet}
			{#snippet clearAction()}
				<Button variant="ghost" onclick={() => (filter = '')}>Clear filter</Button>
			{/snippet}
			<EmptyState
				icon={searchIcon}
				title="No matches."
				hint={`No items in this view contain "${filter}".`}
				action={clearAction}
			/>
		{:else if items.length === 0}
			{#snippet folderIcon()}<Folder size={22} strokeWidth={1.7} />{/snippet}
			{#snippet uploadAction()}
				{#snippet uploadIcon()}<Upload size={13} strokeWidth={1.7} />{/snippet}
				{#if canWrite}
					<Button variant="primary" icon={uploadIcon} onclick={onPickFiles}>Upload files</Button>
				{/if}
			{/snippet}
			<EmptyState
				icon={folderIcon}
				title="No objects yet."
				hint={canWrite ? 'Drop files anywhere, or use the upload button.' : 'This bucket is empty.'}
				action={uploadAction}
			/>
		{:else}
			<ObjectTable
				{items}
				{selected}
				{folderSizes}
				setSelected={(s) => (selected = s)}
				onopen={openItem}
				onshare={(it) => onshare(it)}
				onpreview={openItem}
				ondownload={downloadOne}
				onFolderVisible={loadOneFolderSize}
			/>
		{/if}

		{#if detail}
			<DetailDrawer
				item={detail}
				onclose={() => (detail = null)}
				onshare={(it) => onshare(it)}
				ondelete={canWrite ? deleteOne : undefined}
				onrename={canWrite ? renameOne : undefined}
				onmove={canWrite ? (it) => openMove([it.key], 'copy') : undefined}
				ondownload={downloadOne}
				backend={backend.id}
				{bucket}
				{prefix}
				capabilities={backend.capabilities}
				canEdit={canWrite}
			/>
		{/if}
	</div>

	{#if moveDialog}
		<MoveDialog
			backendId={backend.id}
			srcBucket={bucket}
			srcPrefix={s3Prefix()}
			keys={moveDialog.keys}
			backends={page.data.backends ?? [backend]}
			defaultOperation={moveDialog.defaultOperation}
			onclose={() => (moveDialog = null)}
			oncomplete={onMoveComplete}
		/>
	{/if}

	{#if confirmDelete}
		<ConfirmDialog
			title={confirmDelete.title}
			description={confirmDelete.description}
			variant="danger"
			confirmLabel="Delete"
			busy={confirmDelete.busy}
			onconfirm={confirmDelete.onconfirm}
			oncancel={() => (confirmDelete = null)}
		/>
	{/if}

	{#if pendingConflicts.length > 0}
		{@const names = pendingConflicts.map((u) => u.name)}
		<ConfirmDialog
			title={pendingConflicts.length === 1
				? 'Replace existing file?'
				: `Replace ${pendingConflicts.length} existing files?`}
			variant="danger"
			confirmLabel={pendingConflicts.length === 1 ? 'Replace' : 'Replace all'}
			cancelLabel={pendingConflicts.length === 1 ? 'Skip' : 'Skip all'}
			onconfirm={() => resolveAllConflicts('replace')}
			oncancel={() => resolveAllConflicts('skip')}
		>
			{#snippet body()}
				<div class="mb-2">
					{pendingConflicts.length === 1
						? 'A file with this name already exists in this location.'
						: 'These files already exist in this location:'}
				</div>
				<ul class="m-0 max-h-[160px] overflow-auto pl-[18px] font-mono text-[12px] text-stw-fg">
					{#each names.slice(0, 8) as n (n)}
						<li class="truncate">{n}</li>
					{/each}
					{#if names.length > 8}
						<li class="text-stw-fg-soft italic">
							…and {names.length - 8} more
						</li>
					{/if}
				</ul>
			{/snippet}
		</ConfirmDialog>
	{/if}

	<!-- Drop overlay -->
	{#if dragOver}
		<div
			class="pointer-events-none absolute inset-0 z-[5] flex items-center justify-center border-2 border-dashed border-stw-accent-500 stw-drag-tint"
		>
			<div
				class="flex items-center gap-2.5 rounded-[10px] bg-stw-bg-panel px-[22px] py-4 shadow-stw-lg"
			>
				<Upload size={18} strokeWidth={1.7} />
				<div>
					<div class="text-[14px] font-semibold">Drop to upload</div>
					<div class="font-mono text-[12px] text-stw-fg-mute">
						→ {backend.id}/{bucket}/{s3Prefix()}
					</div>
				</div>
			</div>
		</div>
	{/if}
</div>
