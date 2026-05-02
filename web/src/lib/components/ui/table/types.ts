// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { ColumnDef, CellContext, HeaderContext, Table } from '@tanstack/table-core';
import type { Snippet } from 'svelte';

export type Align = 'left' | 'right' | 'center';
export type Density = 'compact' | 'cosy' | 'roomy';

export interface ColumnExtras<TData, TValue = unknown> {
	align?: Align;
	mono?: boolean;
	headerClass?: string;
	cellClass?: string;
	headerSnippet?: Snippet<[HeaderContext<TData, TValue>]>;
	cellSnippet?: Snippet<[CellContext<TData, TValue>]>;
}

/**
 * A stowage column definition: TanStack's `ColumnDef` plus the
 * presentational knobs the design system uses (alignment, mono font,
 * fixed widths via class) and optional Svelte snippets for header /
 * cell content. Defined as an intersection because `ColumnDef` is a
 * discriminated union and can't be `extend`ed by an interface.
 */
export type Column<TData, TValue = unknown> = ColumnDef<TData, TValue> &
	ColumnExtras<TData, TValue>;

export type DataTable<TData> = Table<TData>;
