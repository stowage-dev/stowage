// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import { ApiClient, ApiException } from '$lib/api';
import type { PageLoad } from './$types';
import type { S3AnonymousBinding, S3CredentialView } from '$lib/types';

export const load: PageLoad = async ({ fetch }) => {
	const api = new ApiClient(fetch);
	const [credsResult, anonResult] = await Promise.allSettled([
		api.listS3ProxyCredentials(),
		api.listS3ProxyAnonymous()
	]);

	const isProxyDisabled = (r: PromiseSettledResult<unknown>) =>
		r.status === 'rejected' &&
		r.reason instanceof ApiException &&
		r.reason.code === 's3_proxy_disabled';

	let credentials: S3CredentialView[] = [];
	let bindings: S3AnonymousBinding[] = [];
	let error: string | null = null;
	const disabled = isProxyDisabled(credsResult) && isProxyDisabled(anonResult);

	if (!disabled) {
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
	}

	return { credentials, bindings, error, disabled };
};
