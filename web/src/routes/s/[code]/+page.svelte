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
				<div style="flex:1;min-width:0;">
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

<style>
	.stw-share-page {
		min-height: 100vh;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		padding: 32px 16px;
		gap: 16px;
		background: var(--stw-bg);
		color: var(--stw-fg);
	}
	.stw-share-card {
		width: 100%;
		max-width: 720px;
		background: var(--stw-bg-panel);
		border: 1px solid var(--stw-border);
		border-radius: 14px;
		box-shadow: var(--stw-shadow-lg);
		overflow: hidden;
	}
	.stw-share-status {
		padding: 56px 24px;
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;
		gap: 12px;
	}
	.stw-share-status h1 {
		font-size: 18px;
		margin: 0;
		color: var(--stw-fg);
	}
	.stw-share-spinner {
		width: 22px;
		height: 22px;
		border: 2px solid var(--stw-border);
		border-top-color: var(--stw-accent-500);
		border-radius: 50%;
		animation: stw-spin 800ms linear infinite;
	}
	@keyframes stw-spin {
		to {
			transform: rotate(360deg);
		}
	}
	.stw-share-pw {
		padding: 36px 28px 28px;
		display: flex;
		flex-direction: column;
		align-items: stretch;
		gap: 12px;
		max-width: 380px;
		margin: 0 auto;
	}
	.stw-share-pw .stw-share-icon {
		align-self: center;
	}
	.stw-share-pw h1 {
		text-align: center;
		font-size: 18px;
		margin: 4px 0 0;
	}
	.stw-share-pw p {
		text-align: center;
		margin: 0 0 8px;
	}
	.stw-share-pw-input {
		position: relative;
		display: flex;
	}
	.stw-share-pw-input input {
		flex: 1;
		padding: 10px 36px 10px 12px;
		background: var(--stw-bg-sunken);
		border: 1px solid var(--stw-border);
		border-radius: 8px;
		color: var(--stw-fg);
		font: inherit;
		font-size: 14px;
		outline: none;
	}
	.stw-share-pw-input input:focus {
		border-color: var(--stw-accent-500);
	}
	.stw-share-eye {
		position: absolute;
		right: 4px;
		top: 4px;
		width: 30px;
		height: 30px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		background: transparent;
		border: 0;
		border-radius: 6px;
		cursor: pointer;
		color: var(--stw-fg-mute);
	}
	.stw-share-err {
		color: var(--stw-err);
		font-size: 13px;
		margin: 0;
		text-align: center;
	}
	.stw-share-mute {
		color: var(--stw-fg-mute);
		font-size: 13px;
		margin: 4px 0 0;
	}
	.stw-share-icon {
		width: 36px;
		height: 36px;
		flex-shrink: 0;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		background: color-mix(in oklch, var(--stw-accent-500) 12%, var(--stw-bg-panel));
		border: 1px solid color-mix(in oklch, var(--stw-accent-500) 25%, var(--stw-border));
		border-radius: 8px;
		color: var(--stw-accent-600);
	}
	.stw-share-head {
		padding: 18px 22px;
		display: flex;
		gap: 12px;
		align-items: center;
		border-bottom: 1px solid var(--stw-border);
	}
	.stw-share-head h1 {
		margin: 0;
		font-size: 17px;
		font-weight: 600;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.stw-share-head p {
		font-family: var(--stw-font-mono);
		font-size: 12px;
	}
	.stw-share-preview {
		background: var(--stw-bg-sunken);
		display: flex;
		align-items: center;
		justify-content: center;
		min-height: 200px;
		max-height: min(70vh, 640px);
		overflow: hidden;
	}
	.stw-share-preview img,
	.stw-share-preview video {
		max-width: 100%;
		max-height: min(70vh, 640px);
		display: block;
	}
	.stw-share-audio {
		padding: 32px;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 14px;
		color: var(--stw-fg-mute);
	}
	.stw-share-audio audio {
		width: min(100%, 420px);
	}
	.stw-share-iframe {
		width: 100%;
		height: 70vh;
		max-height: 640px;
		border: 0;
		background: var(--stw-bg);
	}
	.stw-share-iframe--text {
		background: #fff;
	}
	.stw-share-noprev {
		padding: 56px 24px;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 10px;
		color: var(--stw-fg-mute);
		font-size: 13px;
		text-align: center;
	}
	.stw-share-foot {
		padding: 14px 22px;
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
		border-top: 1px solid var(--stw-border);
	}
	.stw-share-meta {
		flex: 1;
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		min-width: 0;
	}
	.stw-share-meta-pill {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 3px 8px;
		background: var(--stw-bg-sunken);
		border: 1px solid var(--stw-border);
		border-radius: 4px;
		font-size: 11.5px;
		color: var(--stw-fg-mute);
	}
	.stw-share-cta {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
		padding: 10px 16px;
		background: var(--stw-accent-500);
		color: #fff;
		border: 0;
		border-radius: 8px;
		font: inherit;
		font-size: 13px;
		font-weight: 600;
		cursor: pointer;
		text-decoration: none;
		transition: background 120ms;
	}
	.stw-share-cta:hover {
		background: var(--stw-accent-600);
	}
	.stw-share-cta:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}
	.stw-share-brand {
		font-size: 11px;
		color: var(--stw-fg-soft);
		margin: 0;
	}
</style>
