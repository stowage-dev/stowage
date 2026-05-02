// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Adapted from shadcn-svelte's `createSvelteTable` helper — see
// https://www.shadcn-svelte.com/docs/components/data-table.
//
// The bridge between Svelte 5 runes and TanStack Table v8 hinges on
// `mergeObjects`: a Proxy-based merge that intercepts property reads instead
// of spreading values. `createTable` would otherwise spread the options
// object once at construction, evaluating any getter we put on `data` /
// `state.sorting` / etc. exactly once and freezing the result. The Proxy
// makes those reads pass through to our reactive sources every time, so
// `$state` updates surface naturally inside TanStack's memoized callbacks.

import {
	createTable,
	getCoreRowModel,
	getFilteredRowModel,
	getPaginationRowModel,
	getSortedRowModel,
	type ColumnFiltersState,
	type PaginationState,
	type RowData,
	type RowSelectionState,
	type SortingState,
	type Table,
	type TableOptions,
	type TableOptionsResolved,
	type TableState,
	type Updater,
	type VisibilityState
} from '@tanstack/table-core';
import type { Column } from './types';

export interface CreateDataTableOptions<TData extends RowData> extends Omit<
	TableOptions<TData>,
	| 'data'
	| 'columns'
	| 'state'
	| 'onStateChange'
	| 'getCoreRowModel'
	| 'getSortedRowModel'
	| 'getFilteredRowModel'
	| 'getPaginationRowModel'
> {
	data: () => TData[];
	columns: Column<TData>[];
	initialSorting?: SortingState;
	initialPagination?: PaginationState;
	initialColumnVisibility?: VisibilityState;
	pageSize?: number;
	enablePagination?: boolean;
}

/**
 * Build a reactive TanStack table backed by Svelte 5 runes. The caller
 * passes a `data` getter (so the table tracks the source array reactively),
 * column definitions, and any additional TanStack options. Sorting,
 * filtering, pagination, selection and column visibility state are owned
 * inside this helper and exposed through the standard TanStack API.
 */
export function createDataTable<TData extends RowData>(
	opts: CreateDataTableOptions<TData>
): { table: Table<TData> } {
	const {
		data,
		columns,
		initialSorting = [],
		initialPagination,
		initialColumnVisibility = {},
		pageSize,
		enablePagination = false,
		...rest
	} = opts;

	let sorting = $state<SortingState>(initialSorting);
	let columnFilters = $state<ColumnFiltersState>([]);
	let globalFilter = $state<string>('');
	let pagination = $state<PaginationState>(
		initialPagination ?? { pageIndex: 0, pageSize: pageSize ?? 50 }
	);
	let rowSelection = $state<RowSelectionState>({});
	let columnVisibility = $state<VisibilityState>(initialColumnVisibility);

	const reactiveOptions: TableOptions<TData> = {
		...rest,
		get data() {
			return data();
		},
		columns: columns as TableOptions<TData>['columns'],
		state: {
			get sorting() {
				return sorting;
			},
			get columnFilters() {
				return columnFilters;
			},
			get globalFilter() {
				return globalFilter;
			},
			get pagination() {
				return pagination;
			},
			get rowSelection() {
				return rowSelection;
			},
			get columnVisibility() {
				return columnVisibility;
			}
		},
		onSortingChange: (u) => apply((v) => (sorting = v), sorting, u),
		onColumnFiltersChange: (u) => apply((v) => (columnFilters = v), columnFilters, u),
		onGlobalFilterChange: (u) => apply((v) => (globalFilter = v), globalFilter, u),
		onPaginationChange: (u) => apply((v) => (pagination = v), pagination, u),
		onRowSelectionChange: (u) => apply((v) => (rowSelection = v), rowSelection, u),
		onColumnVisibilityChange: (u) => apply((v) => (columnVisibility = v), columnVisibility, u),
		getCoreRowModel: getCoreRowModel(),
		getSortedRowModel: getSortedRowModel(),
		getFilteredRowModel: getFilteredRowModel(),
		...(enablePagination ? { getPaginationRowModel: getPaginationRowModel() } : {})
	};

	const resolvedOptions: TableOptionsResolved<TData> = mergeObjects(
		{
			state: {},
			onStateChange() {},
			renderFallbackValue: null,
			mergeOptions: (defaultOptions: TableOptions<TData>, options: Partial<TableOptions<TData>>) =>
				mergeObjects(defaultOptions, options)
		},
		reactiveOptions
	);

	const table = createTable(resolvedOptions);

	let tableState = $state<TableState>(table.initialState);

	function updateOptions(): void {
		table.setOptions(() =>
			mergeObjects(resolvedOptions, reactiveOptions, {
				state: mergeObjects(tableState, reactiveOptions.state || {}),
				onStateChange: (updater: Updater<TableState>) => {
					if (updater instanceof Function) tableState = updater(tableState);
					else tableState = mergeObjects(tableState, updater);
					reactiveOptions.onStateChange?.(updater);
				}
			})
		);
	}

	updateOptions();
	$effect.pre(() => {
		updateOptions();
	});

	return { table };
}

function apply<T>(setter: (next: T) => void, current: T, updater: Updater<T>): void {
	setter(typeof updater === 'function' ? (updater as (old: T) => T)(current) : updater);
}

type MaybeThunk<T extends object> = T | (() => T | null | undefined);
type Intersection<T extends readonly unknown[]> = (T extends [infer H, ...infer R]
	? H & Intersection<R>
	: unknown) & {};

/**
 * Proxy-based shallow merge that preserves getter semantics from every
 * source — unlike `{ ...a, ...b }`, which evaluates getters once and
 * freezes the result. Lifted from shadcn-svelte's helper of the same name.
 */
function mergeObjects<Sources extends readonly MaybeThunk<object>[]>(
	...sources: Sources
): Intersection<{ [K in keyof Sources]: Sources[K] }> {
	const resolve = <T extends object>(src: MaybeThunk<T>): T | undefined =>
		typeof src === 'function' ? ((src as () => T | null | undefined)() ?? undefined) : src;

	const findSourceWithKey = (key: PropertyKey): object | undefined => {
		for (let i = sources.length - 1; i >= 0; i--) {
			const obj = resolve(sources[i] as MaybeThunk<object>);
			if (obj && key in obj) return obj;
		}
		return undefined;
	};

	return new Proxy(Object.create(null) as object, {
		get(_, key) {
			const src = findSourceWithKey(key);
			return src ? (src as Record<PropertyKey, unknown>)[key] : undefined;
		},
		has(_, key) {
			return !!findSourceWithKey(key);
		},
		ownKeys() {
			const all = new Set<string | symbol>();
			for (const s of sources) {
				const obj = resolve(s as MaybeThunk<object>);
				if (obj) for (const k of Reflect.ownKeys(obj)) all.add(k);
			}
			return [...all];
		},
		getOwnPropertyDescriptor(_, key) {
			const src = findSourceWithKey(key);
			if (!src) return undefined;
			return {
				configurable: true,
				enumerable: true,
				value: (src as Record<PropertyKey, unknown>)[key],
				writable: true
			};
		}
	}) as Intersection<{ [K in keyof Sources]: Sources[K] }>;
}
