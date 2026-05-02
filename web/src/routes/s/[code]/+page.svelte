<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import {
		Download,
		Lock,
		Eye,
		EyeOff,
		FileText,
		Image as ImageIcon,
		Film,
		Music,
		File,
		Clock
	} from 'lucide-svelte';
	import { bytes } from '$lib/format';

	type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'none';

	interface ShareInfo {
		code: string;
		name: string;
		size: number;
		content_type?: string;
		etag?: string;
		last_modified?: string;
		expires_at?: string;
		has_password: boolean;
		max_downloads?: number;
		download_count: number;
		downloads_left?: number;
		disposition: 'attachment' | 'inline';
		preview_kind: PreviewKind;
		raw_url: string;
	}

	type ErrorCode = 'not_found' | 'revoked' | 'expired' | 'exhausted' | 'backend_error' | 'unknown';

	type Phase =
		| { kind: 'loading' }
		| { kind: 'password'; error?: string }
		| { kind: 'ready'; info: ShareInfo }
		| { kind: 'error'; code: ErrorCode; message: string };

	const code = $derived(page.params.code);

	let phase = $state<Phase>({ kind: 'loading' });
	let password = $state('');
	let showPassword = $state(false);
	let unlocking = $state(false);

	async function loadInfo(): Promise<void> {
		phase = { kind: 'loading' };
		try {
			const res = await fetch(`/s/${code}/info`, { credentials: 'same-origin' });
			if (res.ok) {
				const info = (await res.json()) as ShareInfo;
				phase = { kind: 'ready', info };
				return;
			}
			const data = (await res.json().catch(() => ({}))) as {
				error?: { code?: string; message?: string };
			};
			const errCode = data.error?.code ?? '';
			if (res.status === 401 && errCode === 'password_required') {
				phase = { kind: 'password' };
				return;
			}
			phase = { kind: 'error', ...mapError(errCode, data.error?.message ?? 'Share unavailable') };
		} catch (err) {
			phase = {
				kind: 'error',
				code: 'unknown',
				message: err instanceof Error ? err.message : 'Could not load share.'
			};
		}
	}

	function mapError(code: string, fallback: string): { code: ErrorCode; message: string } {
		switch (code) {
			case 'not_found':
				return { code: 'not_found', message: 'This share link does not exist.' };
			case 'revoked':
				return { code: 'revoked', message: 'This share link has been revoked.' };
			case 'expired':
				return { code: 'expired', message: 'This share link has expired.' };
			case 'exhausted':
				return {
					code: 'exhausted',
					message: 'This share link has hit its download limit.'
				};
			case 'backend_error':
				return { code: 'backend_error', message: 'The shared file is temporarily unavailable.' };
			default:
				return { code: 'unknown', message: fallback };
		}
	}

	async function unlock(): Promise<void> {
		if (unlocking || !password) return;
		unlocking = true;
		try {
			const res = await fetch(`/s/${code}/unlock`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				credentials: 'same-origin',
				body: JSON.stringify({ password })
			});
			if (res.ok) {
				const info = (await res.json()) as ShareInfo;
				phase = { kind: 'ready', info };
				password = '';
				return;
			}
			const data = (await res.json().catch(() => ({}))) as {
				error?: { code?: string; message?: string };
			};
			const errCode = data.error?.code ?? '';
			if (res.status === 401) {
				phase = { kind: 'password', error: 'Wrong password. Try again.' };
				return;
			}
			phase = { kind: 'error', ...mapError(errCode, data.error?.message ?? 'Could not unlock.') };
		} catch (err) {
			phase = {
				kind: 'password',
				error: err instanceof Error ? err.message : 'Could not unlock.'
			};
		} finally {
			unlocking = false;
		}
	}

	function expiryLabel(info: ShareInfo): string | null {
		if (!info.expires_at) return null;
		const ms = new Date(info.expires_at).getTime() - Date.now();
		if (ms <= 0) return 'Expired';
		const day = 24 * 60 * 60 * 1000;
		if (ms < 60 * 60 * 1000) return `Expires in ${Math.max(1, Math.round(ms / 60000))} minutes`;
		if (ms < day) return `Expires in ${Math.round(ms / (60 * 60 * 1000))} hours`;
		if (ms < 30 * day) return `Expires in ${Math.round(ms / day)} days`;
		return `Expires ${new Date(info.expires_at).toLocaleDateString()}`;
	}

	function inlineURL(info: ShareInfo): string {
		// `inline=1` overrides the share's stored disposition so the browser
		// renders the bytes inside <img>/<video>/<iframe> rather than
		// triggering a download.
		return `${info.raw_url}?inline=1`;
	}

	onMount(loadInfo);
</script>

<svelte:head>
	<title>
		{phase.kind === 'ready' ? phase.info.name + ' · Stowage' : 'Stowage share'}
	</title>
</svelte:head>

<div class="stw-share-page">
	<div class="stw-share-card">
		{#if phase.kind === 'loading'}
			<div class="stw-share-status">
				<div class="stw-share-spinner" aria-label="Loading"></div>
				<p>Loading share…</p>
			</div>
		{:else if phase.kind === 'password'}
			<form
				class="stw-share-pw"
				onsubmit={(e) => {
					e.preventDefault();
					void unlock();
				}}
			>
				<div class="stw-share-icon"><Lock size={20} strokeWidth={1.7} /></div>
				<h1>Password required</h1>
				<p class="stw-share-mute">
					This file is protected. Enter the password the sender shared with you.
				</p>
				<div class="stw-share-pw-input">
					<!-- svelte-ignore a11y_autofocus -->
					<input
						type={showPassword ? 'text' : 'password'}
						placeholder="Password"
						autocomplete="current-password"
						autofocus
						bind:value={password}
						disabled={unlocking}
					/>
					<button
						type="button"
						class="stw-share-eye"
						aria-label={showPassword ? 'Hide password' : 'Show password'}
						onclick={() => (showPassword = !showPassword)}
					>
						{#if showPassword}
							<EyeOff size={14} strokeWidth={1.7} />
						{:else}
							<Eye size={14} strokeWidth={1.7} />
						{/if}
					</button>
				</div>
				{#if phase.error}
					<p class="stw-share-err">{phase.error}</p>
				{/if}
				<button class="stw-share-cta" type="submit" disabled={unlocking || !password}>
					{unlocking ? 'Unlocking…' : 'Unlock'}
				</button>
			</form>
		{:else if phase.kind === 'error'}
			<div class="stw-share-status">
				<h1>{phase.message}</h1>
				<p class="stw-share-mute">If you think this is wrong, ask whoever sent the link.</p>
			</div>
		{:else}
			{@const info = phase.info}
			{@const expires = expiryLabel(info)}
			<header class="stw-share-head">
				<div class="stw-share-icon">
					{#if info.preview_kind === 'image'}
						<ImageIcon size={20} strokeWidth={1.7} />
					{:else if info.preview_kind === 'video'}
						<Film size={20} strokeWidth={1.7} />
					{:else if info.preview_kind === 'audio'}
						<Music size={20} strokeWidth={1.7} />
					{:else if info.preview_kind === 'text' || info.preview_kind === 'pdf'}
						<FileText size={20} strokeWidth={1.7} />
					{:else}
						<File size={20} strokeWidth={1.7} />
					{/if}
				</div>
				<div class="min-w-0 flex-1">
					<h1 title={info.name}>{info.name}</h1>
					<p class="stw-share-mute">
						{bytes(info.size)}{#if info.content_type}
							· <span>{info.content_type}</span>{/if}
					</p>
				</div>
			</header>

			<div class="stw-share-preview">
				{#if info.max_downloads}
					<!-- Each preview tag (img / video / iframe) issues its own
					     /raw request, which would consume the share's download
					     budget. Skip the preview entirely on quota-limited
					     links so the recipient gets exactly one download. -->
					<div class="stw-share-noprev">
						<File size={36} strokeWidth={1.5} />
						<p>
							Preview disabled for download-limited shares — each preview would consume one of the
							{info.max_downloads}
							download{info.max_downloads === 1 ? '' : 's'}. Click <strong>Download</strong> below.
						</p>
					</div>
				{:else if info.preview_kind === 'image'}
					<img src={inlineURL(info)} alt={info.name} />
				{:else if info.preview_kind === 'video'}
					<video controls preload="metadata" src={inlineURL(info)}>
						<track kind="captions" />
					</video>
				{:else if info.preview_kind === 'audio'}
					<div class="stw-share-audio">
						<Music size={36} strokeWidth={1.5} />
						<audio controls src={inlineURL(info)}></audio>
					</div>
				{:else if info.preview_kind === 'pdf'}
					<iframe title={info.name} src={inlineURL(info)} class="stw-share-iframe"></iframe>
				{:else if info.preview_kind === 'text' && info.size <= 256 * 1024}
					<iframe
						title={info.name}
						src={inlineURL(info)}
						class="stw-share-iframe stw-share-iframe--text"
					></iframe>
				{:else}
					<div class="stw-share-noprev">
						<File size={36} strokeWidth={1.5} />
						<p>Preview not available — click <strong>Download</strong> to fetch the file.</p>
					</div>
				{/if}
			</div>

			<footer class="stw-share-foot">
				<div class="stw-share-meta">
					{#if expires}
						<span class="stw-share-meta-pill">
							<Clock size={11} strokeWidth={1.7} />
							{expires}
						</span>
					{/if}
					{#if info.max_downloads}
						<span class="stw-share-meta-pill">
							{info.downloads_left ?? Math.max(0, info.max_downloads - info.download_count)} of {info.max_downloads}
							downloads left
						</span>
					{/if}
				</div>
				<a class="stw-share-cta" href={info.raw_url} download={info.name}>
					<Download size={14} strokeWidth={1.7} />
					Download
				</a>
			</footer>
		{/if}
	</div>
	<p class="stw-share-brand">Shared via Stowage</p>
</div>
