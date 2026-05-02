<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { onMount, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { invalidateAll } from '$app/navigation';
	import { Save, Trash2, Plus, Globe, RotateCw, Database, FolderTree } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import Toggle from '$lib/components/ui/Toggle.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import SectionCard from '$lib/components/ui/SectionCard.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import QuotaMeter from '$lib/components/ui/QuotaMeter.svelte';
	import { api, ApiException } from '$lib/api';
	import { bytes as fmtBytes } from '$lib/format';
	import { session } from '$lib/stores/session.svelte';
	import type { BucketQuota, CORSRule, LifecycleRule } from '$lib/types';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	const backendId = untrack(() => data.backend?.id ?? '');
	const bucket = untrack(() => data.bucket);
	const caps = untrack(() => data.backend?.capabilities);
	const isAdmin = $derived(session.me?.role === 'admin');

	let versioning = $state(false);
	let versioningReady = $state(false);
	let versioningError = $state<string | null>(null);

	let corsJSON = $state('');
	let corsReady = $state(false);
	let corsError = $state<string | null>(null);

	let policy = $state<string | null>(null);
	let policyJSON = $state('');
	let policyReady = $state(false);
	let policyError = $state<string | null>(null);

	let lifecycle = $state<LifecycleRule[]>([]);
	let lifecycleReady = $state(false);
	let lifecycleError = $state<string | null>(null);

	let quotaState = $state<BucketQuota | null>(null);
	let softInput = $state('');
	let hardInput = $state('');
	let quotaReady = $state(false);
	let quotaError = $state<string | null>(null);

	let sizeTracking = $state(true);
	let sizeTrackingReady = $state(false);
	let sizeTrackingError = $state<string | null>(null);

	const publicReadCanned = $derived(isCannedPublicRead(policy ?? '', bucket));

	onMount(() => {
		void data.versioning.then((r) => {
			versioning = r.value ?? false;
			versioningError = r.error;
			versioningReady = true;
		});
		void data.cors.then((r) => {
			corsJSON = JSON.stringify(r.value ?? [], null, 2);
			corsError = r.error;
			corsReady = true;
		});
		void data.policy.then((r) => {
			policy = r.value;
			policyJSON = prettyPolicy(r.value);
			policyError = r.error;
			policyReady = true;
		});
		void data.lifecycle.then((r) => {
			lifecycle = structuredClone(r.value ?? []);
			lifecycleError = r.error;
			lifecycleReady = true;
		});
		void data.quota.then((r) => {
			quotaState = r.value;
			softInput = bytesToHuman(r.value?.soft_bytes ?? 0);
			hardInput = bytesToHuman(r.value?.hard_bytes ?? 0);
			quotaError = r.error;
			quotaReady = true;
		});
		void data.sizeTracking.then((r) => {
			sizeTracking = r.value?.enabled ?? true;
			sizeTrackingError = r.error;
			sizeTrackingReady = true;
		});
	});

	let saving = $state<Record<string, boolean>>({});

	function prettyPolicy(p: string | null | undefined): string {
		if (!p) return '';
		try {
			return JSON.stringify(JSON.parse(p), null, 2);
		} catch {
			return p;
		}
	}

	function isCannedPublicRead(policy: string, bucket: string): boolean {
		if (!policy) return false;
		try {
			const p = JSON.parse(policy) as {
				Statement?: Array<{
					Effect?: string;
					Principal?: unknown;
					Action?: string | string[];
					Resource?: string | string[];
				}>;
			};
			if (!Array.isArray(p.Statement) || p.Statement.length !== 1) return false;
			const s = p.Statement[0];
			if (s.Effect !== 'Allow' || s.Principal !== '*') return false;
			const acts = Array.isArray(s.Action) ? s.Action : [s.Action];
			if (!acts.includes('s3:GetObject')) return false;
			const res = Array.isArray(s.Resource) ? s.Resource : [s.Resource];
			return res.includes(`arn:aws:s3:::${bucket}/*`);
		} catch {
			return false;
		}
	}

	async function saveSizeTracking(value: boolean): Promise<void> {
		saving.sizeTracking = true;
		try {
			const r = await api.setBucketSizeTracking(backendId, bucket, value);
			sizeTracking = r.enabled;
			toast.success(value ? 'Size tracking enabled' : 'Size tracking disabled');
			await invalidateAll();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not save setting.');
		} finally {
			saving.sizeTracking = false;
		}
	}

	async function saveVersioning(value: boolean): Promise<void> {
		saving.versioning = true;
		try {
			await api.setBucketVersioning(backendId, bucket, value);
			versioning = value;
			toast.success(`Versioning ${value ? 'enabled' : 'suspended'}`);
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not save versioning.');
		} finally {
			saving.versioning = false;
		}
	}

	async function togglePublicRead(value: boolean): Promise<void> {
		saving.public = true;
		try {
			await api.setBucketPublicRead(backendId, bucket, value);
			toast.success(value ? 'Bucket is now publicly readable.' : 'Public-read policy removed.');
			await invalidateAll();
			const fresh = await api.getBucketPolicy(backendId, bucket);
			policy = fresh;
			policyJSON = prettyPolicy(fresh);
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not update public access.');
		} finally {
			saving.public = false;
		}
	}

	async function saveCORS(): Promise<void> {
		let parsed: CORSRule[];
		try {
			const v = JSON.parse(corsJSON || '[]') as unknown;
			if (!Array.isArray(v)) throw new Error('CORS must be an array of rules.');
			parsed = v as CORSRule[];
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'CORS JSON is invalid.');
			return;
		}
		saving.cors = true;
		try {
			await api.setBucketCORS(backendId, bucket, parsed);
			toast.success('CORS rules saved');
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not save CORS.');
		} finally {
			saving.cors = false;
		}
	}

	async function savePolicy(): Promise<void> {
		const v = policyJSON.trim();
		if (!v) {
			if (!confirm('Delete the bucket policy?')) return;
			saving.policy = true;
			try {
				await api.deleteBucketPolicy(backendId, bucket);
				policy = '';
				toast.success('Policy deleted');
				await invalidateAll();
			} catch (err) {
				toast.error(err instanceof ApiException ? err.message : 'Could not delete policy.');
			} finally {
				saving.policy = false;
			}
			return;
		}
		try {
			JSON.parse(v);
		} catch (err) {
			toast.error('Policy is not valid JSON: ' + (err instanceof Error ? err.message : ''));
			return;
		}
		saving.policy = true;
		try {
			await api.setBucketPolicy(backendId, bucket, v);
			policy = v;
			toast.success('Policy saved');
			await invalidateAll();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not save policy.');
		} finally {
			saving.policy = false;
		}
	}

	function addLifecycleRule(): void {
		lifecycle.push({
			id: '',
			prefix: '',
			enabled: true,
			expiration_days: 30
		});
	}

	function removeLifecycleRule(i: number): void {
		lifecycle.splice(i, 1);
	}

	function bytesToHuman(n: number): string {
		if (!n) return '';
		const units: Array<[number, string]> = [
			[1024 ** 4, 'TB'],
			[1024 ** 3, 'GB'],
			[1024 ** 2, 'MB'],
			[1024, 'KB']
		];
		for (const [base, unit] of units) {
			if (n >= base && n % base === 0) return `${n / base} ${unit}`;
		}
		return `${n}`;
	}

	function humanToBytes(s: string): number {
		const trimmed = s.trim();
		if (!trimmed) return 0;
		const m = trimmed.match(/^([\d.]+)\s*(B|KB|MB|GB|TB)?$/i);
		if (!m) return NaN;
		const n = parseFloat(m[1]);
		if (Number.isNaN(n)) return NaN;
		const unit = (m[2] ?? 'B').toUpperCase();
		const mul =
			unit === 'TB'
				? 1024 ** 4
				: unit === 'GB'
					? 1024 ** 3
					: unit === 'MB'
						? 1024 ** 2
						: unit === 'KB'
							? 1024
							: 1;
		return Math.round(n * mul);
	}

	async function saveQuota(): Promise<void> {
		const soft = humanToBytes(softInput);
		const hard = humanToBytes(hardInput);
		if (Number.isNaN(soft) || Number.isNaN(hard)) {
			toast.error('Quota values must be numbers, optionally followed by KB/MB/GB/TB.');
			return;
		}
		if (soft === 0 && hard === 0) {
			toast.error('Set at least one of soft or hard quota — or use Clear to remove the quota.');
			return;
		}
		if (soft > 0 && hard > 0 && soft > hard) {
			toast.error('Soft quota must be less than or equal to hard quota.');
			return;
		}
		saving.quota = true;
		try {
			quotaState = await api.setBucketQuota(backendId, bucket, soft, hard);
			toast.success('Quota saved');
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not save quota.');
		} finally {
			saving.quota = false;
		}
	}

	async function clearQuota(): Promise<void> {
		if (!confirm('Remove the quota? Uploads will be unbounded after this.')) return;
		saving.quota = true;
		try {
			await api.deleteBucketQuota(backendId, bucket);
			quotaState = { configured: false, has_usage: quotaState?.has_usage ?? false };
			softInput = '';
			hardInput = '';
			toast.success('Quota cleared');
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not clear quota.');
		} finally {
			saving.quota = false;
		}
	}

	async function recomputeQuota(): Promise<void> {
		saving.quota = true;
		try {
			quotaState = await api.recomputeBucketQuota(backendId, bucket);
			toast.success('Recomputed');
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Recompute failed.');
		} finally {
			saving.quota = false;
		}
	}

	async function saveLifecycle(): Promise<void> {
		const rules: LifecycleRule[] = lifecycle
			.map((r) => ({
				...r,
				expiration_days: Number(r.expiration_days) || undefined,
				noncurrent_expire_days: Number(r.noncurrent_expire_days) || undefined,
				abort_incomplete_days: Number(r.abort_incomplete_days) || undefined,
				transition_days: Number(r.transition_days) || undefined
			}))
			.filter(
				(r) =>
					r.expiration_days ||
					r.noncurrent_expire_days ||
					r.abort_incomplete_days ||
					r.transition_days
			);

		saving.lifecycle = true;
		try {
			await api.setBucketLifecycle(backendId, bucket, rules);
			toast.success(`Saved ${rules.length} lifecycle rule${rules.length === 1 ? '' : 's'}`);
			await invalidateAll();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Could not save lifecycle.');
		} finally {
			saving.lifecycle = false;
		}
	}
</script>

<div class="mx-auto flex max-w-[880px] flex-col gap-[18px] stw-page-pad">
	<PageHeader title="Bucket settings" subtitle="{backendId}/{bucket}" />

	{#if !data.backend}
		<Banner variant="err">Backend not found.</Banner>
	{:else if !isAdmin}
		<Banner variant="err">Bucket settings are admin-only.</Banner>
	{:else}
		{#if caps?.versioning}
			<SectionCard>
				<div class="flex items-start gap-4">
					<div class="min-w-0 flex-1">
						<h2 class="m-0 mb-1 text-[14px] font-semibold">Versioning</h2>
						<p class="m-0 text-[12.5px] leading-[1.5] text-stw-fg-mute">
							When on, every overwrite or delete keeps the previous version of the object. Required
							for the version history panel.
						</p>
						{#if versioningError}
							<p class="m-0 mt-1 text-[12px] text-stw-err">{versioningError}</p>
						{/if}
					</div>
					{#if versioningReady}
						<Toggle value={versioning} onchange={(v) => saveVersioning(v)} />
					{:else}
						<Skeleton w={44} h={22} class="shrink-0 rounded-full" />
					{/if}
				</div>
			</SectionCard>
		{/if}

		<SectionCard>
			<div class="flex items-start gap-4">
				<div class="min-w-0 flex-1">
					<h2 class="m-0 mb-1 inline-flex items-center gap-2 text-[14px] font-semibold">
						<FolderTree size={14} strokeWidth={1.7} /> Bucket & folder size tracking
					</h2>
					<p class="m-0 text-[12.5px] leading-[1.5] text-stw-fg-mute">
						When on, the proxy computes recursive byte totals for this bucket and renders them next
						to the bucket name and each folder in the browser. Sizes refresh on a 30-minute schedule
						plus a 60-second cache for ad-hoc folder lookups. Turning this off skips the bucket
						entirely on the next scan and hides folder sizes in the browser.
					</p>
					{#if sizeTrackingError}
						<p class="m-0 mt-1 text-[12px] text-stw-err">{sizeTrackingError}</p>
					{/if}
				</div>
				{#if sizeTrackingReady}
					<Toggle
						value={sizeTracking}
						onchange={(v) => {
							if (saving.sizeTracking) return;
							void saveSizeTracking(v);
						}}
					/>
				{:else}
					<Skeleton w={44} h={22} class="shrink-0 rounded-full" />
				{/if}
			</div>
		</SectionCard>

		<SectionCard>
			<div class="flex flex-col gap-3">
				<div class="flex items-center gap-2.5">
					<Database size={14} strokeWidth={1.7} />
					<h2 class="m-0 text-[14px] font-semibold">Storage quota</h2>
				</div>
				<p class="m-0 text-[12.5px] leading-[1.5] text-stw-fg-mute">
					Proxy-enforced caps. Soft quota fires a warning banner in the object browser when usage
					exceeds it; hard quota rejects new uploads with HTTP 507 once full. Usage is sampled every
					30 minutes — use Recompute for an immediate refresh.
				</p>

				{#if !quotaReady}
					<div class="flex flex-col gap-1.5">
						<Skeleton h={8} class="rounded-full" />
						<div class="flex flex-wrap gap-3">
							<Skeleton w={120} h={12} />
							<Skeleton w={70} h={12} />
						</div>
					</div>
					<div class="grid grid-cols-2 gap-2.5">
						<Skeleton h={32} class="rounded-stw-md" />
						<Skeleton h={32} class="rounded-stw-md" />
					</div>
				{:else if quotaError}
					<p class="m-0 text-[12px] text-stw-err">{quotaError}</p>
				{:else}
					{#if quotaState?.has_usage}
						{@const q = quotaState}
						{@const used = q.usage_bytes ?? 0}
						{@const cap = q.hard_bytes ?? 0}
						<QuotaMeter {used} soft={q.soft_bytes} hard={q.hard_bytes}>
							{#snippet stats()}
								<span>
									<strong>{fmtBytes(used)}</strong> used
									{#if cap > 0}
										of <strong>{fmtBytes(cap)}</strong>{/if}
								</span>
								<span>{q.object_count ?? 0} objects</span>
								{#if q.computed_at}
									<span class="text-[11px] text-stw-fg-soft">
										as of {new Date(q.computed_at).toLocaleString()}
									</span>
								{/if}
							{/snippet}
						</QuotaMeter>
					{:else}
						<p class="text-[12px] text-stw-fg-soft">
							Usage hasn't been computed yet — set a quota or click Recompute.
						</p>
					{/if}

					<div class="grid grid-cols-2 gap-2.5">
						<FormField label="Soft quota">
							<input
								class="stw-input font-mono"
								placeholder="e.g. 8 GB (blank = unset)"
								bind:value={softInput}
							/>
						</FormField>
						<FormField label="Hard quota">
							<input
								class="stw-input font-mono"
								placeholder="e.g. 10 GB (blank = unset)"
								bind:value={hardInput}
							/>
						</FormField>
					</div>

					<div class="flex flex-wrap justify-end gap-2">
						{#snippet recomputeIcon()}<RotateCw size={13} strokeWidth={1.7} />{/snippet}
						<Button
							variant="ghost"
							icon={recomputeIcon}
							onclick={recomputeQuota}
							disabled={saving.quota}
						>
							Recompute
						</Button>
						{#if quotaState?.configured}
							{#snippet trashIcon()}<Trash2 size={13} strokeWidth={1.7} />{/snippet}
							<Button variant="ghost" icon={trashIcon} onclick={clearQuota} disabled={saving.quota}>
								Clear
							</Button>
						{/if}
						{#snippet saveIcon()}<Save size={13} strokeWidth={1.7} />{/snippet}
						<Button variant="primary" icon={saveIcon} onclick={saveQuota} disabled={saving.quota}>
							{saving.quota ? 'Saving…' : 'Save quota'}
						</Button>
					</div>
				{/if}
			</div>
		</SectionCard>

		{#if caps?.bucket_policy}
			<SectionCard>
				<div class="flex items-start gap-4">
					<div class="min-w-0 flex-1">
						<h2 class="m-0 mb-1 inline-flex items-center gap-2 text-[14px] font-semibold">
							<Globe size={14} strokeWidth={1.7} /> Public read access
						</h2>
						<p class="m-0 text-[12.5px] leading-[1.5] text-stw-fg-mute">
							Installs the canned <code>s3:GetObject</code> Allow-everyone policy. Anyone with an
							object key can fetch it. {#if policyReady && policy && !publicReadCanned}<strong
									>This bucket has a custom policy</strong
								> — toggle is disabled. Edit the policy below to change it.{/if}
						</p>
						{#if policyError}
							<p class="m-0 mt-1 text-[12px] text-stw-err">{policyError}</p>
						{/if}
					</div>
					{#if policyReady}
						<Toggle
							value={publicReadCanned}
							onchange={(v) => {
								if (saving.public) return;
								if (policy && !publicReadCanned) {
									toast.error('Custom policy is in place — edit the policy below to change it.');
									return;
								}
								void togglePublicRead(v);
							}}
						/>
					{:else}
						<Skeleton w={44} h={22} class="shrink-0 rounded-full" />
					{/if}
				</div>
			</SectionCard>
		{/if}

		{#if caps?.cors}
			<SectionCard>
				<div class="flex flex-col gap-3">
					<div>
						<h2 class="m-0 mb-1 text-[14px] font-semibold">CORS rules</h2>
						<p class="m-0 text-[12.5px] leading-[1.5] text-stw-fg-mute">
							JSON array of rules. Each rule has <code>allowed_origins</code>,
							<code>allowed_methods</code>, and optional <code>allowed_headers</code>,
							<code>expose_headers</code>, <code>max_age_seconds</code>.
						</p>
						{#if corsError}
							<p class="m-0 mt-1 text-[12px] text-stw-err">{corsError}</p>
						{/if}
					</div>
					{#if corsReady}
						<textarea
							class="stw-input min-h-[160px] resize-y px-3 py-2.5 font-mono text-[12.5px] leading-[1.5]"
							spellcheck="false"
							bind:value={corsJSON}
						></textarea>
					{:else}
						<Skeleton h={160} class="block rounded-stw-md" />
					{/if}
					<div class="flex justify-end">
						{#snippet saveIcon()}<Save size={13} strokeWidth={1.7} />{/snippet}
						<Button
							variant="primary"
							icon={saveIcon}
							onclick={saveCORS}
							disabled={saving.cors || !corsReady}
						>
							{saving.cors ? 'Saving…' : 'Save CORS'}
						</Button>
					</div>
				</div>
			</SectionCard>
		{/if}

		{#if caps?.bucket_policy}
			<SectionCard>
				<div class="flex flex-col gap-3">
					<div>
						<h2 class="m-0 mb-1 text-[14px] font-semibold">Bucket policy</h2>
						<p class="m-0 text-[12.5px] leading-[1.5] text-stw-fg-mute">
							Raw JSON policy document. Leave the editor empty and Save to delete the policy
							entirely. Saving while the public-read toggle is on will overwrite the canned shape.
						</p>
					</div>
					{#if policyReady}
						<textarea
							class="stw-input min-h-[160px] resize-y px-3 py-2.5 font-mono text-[12.5px] leading-[1.5]"
							spellcheck="false"
							placeholder="(no policy set)"
							bind:value={policyJSON}
						></textarea>
					{:else}
						<Skeleton h={160} class="block rounded-stw-md" />
					{/if}
					<div class="flex justify-end">
						{#snippet saveIcon()}<Save size={13} strokeWidth={1.7} />{/snippet}
						<Button
							variant="primary"
							icon={saveIcon}
							onclick={savePolicy}
							disabled={saving.policy || !policyReady}
						>
							{saving.policy ? 'Saving…' : 'Save policy'}
						</Button>
					</div>
				</div>
			</SectionCard>
		{/if}

		{#if caps?.lifecycle}
			<SectionCard>
				<div class="flex flex-col gap-3">
					<div>
						<h2 class="m-0 mb-1 text-[14px] font-semibold">Lifecycle rules</h2>
						<p class="m-0 text-[12.5px] leading-[1.5] text-stw-fg-mute">
							Each rule applies to objects under <code>prefix</code> and triggers an action after the
							given days. Supply at least one of expiration / noncurrent / abort-incomplete / transition.
						</p>
						{#if lifecycleError}
							<p class="m-0 mt-1 text-[12px] text-stw-err">{lifecycleError}</p>
						{/if}
					</div>

					{#if !lifecycleReady}
						<div class="flex flex-col gap-1">
							<Skeleton h={32} class="block rounded-stw-md" />
							<Skeleton h={32} class="block rounded-stw-md" />
							<Skeleton h={32} class="block rounded-stw-md" />
						</div>
					{:else if lifecycle.length === 0}
						<div
							class="rounded-md border border-dashed border-stw-border p-5 text-center text-[13px] text-stw-fg-mute"
						>
							No rules configured.
						</div>
					{:else}
						<div class="flex flex-col gap-1">
							<div
								class="grid grid-cols-[60px_1.2fr_1.4fr_90px_100px_110px_32px] items-center gap-1.5 px-1 text-[11px] font-semibold tracking-[0.04em] text-stw-fg-soft uppercase"
							>
								<span>Enabled</span>
								<span>ID</span>
								<span>Prefix</span>
								<span>Expire (days)</span>
								<span>Noncurrent (days)</span>
								<span>Abort multipart (days)</span>
								<span></span>
							</div>
							{#each lifecycle as _rule, i (i)}
								<div
									class="grid grid-cols-[60px_1.2fr_1.4fr_90px_100px_110px_32px] items-center gap-1.5"
								>
									<Toggle
										value={lifecycle[i].enabled}
										onchange={(v) => (lifecycle[i].enabled = v)}
									/>
									<input
										class="stw-input h-[30px] min-w-0 font-mono text-[12.5px]"
										placeholder="rule-id"
										bind:value={lifecycle[i].id}
									/>
									<input
										class="stw-input h-[30px] min-w-0 font-mono text-[12.5px]"
										placeholder="prefix/"
										bind:value={lifecycle[i].prefix}
									/>
									<input
										class="stw-input h-[30px] min-w-0 font-mono text-[12.5px]"
										type="number"
										min="0"
										bind:value={lifecycle[i].expiration_days}
									/>
									<input
										class="stw-input h-[30px] min-w-0 font-mono text-[12.5px]"
										type="number"
										min="0"
										bind:value={lifecycle[i].noncurrent_expire_days}
									/>
									<input
										class="stw-input h-[30px] min-w-0 font-mono text-[12.5px]"
										type="number"
										min="0"
										bind:value={lifecycle[i].abort_incomplete_days}
									/>
									<button
										type="button"
										onclick={() => removeLifecycleRule(i)}
										aria-label="Remove rule"
										class="inline-flex h-[26px] w-[26px] cursor-pointer items-center justify-center rounded-[5px] border-0 bg-transparent text-stw-fg-mute focus-ring hover:bg-stw-bg-hover"
									>
										<Trash2 size={13} strokeWidth={1.7} />
									</button>
								</div>
							{/each}
						</div>
					{/if}
					<div class="flex justify-between gap-2">
						{#snippet plusIcon()}<Plus size={13} strokeWidth={1.7} />{/snippet}
						<Button
							variant="ghost"
							icon={plusIcon}
							onclick={addLifecycleRule}
							disabled={!lifecycleReady}
						>
							Add rule
						</Button>
						{#snippet saveIcon()}<Save size={13} strokeWidth={1.7} />{/snippet}
						<Button
							variant="primary"
							icon={saveIcon}
							onclick={saveLifecycle}
							disabled={saving.lifecycle || !lifecycleReady}
						>
							{saving.lifecycle ? 'Saving…' : 'Save lifecycle'}
						</Button>
					</div>
				</div>
			</SectionCard>
		{/if}

		{#if !caps?.versioning && !caps?.cors && !caps?.bucket_policy && !caps?.lifecycle}
			<Banner variant="err">This backend doesn't expose any bucket-level settings.</Banner>
		{/if}
	{/if}
</div>
