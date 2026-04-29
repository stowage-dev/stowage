<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { invalidateAll } from '$app/navigation';
	import { Activity, CircleCheck, CircleAlert, RotateCw } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import SectionCard from '$lib/components/ui/SectionCard.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import { session } from '$lib/stores/session.svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	const isAdmin = $derived(session.me?.role === 'admin');

	let refreshing = $state(false);
	async function refresh(): Promise<void> {
		refreshing = true;
		try {
			await invalidateAll();
		} finally {
			refreshing = false;
		}
	}

	function fmtTime(s: string | undefined): string {
		if (!s) return '—';
		return new Date(s).toLocaleString();
	}
</script>

<div class="stw-page-pad mx-auto flex max-w-[1100px] flex-col gap-[18px]">
	<PageHeader
		title="Backend health"
		subtitle="Last 20 probe results per backend. Probe = a quick ListBuckets call against each backend on a 60-second cadence."
	>
		{#snippet icon()}<Activity size={18} strokeWidth={1.7} />{/snippet}
		{#snippet actions()}
			{#snippet refreshIcon()}<RotateCw size={12} strokeWidth={1.7} />{/snippet}
			<Button size="sm" icon={refreshIcon} onclick={refresh} disabled={refreshing}>
				{refreshing ? 'Refreshing…' : 'Refresh'}
			</Button>
		{/snippet}
	</PageHeader>

	{#if !isAdmin}
		<Banner variant="err">Admin only.</Banner>
	{:else if data.error}
		<Banner variant="err">{data.error}</Banner>
	{:else if data.backends.length === 0}
		<EmptyState variant="card" hint="No backends configured." />
	{:else}
		{#each data.backends as bk (bk.id)}
			<SectionCard>
				{#snippet header()}
					<header
						class="flex flex-wrap items-center gap-2.5 border-b border-[var(--stw-border)] px-[18px] py-3.5"
					>
						{#if bk.healthy}
							<span class="inline-flex text-[var(--stw-ok)]">
								<CircleCheck size={16} strokeWidth={1.7} />
							</span>
						{:else}
							<span class="inline-flex text-[var(--stw-err)]">
								<CircleAlert size={16} strokeWidth={1.7} />
							</span>
						{/if}
						<div class="min-w-0 flex-1">
							<div class="text-[14px] font-semibold">{bk.name}</div>
							<div class="font-mono text-[11.5px] text-[var(--stw-fg-soft)]">{bk.id}</div>
						</div>
						<div class="text-right font-mono text-[12px] text-[var(--stw-fg-mute)]">
							<div>last probe {fmtTime(bk.last_probe_at)}</div>
							<div>{bk.latency_ms}ms</div>
						</div>
					</header>
				{/snippet}

				<div class="flex flex-col gap-3">
					{#if bk.last_error}
						<div
							class="truncate rounded-md border px-3 py-2 font-mono text-[11.5px]"
							style="background:color-mix(in oklch, var(--stw-err) 8%, var(--stw-bg-panel));border-color:color-mix(in oklch, var(--stw-err) 30%, var(--stw-border));color:var(--stw-err);"
							title={bk.last_error}
						>
							{bk.last_error}
						</div>
					{/if}

					<div
						class="grid gap-[3px]"
						role="img"
						aria-label="Recent probe history"
						style="grid-template-columns:repeat(20,1fr);"
					>
						{#each bk.history as p, i (i)}
							<div
								class="h-[22px] rounded-[3px]"
								style="background:color-mix(in oklch, {p.healthy
									? 'var(--stw-ok)'
									: 'var(--stw-err)'} 60%, var(--stw-bg-panel));"
								title="{fmtTime(p.at)} · {p.latency_ms}ms{p.error ? ' · ' + p.error : ''}"
							></div>
						{/each}
						{#each Array(Math.max(0, 20 - bk.history.length)) as _, i (i)}
							<div
								class="h-[22px] rounded-[3px] border border-dashed border-[var(--stw-border)] bg-[var(--stw-bg-sunken)]"
							></div>
						{/each}
					</div>
				</div>
			</SectionCard>
		{/each}
	{/if}
</div>
