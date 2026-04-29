// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

import type {
	AdminDashboard,
	AuditEvent,
	AuditFilter,
	AuthConfig,
	Backend,
	BackendHealth,
	BucketPin,
	BucketQuota,
	BulkDeleteResult,
	Bucket,
	CORSRule,
	CreateEndpointRequest,
	CreateS3CredentialRequest,
	CreateShareRequest,
	CreateUserRequest,
	Endpoint,
	LifecycleRule,
	ListObjectsRequest,
	ListObjectsResult,
	LoginResult,
	Me,
	ObjectIdentifier,
	ObjectInfo,
	ObjectVersion,
	PatchEndpointRequest,
	PatchS3CredentialRequest,
	PatchUserRequest,
	PrefixDoneEvent,
	PrefixEvent,
	PrefixSize,
	BucketSizeTracking,
	S3AnonymousBinding,
	S3Credential,
	S3CredentialView,
	SearchResponse,
	Share,
	TestEndpointRequest,
	TestEndpointResult,
	UpsertS3AnonymousBindingRequest,
	User,
	UserListFilter
} from './types';
import { messageFor } from './i18n';

export interface ApiErrorPayload {
	code: string;
	message: string;
	detail?: string;
}

export class ApiException extends Error {
	code: string;
	detail?: string;
	status: number;
	constructor(status: number, err: ApiErrorPayload) {
		super(err.message);
		this.code = err.code;
		this.detail = err.detail;
		this.status = status;
	}
}

function csrfToken(): string | null {
	if (typeof document === 'undefined') return null;
	const m = document.cookie.match(/(?:^|;\s*)stowage_csrf=([^;]+)/);
	return m ? decodeURIComponent(m[1]) : null;
}

interface RequestOpts {
	method?: 'GET' | 'POST' | 'PATCH' | 'DELETE' | 'PUT';
	body?: unknown;
	query?: Record<string, string | number | boolean | undefined>;
	signal?: AbortSignal;
	/** Extra request headers (e.g. `If-None-Match: *` for conditional writes). */
	headers?: Record<string, string>;
	/** Set when the call should swallow 401 instead of throwing (e.g. `/api/me`). */
	allow401?: boolean;
}

export class ApiClient {
	constructor(private fetcher: typeof fetch = fetch) {}

	private async request<T>(path: string, opts: RequestOpts = {}): Promise<T> {
		const url = buildUrl(path, opts.query);
		const headers = new Headers();
		const isMutating = opts.method && opts.method !== 'GET';
		if (isMutating) {
			const csrf = csrfToken();
			if (csrf) headers.set('X-CSRF-Token', csrf);
		}

		let body: BodyInit | undefined;
		if (opts.body instanceof FormData) {
			body = opts.body;
		} else if (opts.body !== undefined) {
			headers.set('Content-Type', 'application/json');
			body = JSON.stringify(opts.body);
		}

		if (opts.headers) {
			for (const [k, v] of Object.entries(opts.headers)) headers.set(k, v);
		}

		const res = await this.fetcher(url, {
			method: opts.method ?? 'GET',
			headers,
			body,
			credentials: 'same-origin',
			signal: opts.signal
		});

		if (res.status === 204) return undefined as T;
		if (res.status === 401 && opts.allow401) return undefined as T;

		const ct = res.headers.get('content-type') ?? '';
		if (!ct.includes('application/json')) {
			if (res.ok) return (await res.text()) as unknown as T;
			throw new ApiException(res.status, {
				code: 'unknown',
				message: messageFor('unknown', `${res.status} ${res.statusText}`)
			});
		}

		const json = await res.json();
		if (!res.ok) {
			const err = (json?.error ?? json) as ApiErrorPayload;
			throw new ApiException(res.status, {
				code: err.code ?? 'unknown',
				message: messageFor(err.code ?? 'unknown', err.message),
				detail: err.detail
			});
		}
		return json as T;
	}

	// ---- Auth -----------------------------------------------------------

	authConfig(): Promise<AuthConfig> {
		return this.request<AuthConfig>('/api/auth/config');
	}

	loginLocal(username: string, password: string): Promise<LoginResult> {
		return this.request<LoginResult>('/auth/login/local', {
			method: 'POST',
			body: { username, password }
		});
	}

	logout(): Promise<void> {
		return this.request<void>('/auth/logout', { method: 'POST' });
	}

	loginOIDCStartURL(): string {
		return '/auth/login/oidc';
	}

	async me(): Promise<Me | null> {
		const out = await this.request<Me | undefined>('/api/me', { allow401: true });
		return out ?? null;
	}

	changeOwnPassword(currentPassword: string, newPassword: string): Promise<void> {
		return this.request<void>('/api/me/password', {
			method: 'POST',
			body: { current_password: currentPassword, new_password: newPassword }
		});
	}

	// ---- Pinned buckets ------------------------------------------------

	async listPins(): Promise<BucketPin[]> {
		const res = await this.request<{ pins: BucketPin[] }>('/api/me/pins/');
		return res.pins ?? [];
	}

	pinBucket(backendId: string, bucket: string): Promise<BucketPin> {
		return this.request<BucketPin>('/api/me/pins/', {
			method: 'POST',
			body: { backend_id: backendId, bucket }
		});
	}

	unpinBucket(backendId: string, bucket: string): Promise<void> {
		return this.request<void>(
			`/api/me/pins/${encodeURIComponent(backendId)}/${encodeURIComponent(bucket)}`,
			{ method: 'DELETE' }
		);
	}

	// ---- Admin users ----------------------------------------------------

	async listUsers(filter: UserListFilter = {}): Promise<User[]> {
		const out = await this.request<{ users: User[] }>('/api/admin/users/', {
			query: filter as Record<string, string | number | boolean | undefined>
		});
		return out.users ?? [];
	}

	createUser(req: CreateUserRequest): Promise<User> {
		return this.request<User>('/api/admin/users/', { method: 'POST', body: req });
	}

	getUser(id: string): Promise<User> {
		return this.request<User>(`/api/admin/users/${encodeURIComponent(id)}`);
	}

	patchUser(id: string, req: PatchUserRequest): Promise<User> {
		return this.request<User>(`/api/admin/users/${encodeURIComponent(id)}`, {
			method: 'PATCH',
			body: req
		});
	}

	resetUserPassword(id: string, newPassword: string): Promise<void> {
		return this.request<void>(`/api/admin/users/${encodeURIComponent(id)}/reset-password`, {
			method: 'POST',
			body: { new_password: newPassword }
		});
	}

	unlockUser(id: string): Promise<void> {
		return this.request<void>(`/api/admin/users/${encodeURIComponent(id)}/unlock`, {
			method: 'POST'
		});
	}

	deleteUser(id: string): Promise<void> {
		return this.request<void>(`/api/admin/users/${encodeURIComponent(id)}`, {
			method: 'DELETE'
		});
	}

	// ---- Admin endpoints (S3 backend management) -----------------------

	async listEndpoints(): Promise<Endpoint[]> {
		const out = await this.request<{ backends: Endpoint[] }>('/api/admin/backends/');
		return out.backends ?? [];
	}

	getEndpoint(id: string): Promise<Endpoint> {
		return this.request<Endpoint>(`/api/admin/backends/${encodeURIComponent(id)}`);
	}

	createEndpoint(req: CreateEndpointRequest): Promise<Endpoint> {
		return this.request<Endpoint>('/api/admin/backends/', { method: 'POST', body: req });
	}

	patchEndpoint(id: string, req: PatchEndpointRequest): Promise<Endpoint> {
		return this.request<Endpoint>(`/api/admin/backends/${encodeURIComponent(id)}`, {
			method: 'PATCH',
			body: req
		});
	}

	deleteEndpoint(id: string): Promise<void> {
		return this.request<void>(`/api/admin/backends/${encodeURIComponent(id)}`, {
			method: 'DELETE'
		});
	}

	testEndpoint(req: TestEndpointRequest): Promise<TestEndpointResult> {
		return this.request<TestEndpointResult>('/api/admin/backends/test', {
			method: 'POST',
			body: req
		});
	}

	// ---- Admin dashboard ------------------------------------------------

	adminDashboard(): Promise<AdminDashboard> {
		return this.request<AdminDashboard>('/api/admin/dashboard');
	}

	// ---- S3 Proxy: admin merged view (read-only) ------------------------

	async listS3ProxyCredentials(): Promise<S3CredentialView[]> {
		const out = await this.request<{ credentials: S3CredentialView[] }>(
			'/api/admin/s3-proxy/credentials'
		);
		return out.credentials ?? [];
	}

	async listS3ProxyAnonymous(): Promise<S3AnonymousBinding[]> {
		const out = await this.request<{ bindings: S3AnonymousBinding[] }>(
			'/api/admin/s3-proxy/anonymous'
		);
		return out.bindings ?? [];
	}

	// ---- S3 Proxy: admin CRUD on UI-managed virtual credentials ---------

	async listAdminS3Credentials(): Promise<S3Credential[]> {
		const out = await this.request<{ credentials: S3Credential[] }>('/api/admin/s3-credentials/');
		return out.credentials ?? [];
	}

	createAdminS3Credential(req: CreateS3CredentialRequest): Promise<S3Credential> {
		return this.request<S3Credential>('/api/admin/s3-credentials/', {
			method: 'POST',
			body: req
		});
	}

	patchAdminS3Credential(akid: string, req: PatchS3CredentialRequest): Promise<S3Credential> {
		return this.request<S3Credential>(`/api/admin/s3-credentials/${encodeURIComponent(akid)}`, {
			method: 'PATCH',
			body: req
		});
	}

	deleteAdminS3Credential(akid: string): Promise<void> {
		return this.request<void>(`/api/admin/s3-credentials/${encodeURIComponent(akid)}`, {
			method: 'DELETE'
		});
	}

	// ---- S3 Proxy: admin CRUD on anonymous bindings ---------------------

	upsertAdminS3Anonymous(req: UpsertS3AnonymousBindingRequest): Promise<S3AnonymousBinding> {
		return this.request<S3AnonymousBinding>('/api/admin/s3-anonymous/', {
			method: 'POST',
			body: req
		});
	}

	deleteAdminS3Anonymous(backendId: string, bucket: string): Promise<void> {
		return this.request<void>(
			`/api/admin/s3-anonymous/${encodeURIComponent(backendId)}/${encodeURIComponent(bucket)}`,
			{ method: 'DELETE' }
		);
	}

	// ---- S3 Proxy: per-user self-service --------------------------------

	async listMyS3Credentials(): Promise<S3Credential[]> {
		const out = await this.request<{ credentials: S3Credential[] }>('/api/me/s3-credentials/');
		return out.credentials ?? [];
	}

	createMyS3Credential(req: CreateS3CredentialRequest): Promise<S3Credential> {
		return this.request<S3Credential>('/api/me/s3-credentials/', {
			method: 'POST',
			body: req
		});
	}

	patchMyS3Credential(akid: string, req: PatchS3CredentialRequest): Promise<S3Credential> {
		return this.request<S3Credential>(`/api/me/s3-credentials/${encodeURIComponent(akid)}`, {
			method: 'PATCH',
			body: req
		});
	}

	deleteMyS3Credential(akid: string): Promise<void> {
		return this.request<void>(`/api/me/s3-credentials/${encodeURIComponent(akid)}`, {
			method: 'DELETE'
		});
	}

	// ---- Search ---------------------------------------------------------

	search(q: string, signal?: AbortSignal): Promise<SearchResponse> {
		return this.request<SearchResponse>('/api/search', { query: { q }, signal });
	}

	// ---- Backend health -------------------------------------------------

	async listBackendHealth(): Promise<BackendHealth[]> {
		const out = await this.request<{ backends: BackendHealth[] }>('/api/admin/backends/health');
		return out.backends ?? [];
	}

	// ---- Audit log ------------------------------------------------------

	listAudit(filter: AuditFilter = {}): Promise<{ events: AuditEvent[]; total: number }> {
		return this.request('/api/admin/audit', {
			query: filter as Record<string, string | number | boolean | undefined>
		});
	}

	/** Build the CSV download URL with the given filter applied. */
	auditCSVURL(filter: AuditFilter = {}): string {
		const sp = new URLSearchParams();
		for (const [k, v] of Object.entries(filter)) {
			if (v === undefined || v === '' || v === false) continue;
			sp.set(k, String(v));
		}
		const qs = sp.toString();
		return '/api/admin/audit.csv' + (qs ? '?' + qs : '');
	}

	// ---- Backends -------------------------------------------------------

	async listBackends(): Promise<Backend[]> {
		const out = await this.request<{ backends: Backend[] }>('/api/backends/');
		return out.backends ?? [];
	}

	getBackend(id: string): Promise<Backend> {
		return this.request<Backend>(`/api/backends/${encodeURIComponent(id)}/`);
	}

	probeBackend(
		id: string
	): Promise<{ healthy: boolean; last_probe_at: string; last_error?: string }> {
		return this.request(`/api/backends/${encodeURIComponent(id)}/health`);
	}

	// ---- Buckets --------------------------------------------------------

	async listBuckets(backendId: string, opts: { signal?: AbortSignal } = {}): Promise<Bucket[]> {
		const out = await this.request<{ buckets: Bucket[] }>(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/`,
			{ signal: opts.signal }
		);
		return out.buckets ?? [];
	}

	createBucket(backendId: string, name: string, region = ''): Promise<{ name: string }> {
		return this.request(`/api/backends/${encodeURIComponent(backendId)}/buckets/`, {
			method: 'POST',
			body: { name, region }
		});
	}

	deleteBucket(backendId: string, bucket: string): Promise<void> {
		return this.request<void>(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/`,
			{ method: 'DELETE' }
		);
	}

	// ---- Bucket settings (Phase 6 Slice A) ------------------------------

	private bucketSettingURL(backendId: string, bucket: string, leaf: string): string {
		return (
			`/api/backends/${encodeURIComponent(backendId)}/buckets/` +
			`${encodeURIComponent(bucket)}/${leaf}`
		);
	}

	async getBucketVersioning(backendId: string, bucket: string): Promise<boolean> {
		const out = await this.request<{ enabled: boolean }>(
			this.bucketSettingURL(backendId, bucket, 'versioning')
		);
		return out.enabled;
	}

	setBucketVersioning(backendId: string, bucket: string, enabled: boolean): Promise<void> {
		return this.request<void>(this.bucketSettingURL(backendId, bucket, 'versioning'), {
			method: 'PUT',
			body: { enabled }
		});
	}

	async getBucketCORS(backendId: string, bucket: string): Promise<CORSRule[]> {
		const out = await this.request<{ rules: CORSRule[] }>(
			this.bucketSettingURL(backendId, bucket, 'cors')
		);
		return out.rules ?? [];
	}

	setBucketCORS(backendId: string, bucket: string, rules: CORSRule[]): Promise<void> {
		return this.request<void>(this.bucketSettingURL(backendId, bucket, 'cors'), {
			method: 'PUT',
			body: { rules }
		});
	}

	async getBucketPolicy(backendId: string, bucket: string): Promise<string> {
		const out = await this.request<{ policy: string }>(
			this.bucketSettingURL(backendId, bucket, 'policy')
		);
		return out.policy ?? '';
	}

	setBucketPolicy(backendId: string, bucket: string, policy: string): Promise<void> {
		return this.request<void>(this.bucketSettingURL(backendId, bucket, 'policy'), {
			method: 'PUT',
			body: { policy }
		});
	}

	deleteBucketPolicy(backendId: string, bucket: string): Promise<void> {
		return this.request<void>(this.bucketSettingURL(backendId, bucket, 'policy'), {
			method: 'DELETE'
		});
	}

	async getBucketLifecycle(backendId: string, bucket: string): Promise<LifecycleRule[]> {
		const out = await this.request<{ rules: LifecycleRule[] }>(
			this.bucketSettingURL(backendId, bucket, 'lifecycle')
		);
		return out.rules ?? [];
	}

	setBucketLifecycle(backendId: string, bucket: string, rules: LifecycleRule[]): Promise<void> {
		return this.request<void>(this.bucketSettingURL(backendId, bucket, 'lifecycle'), {
			method: 'PUT',
			body: { rules }
		});
	}

	getBucketQuota(backendId: string, bucket: string): Promise<BucketQuota> {
		return this.request<BucketQuota>(this.bucketSettingURL(backendId, bucket, 'quota'));
	}

	setBucketQuota(
		backendId: string,
		bucket: string,
		soft_bytes: number,
		hard_bytes: number
	): Promise<BucketQuota> {
		return this.request<BucketQuota>(this.bucketSettingURL(backendId, bucket, 'quota'), {
			method: 'PUT',
			body: { soft_bytes, hard_bytes }
		});
	}

	deleteBucketQuota(backendId: string, bucket: string): Promise<void> {
		return this.request<void>(this.bucketSettingURL(backendId, bucket, 'quota'), {
			method: 'DELETE'
		});
	}

	recomputeBucketQuota(backendId: string, bucket: string): Promise<BucketQuota> {
		return this.request<BucketQuota>(this.bucketSettingURL(backendId, bucket, 'quota/recompute'), {
			method: 'POST'
		});
	}

	/**
	 * Set or clear the canned public-read bucket policy. Implementation
	 * delegates to setBucketPolicy / deleteBucketPolicy and relies on the
	 * caller's bucket name to template the resource ARN.
	 */
	async setBucketPublicRead(backendId: string, bucket: string, enabled: boolean): Promise<void> {
		if (!enabled) {
			await this.deleteBucketPolicy(backendId, bucket);
			return;
		}
		const policy = JSON.stringify({
			Version: '2012-10-17',
			Statement: [
				{
					Sid: 'PublicReadGetObject',
					Effect: 'Allow',
					Principal: '*',
					Action: 's3:GetObject',
					Resource: `arn:aws:s3:::${bucket}/*`
				}
			]
		});
		await this.setBucketPolicy(backendId, bucket, policy);
	}

	// ---- Objects --------------------------------------------------------

	listObjects(
		backendId: string,
		bucket: string,
		req: ListObjectsRequest = {}
	): Promise<ListObjectsResult> {
		return this.request(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/objects`,
			{ query: req as Record<string, string | number | boolean | undefined> }
		);
	}

	/**
	 * Server-side recursive byte total for `prefix`. The proxy walks the
	 * bucket once per 60 s window and serves the cached figure to every
	 * subsequent caller, so two tabs hitting the same folder share one
	 * upstream walk. Throws ApiException with code `size_tracking_disabled`
	 * (HTTP 409) when the bucket has the toggle off.
	 */
	async prefixSize(
		backendId: string,
		bucket: string,
		prefix: string,
		signal?: AbortSignal
	): Promise<PrefixSize> {
		return this.request<PrefixSize>(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/prefix-size`,
			{ query: { prefix }, signal }
		);
	}

	async getBucketSizeTracking(backendId: string, bucket: string): Promise<BucketSizeTracking> {
		return this.request<BucketSizeTracking>(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/size-tracking`
		);
	}

	async setBucketSizeTracking(
		backendId: string,
		bucket: string,
		enabled: boolean
	): Promise<BucketSizeTracking> {
		return this.request<BucketSizeTracking>(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/size-tracking`,
			{ method: 'PUT', body: { enabled } }
		);
	}

	/**
	 * Fetch full object info (size/etag/metadata/…) as JSON. Use this
	 * instead of HEAD — Go's stdlib strips the response body for HEAD
	 * requests, so the server exposes the same handler as a GET alias.
	 */
	headObject(backendId: string, bucket: string, key: string): Promise<ObjectInfo> {
		return this.request(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/object/info`,
			{ query: { key } }
		);
	}

	objectURL(
		backendId: string,
		bucket: string,
		key: string,
		disposition = 'attachment',
		versionId?: string
	): string {
		const q = new URLSearchParams({ key, disposition });
		if (versionId) q.set('version_id', versionId);
		return `/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(
			bucket
		)}/object?${q.toString()}`;
	}

	previewURL(backendId: string, bucket: string, key: string): string {
		return this.objectURL(backendId, bucket, key, 'inline');
	}

	/**
	 * URL for the streaming zip endpoint. Each key may be an object key or a
	 * trailing-slash prefix (recursively expanded on the server). Trigger the
	 * download with `window.location.href = zipDownloadURL(...)`.
	 */
	zipDownloadURL(backendId: string, bucket: string, keys: string[]): string {
		const sp = new URLSearchParams();
		for (const k of keys) sp.append('key', k);
		return `/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(
			bucket
		)}/objects/zip?${sp.toString()}`;
	}

	uploadObject(
		backendId: string,
		bucket: string,
		file: File,
		key?: string,
		contentType?: string
	): Promise<ObjectInfo> {
		const fd = new FormData();
		fd.set('file', file);
		if (key) fd.set('key', key);
		if (contentType) fd.set('content_type', contentType);
		return this.request(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/object`,
			{ method: 'POST', body: fd }
		);
	}

	deleteObject(backendId: string, bucket: string, key: string): Promise<void> {
		return this.request<void>(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/object`,
			{ method: 'DELETE', query: { key } }
		);
	}

	bulkDelete(
		backendId: string,
		bucket: string,
		keys: ObjectIdentifier[]
	): Promise<BulkDeleteResult> {
		return this.request(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/objects/delete`,
			{ method: 'POST', body: { keys } }
		);
	}

	createFolder(backendId: string, bucket: string, key: string): Promise<ObjectInfo> {
		return this.request(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/objects/folder`,
			{ method: 'POST', body: { key } }
		);
	}

	/**
	 * Fetch the object's tag set. Empty when none are set or the backend
	 * doesn't support tagging.
	 */
	async getObjectTags(
		backendId: string,
		bucket: string,
		key: string,
		versionId?: string
	): Promise<Record<string, string>> {
		const res = await this.request<{ tags: Record<string, string> }>(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/object/tags`,
			{ query: { key, version_id: versionId } }
		);
		return res.tags ?? {};
	}

	/** Replace the object's entire tag set. Empty map clears all tags. */
	setObjectTags(
		backendId: string,
		bucket: string,
		key: string,
		tags: Record<string, string>,
		versionId?: string
	): Promise<void> {
		return this.request<void>(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/object/tags`,
			{ method: 'PUT', query: { key, version_id: versionId }, body: { tags } }
		);
	}

	/**
	 * List versions of a single object. Only available when the backend
	 * advertises Capabilities.versioning; returns a single "latest" row on
	 * non-versioned buckets and an empty list if the object doesn't exist.
	 */
	async listObjectVersions(
		backendId: string,
		bucket: string,
		key: string
	): Promise<ObjectVersion[]> {
		const res = await this.request<{ versions: ObjectVersion[] }>(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/object/versions`,
			{ query: { key } }
		);
		return res.versions ?? [];
	}

	/**
	 * Replace user metadata. Implemented server-side as a self-copy with
	 * the REPLACE directive; creates a new version on versioned buckets.
	 */
	updateObjectMetadata(
		backendId: string,
		bucket: string,
		key: string,
		metadata: Record<string, string>
	): Promise<ObjectInfo> {
		return this.request(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/object/metadata`,
			{ method: 'PUT', query: { key }, body: { metadata } }
		);
	}

	/**
	 * Server-side copy within the same backend. Destination bucket defaults
	 * to the source. Pass `metadata` (even empty) to replace; omit to
	 * preserve the source's metadata.
	 */
	copyObject(
		backendId: string,
		bucket: string,
		srcKey: string,
		dstKey: string,
		opts: {
			dstBucket?: string;
			dstBackend?: string;
			versionId?: string;
			metadata?: Record<string, string>;
		} = {}
	): Promise<{ bucket: string; backend?: string; object?: ObjectInfo; key?: string }> {
		const body: Record<string, unknown> = { src_key: srcKey, dst_key: dstKey };
		if (opts.dstBucket) body.dst_bucket = opts.dstBucket;
		if (opts.dstBackend) body.dst_backend = opts.dstBackend;
		if (opts.versionId) body.version_id = opts.versionId;
		if (opts.metadata !== undefined) body.metadata = opts.metadata;
		return this.request(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/object/copy`,
			{ method: 'POST', body }
		);
	}

	/**
	 * Rename an object within a single bucket: server-side copy followed by
	 * delete-source. Not atomic — both legs can fail independently — but no
	 * backend S3 API exposes atomic rename, so this is the standard pattern.
	 */
	async renameObject(
		backendId: string,
		bucket: string,
		srcKey: string,
		dstKey: string
	): Promise<void> {
		if (srcKey === dstKey) return;
		await this.copyObject(backendId, bucket, srcKey, dstKey);
		await this.deleteObject(backendId, bucket, srcKey);
	}

	/**
	 * Recursive copy of every object under `srcPrefix` into `dstPrefix`. The
	 * server streams an NDJSON event log; `onEvent` fires for each parsed event
	 * (one of "start", "object", "error", "done"). Resolves to the terminal
	 * "done" event so callers can branch on copied/failed totals.
	 *
	 * Sources are not deleted — for a folder move, sequence with `deletePrefix`.
	 */
	copyPrefix(
		backendId: string,
		bucket: string,
		srcPrefix: string,
		dstPrefix: string,
		opts: {
			dstBucket?: string;
			dstBackend?: string;
			metadata?: Record<string, string>;
			onEvent?: (ev: PrefixEvent) => void;
			signal?: AbortSignal;
		} = {}
	): Promise<PrefixDoneEvent> {
		const body: Record<string, unknown> = { src_prefix: srcPrefix, dst_prefix: dstPrefix };
		if (opts.dstBucket) body.dst_bucket = opts.dstBucket;
		if (opts.dstBackend) body.dst_backend = opts.dstBackend;
		if (opts.metadata !== undefined) body.metadata = opts.metadata;
		return this.streamNDJSON(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/objects/copy-prefix`,
			body,
			opts.onEvent,
			opts.signal
		);
	}

	/**
	 * Recursive delete of every object under `prefix`. Empty prefix is rejected
	 * by the server (use `deleteBucket` for that). Streams the same NDJSON
	 * shape as `copyPrefix`.
	 */
	deletePrefix(
		backendId: string,
		bucket: string,
		prefix: string,
		opts: { onEvent?: (ev: PrefixEvent) => void; signal?: AbortSignal } = {}
	): Promise<PrefixDoneEvent> {
		return this.streamNDJSON(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/objects/delete-prefix`,
			{ prefix },
			opts.onEvent,
			opts.signal
		);
	}

	private async streamNDJSON(
		path: string,
		body: unknown,
		onEvent?: (ev: PrefixEvent) => void,
		signal?: AbortSignal
	): Promise<PrefixDoneEvent> {
		const headers = new Headers({ 'Content-Type': 'application/json' });
		const csrf = csrfToken();
		if (csrf) headers.set('X-CSRF-Token', csrf);

		const res = await this.fetcher(buildUrl(path), {
			method: 'POST',
			headers,
			body: JSON.stringify(body),
			credentials: 'same-origin',
			signal
		});
		if (!res.ok) {
			// Error responses still come back as JSON, not NDJSON.
			let payload: ApiErrorPayload = { code: 'unknown', message: `${res.status}` };
			try {
				const j = await res.json();
				payload = (j?.error ?? j) as ApiErrorPayload;
			} catch {
				/* ignore — body wasn't JSON */
			}
			throw new ApiException(res.status, {
				code: payload.code ?? 'unknown',
				message: messageFor(payload.code ?? 'unknown', payload.message),
				detail: payload.detail
			});
		}
		if (!res.body) {
			throw new ApiException(500, { code: 'no_body', message: 'no response body' });
		}

		const reader = res.body.getReader();
		const decoder = new TextDecoder();
		let buf = '';
		let done: PrefixDoneEvent | null = null;

		for (;;) {
			const { value, done: streamDone } = await reader.read();
			if (value) buf += decoder.decode(value, { stream: true });
			let newline = buf.indexOf('\n');
			while (newline !== -1) {
				const line = buf.slice(0, newline).trim();
				buf = buf.slice(newline + 1);
				newline = buf.indexOf('\n');
				if (!line) continue;
				let ev: PrefixEvent;
				try {
					ev = JSON.parse(line) as PrefixEvent;
				} catch {
					continue; // skip malformed lines rather than abort
				}
				onEvent?.(ev);
				if (ev.event === 'done') done = ev;
			}
			if (streamDone) break;
		}
		// Drain whatever's left in the buffer.
		const tail = buf.trim();
		if (tail) {
			try {
				const ev = JSON.parse(tail) as PrefixEvent;
				onEvent?.(ev);
				if (ev.event === 'done') done = ev;
			} catch {
				/* ignore */
			}
		}
		if (!done) {
			throw new ApiException(500, {
				code: 'no_done',
				message: 'stream ended without a done event'
			});
		}
		return done;
	}

	// ---- Multipart ------------------------------------------------------

	createMultipart(
		backendId: string,
		bucket: string,
		key: string,
		contentType?: string,
		opts?: { ifNoneMatch?: '*' }
	): Promise<{ bucket: string; key: string; upload_id: string }> {
		return this.request(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/multipart/`,
			{
				method: 'POST',
				query: { key },
				body: contentType ? { content_type: contentType } : undefined,
				headers: opts?.ifNoneMatch ? { 'If-None-Match': opts.ifNoneMatch } : undefined
			}
		);
	}

	completeMultipart(
		backendId: string,
		bucket: string,
		key: string,
		uploadId: string,
		parts: { PartNumber: number; ETag: string }[]
	): Promise<ObjectInfo> {
		return this.request(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/multipart/complete`,
			{ method: 'POST', query: { key, upload_id: uploadId }, body: { parts } }
		);
	}

	abortMultipart(backendId: string, bucket: string, key: string, uploadId: string): Promise<void> {
		return this.request(
			`/api/backends/${encodeURIComponent(backendId)}/buckets/${encodeURIComponent(bucket)}/multipart/`,
			{ method: 'DELETE', query: { key, upload_id: uploadId } }
		);
	}

	// ---- Shares ---------------------------------------------------------

	/** Mint a new share. The returned `url` is path-only; combine with the
	 * current origin to get the user-facing link. */
	createShare(req: CreateShareRequest): Promise<Share> {
		return this.request<Share>('/api/shares/', { method: 'POST', body: req });
	}

	/** List my shares, or all shares when scope="all" (admin-only). */
	async listShares(scope: 'mine' | 'all' = 'mine'): Promise<Share[]> {
		const res = await this.request<{ shares: Share[] }>('/api/shares/', {
			query: scope === 'all' ? { scope: 'all' } : {}
		});
		return res.shares ?? [];
	}

	revokeShare(id: string): Promise<void> {
		return this.request<void>(`/api/shares/${encodeURIComponent(id)}`, { method: 'DELETE' });
	}

	/** Full public share URL based on the current origin (browser only). */
	shareURL(share: Pick<Share, 'url'> | string): string {
		const path = typeof share === 'string' ? share : share.url;
		if (typeof window === 'undefined') return path;
		return window.location.origin + path;
	}
}

function buildUrl(
	path: string,
	query?: Record<string, string | number | boolean | undefined>
): string {
	if (!query) return path;
	const sp = new URLSearchParams();
	for (const [k, v] of Object.entries(query)) {
		if (v === undefined || v === '' || v === false) continue;
		sp.set(k, String(v));
	}
	const qs = sp.toString();
	return qs ? `${path}?${qs}` : path;
}

/** Shared client. Components and load functions both use this. */
export const api = new ApiClient();
