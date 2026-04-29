// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { ApiClient } from '$lib/api';
import type { PageLoad } from './$types';
import type { S3AnonymousBinding, S3CredentialView } from '$lib/types';

export const load: PageLoad = async ({ fetch }) => {
	const api = new ApiClient(fetch);
	const [credsResult, anonResult] = await Promise.allSettled([
		api.listS3ProxyCredentials(),
		api.listS3ProxyAnonymous()
	]);

	let credentials: S3CredentialView[] = [];
	let bindings: S3AnonymousBinding[] = [];
	let error: string | null = null;

	if (credsResult.status === 'fulfilled') {
		credentials = credsResult.value;
	} else {
		error =
			credsResult.reason instanceof Error
				? credsResult.reason.message
				: 'Failed to load credentials.';
	}
	if (anonResult.status === 'fulfilled') {
		bindings = anonResult.value;
	} else if (!error) {
		error =
			anonResult.reason instanceof Error
				? anonResult.reason.message
				: 'Failed to load anonymous bindings.';
	}

	return { credentials, bindings, error };
};
