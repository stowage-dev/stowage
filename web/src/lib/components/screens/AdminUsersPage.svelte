<script lang="ts">
	// Copyright (C) 2026 Damian van der Merwe
	// SPDX-License-Identifier: AGPL-3.0-or-later
	import { invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { Plus, Lock, Unlock, Key, Trash2 } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import IconButton from '$lib/components/ui/IconButton.svelte';
	import Chip from '$lib/components/ui/Chip.svelte';
	import Tooltip from '$lib/components/ui/Tooltip.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import SearchField from '$lib/components/ui/SearchField.svelte';
	import DataTable from '$lib/components/ui/DataTable.svelte';
	import Banner from '$lib/components/ui/Banner.svelte';
	import StatLine from '$lib/components/ui/StatLine.svelte';
	import CreateUserPanel from './CreateUserPanel.svelte';
	import ResetPasswordDialog from './ResetPasswordDialog.svelte';
	import { api, ApiException } from '$lib/api';
	import { session } from '$lib/stores/session.svelte';
	import type { User } from '$lib/types';

	interface Props {
		users: User[];
		error: string | null;
	}

	let { users, error }: Props = $props();

	type RoleFilter = 'all' | User['role'];

	let q = $state('');
	let roleFilter = $state<RoleFilter>('all');
	let showCreate = $state(false);
	let resetTarget = $state<User | null>(null);

	const list = $derived(
		users.filter(
			(u) =>
				(roleFilter === 'all' || u.role === roleFilter) &&
				(q === '' ||
					u.username.toLowerCase().includes(q.toLowerCase()) ||
					(u.email ?? '').toLowerCase().includes(q.toLowerCase()))
		)
	);

	const meId = $derived(session.me?.id);
	const roles: RoleFilter[] = ['all', 'admin', 'editor', 'viewer'];

	async function unlock(u: User) {
		try {
			await api.unlockUser(u.id);
			toast.success(`Unlocked ${u.username}`);
			await invalidateAll();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Unlock failed.');
		}
	}

	function reset(u: User) {
		resetTarget = u;
	}

	async function destroy(u: User) {
		if (u.id === meId) {
			toast.error('You cannot delete your own account.');
			return;
		}
		if (!confirm(`Delete user "${u.username}"? This cannot be undone.`)) return;
		try {
			await api.deleteUser(u.id);
			toast.success(`Deleted ${u.username}`);
			await invalidateAll();
		} catch (err) {
			toast.error(err instanceof ApiException ? err.message : 'Delete failed.');
		}
	}

	function shortSource(src: string): { type: string; issuer?: string } {
		const [type, issuer] = src.split(':');
		return { type, issuer };
	}

	const columns = [
		{ key: 'login', label: 'Login' },
		{ key: 'email', label: 'Email' },
		{ key: 'source', label: 'Source' },
		{ key: 'role', label: 'Role' },
		{ key: 'status', label: 'Status' },
		{ key: 'created', label: 'Created' },
		{ key: 'actions', label: '', align: 'right' as const }
	];
</script>

<div class="flex flex-col gap-4 stw-page-pad">
	<PageHeader title="Users">
		{#snippet meta()}
			<StatLine
				items={[
					{ label: 'total', value: users.length },
					{ label: 'enabled', value: users.filter((u) => u.enabled).length },
					{ label: 'locked', value: users.filter((u) => u.locked_until).length }
				]}
			/>
		{/snippet}
		{#snippet actions()}
			<SearchField bind:value={q} placeholder="Search users" size="sm" width="240px" />
			{#snippet plusIcon()}<Plus size={13} strokeWidth={1.7} />{/snippet}
			<Button icon={plusIcon} variant="primary" onclick={() => (showCreate = true)}>
				Create user
			</Button>
		{/snippet}
	</PageHeader>

	<div class="flex gap-1.5">
		{#each roles as r (r)}
			<Chip pressed={roleFilter === r} onclick={() => (roleFilter = r)}>
				{r}
			</Chip>
		{/each}
	</div>

	{#if error}
		<Banner variant="err" role="alert" title="Could not load users">
			<span class="font-mono">{error}</span>
		</Banner>
	{/if}

	<DataTable {columns} rows={list} emptyText="No users match.">
		{#snippet row(u)}
			{@const src = shortSource(u.identity_source)}
			{@const locked = !!u.locked_until && new Date(u.locked_until).getTime() > Date.now()}
			<td class="px-3">
				<span class="inline-flex items-center gap-2">
					<span
						aria-hidden="true"
						class="inline-flex h-[22px] w-[22px] items-center justify-center rounded bg-stw-bg-hover text-[10.5px] font-semibold text-stw-fg-mute"
					>
						{u.username.slice(0, 2).toUpperCase()}
					</span>
					<span class="font-medium">{u.username}</span>
					{#if u.id === meId}
						<span class="text-[10.5px] text-stw-fg-soft">(you)</span>
					{/if}
				</span>
			</td>
			<td class="px-3 font-mono text-[12px] text-stw-fg-mute">{u.email ?? '—'}</td>
			<td class="px-3">
				{#if src.type === 'oidc'}
					<Badge mono class="stw-role-admin">
						oidc:{src.issuer}
					</Badge>
				{:else if src.type === 'static'}
					<Badge variant="warn">static</Badge>
				{:else}
					<Badge>local</Badge>
				{/if}
			</td>
			<td class="px-3">
				<Badge variant={u.role === 'admin' ? 'ok' : undefined}>{u.role}</Badge>
			</td>
			<td class="px-3">
				{#if locked}
					<Badge variant="err">
						<Lock size={10} strokeWidth={1.7} /> locked
					</Badge>
				{:else if u.enabled}
					<span class="text-[11.5px] text-stw-fg-mute">enabled</span>
				{:else}
					<Badge>disabled</Badge>
				{/if}
			</td>
			<td class="px-3 font-mono text-[12px] text-stw-fg-mute">
				{new Date(u.created_at).toLocaleDateString()}
			</td>
			<td class="px-3 text-right">
				<span class="inline-flex gap-0.5">
					{#if locked}
						{#snippet unlockIcon()}<Unlock size={13} strokeWidth={1.7} />{/snippet}
						<Tooltip text="Unlock">
							<IconButton label="Unlock" size={24} icon={unlockIcon} onclick={() => unlock(u)} />
						</Tooltip>
					{/if}
					{#if src.type === 'local' || src.type === ''}
						{#snippet keyIcon()}<Key size={13} strokeWidth={1.7} />{/snippet}
						<Tooltip text="Reset password">
							<IconButton label="Reset" size={24} icon={keyIcon} onclick={() => reset(u)} />
						</Tooltip>
					{/if}
					{#snippet trashIcon()}<Trash2 size={13} strokeWidth={1.7} />{/snippet}
					<Tooltip text={u.id === meId ? "Can't delete yourself" : 'Delete'}>
						<IconButton label="Delete" size={24} icon={trashIcon} onclick={() => destroy(u)} />
					</Tooltip>
				</span>
			</td>
		{/snippet}
	</DataTable>
</div>

{#if showCreate}
	<CreateUserPanel onclose={() => (showCreate = false)} />
{/if}

{#if resetTarget}
	<ResetPasswordDialog user={resetTarget} onclose={() => (resetTarget = null)} />
{/if}
