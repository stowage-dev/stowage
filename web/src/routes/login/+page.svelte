<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { toast } from 'svelte-sonner';
	import { TriangleAlert, Shield, Eye, EyeOff, User, Lock } from 'lucide-svelte';
	import AuthCard from '$lib/components/auth/AuthCard.svelte';
	import { api, ApiException } from '$lib/api';
	import { session, setSession } from '$lib/stores/session.svelte';
	import { theme } from '$lib/stores/theme.svelte';
	import '$lib/styles/auth.css';

	const cfg = $derived(session.authConfig);
	const hasOIDC = $derived(cfg?.modes.includes('oidc') ?? false);
	const hasLocal = $derived(
		(cfg?.modes.includes('local') ?? false) || (cfg?.modes.includes('static') ?? false)
	);
	const showStaticBanner = $derived(cfg?.modes.includes('static') ?? false);

	let username = $state('');
	let password = $state('');
	let submitting = $state(false);
	let showLocal = $state(false);
	let showPassword = $state(false);
	let formError = $state<string | null>(null);

	$effect(() => {
		// If OIDC isn't an option, jump straight to the local form.
		if (!hasOIDC && hasLocal) showLocal = true;
	});

	async function submitLocal(e: SubmitEvent) {
		e.preventDefault();
		if (submitting) return;
		submitting = true;
		formError = null;
		try {
			const res = await api.loginLocal(username, password);
			const me = await api.me();
			if (cfg) setSession(cfg, me);
			const next = page.url.searchParams.get('next') || '/';
			if (res.must_change_pw) {
				goto('/me/password?must=1', { replaceState: true });
			} else {
				goto(next, { replaceState: true });
			}
		} catch (err) {
			const msg = err instanceof ApiException ? err.message : 'Sign-in failed.';
			formError = msg;
			toast.error(msg);
		} finally {
			submitting = false;
		}
	}

	function startOIDC() {
		const next = page.url.searchParams.get('next') || '/';
		window.location.href = api.loginOIDCStartURL() + '?next=' + encodeURIComponent(next);
	}
</script>

<svelte:head>
	<title>Sign in · stowage</title>
</svelte:head>

<div class="lg" class:dark={theme.value === 'dark'}>
	<div class="auth-stack">
		{#if showStaticBanner}
			<div class="warn-banner" role="status">
				<TriangleAlert size={14} strokeWidth={1.7} />
				<span>Static admin enabled — disable in production.</span>
			</div>
		{/if}

		<AuthCard
			title="Sign in to Stowage"
			sub="Welcome back. Enter your credentials to access your buckets."
		>
			{#if formError}
				<div class="err-banner" role="alert">
					<svg
						width="14"
						height="14"
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="2.2"
						stroke-linecap="round"
						stroke-linejoin="round"
					>
						<circle cx="12" cy="12" r="10" />
						<line x1="12" y1="8" x2="12" y2="12" />
						<line x1="12" y1="16" x2="12.01" y2="16" />
					</svg>
					<span>{formError}</span>
				</div>
			{/if}

			{#if hasOIDC}
				<button type="button" class="btn btn-primary" onclick={startOIDC}>
					<Shield size={16} strokeWidth={1.7} /> Continue with SSO
				</button>
			{/if}

			{#if hasOIDC && hasLocal && !showLocal}
				<button type="button" class="link-btn" onclick={() => (showLocal = true)}>
					Sign in with username →
				</button>
			{/if}

			{#if showLocal && hasLocal}
				{#if hasOIDC}
					<div class="auth-divider"><span>or</span></div>
				{/if}

				<form onsubmit={submitLocal} novalidate>
					<div class="field">
						<label for="login-username">Username</label>
						<span class="prefix" aria-hidden="true">
							<User size={14} strokeWidth={1.8} />
						</span>
						<input
							id="login-username"
							class="input with-prefix"
							placeholder="username"
							autocomplete="username"
							bind:value={username}
							required
						/>
					</div>

					<div class="field">
						<label for="login-password">Password</label>
						<span class="prefix" aria-hidden="true">
							<Lock size={14} strokeWidth={1.8} />
						</span>
						<input
							id="login-password"
							class="input with-prefix"
							type={showPassword ? 'text' : 'password'}
							autocomplete="current-password"
							placeholder="••••••••••"
							bind:value={password}
							required
						/>
						<button
							type="button"
							class="suffix-btn"
							aria-label={showPassword ? 'Hide password' : 'Show password'}
							onclick={() => (showPassword = !showPassword)}
						>
							{#if showPassword}
								<EyeOff size={14} strokeWidth={1.8} />
							{:else}
								<Eye size={14} strokeWidth={1.8} />
							{/if}
						</button>
					</div>

					<button
						type="submit"
						class="btn btn-primary"
						class:loading={submitting}
						disabled={submitting}
					>
						{#if submitting}
							<span class="spinner" aria-hidden="true"></span>Signing in…
						{:else}
							Sign in
						{/if}
					</button>
				</form>
			{/if}

			{#if !hasLocal && !hasOIDC && cfg}
				<p class="sub" style="text-align:center;margin:0;">
					No sign-in methods are enabled. Check the server config.
				</p>
			{/if}

			{#snippet footer()}
				<div class="legal">stowage v1.0</div>
			{/snippet}
		</AuthCard>
	</div>
</div>
