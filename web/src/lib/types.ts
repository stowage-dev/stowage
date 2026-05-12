// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// API DTOs — wire shapes for the Go backend in internal/api/.
// Keep names + casing aligned with the JSON the server emits.

// ---- Auth ----------------------------------------------------------------

export type AuthMode = 'local' | 'oidc' | 'static';

export interface AuthConfig {
	modes: AuthMode[];
	allow_self_registration: boolean;
	// Surfaced via the static-mode banner. Server doesn't send this directly,
	// it's derived from `modes.includes('static')`.
}

export interface Me {
	id: string;
	username: string;
	role: 'admin' | 'editor' | 'viewer' | 'user' | 'readonly';
	identity_source: string; // 'local' | 'static' | `oidc:<issuer>`
	must_change_pw: boolean;
	email?: string;
}

export interface LoginResult {
	status: 'ok';
	source: string;
	must_change_pw?: boolean;
}

// ---- Admin users ---------------------------------------------------------

export interface User {
	id: string;
	username: string;
	email?: string;
	role: 'admin' | 'editor' | 'viewer' | 'user' | 'readonly';
	identity_source: string;
	enabled: boolean;
	must_change_pw: boolean;
	failed_attempts: number;
	locked_until?: string;
	created_at: string;
	last_login_at?: string;
}

export interface UserListFilter {
	query?: string;
	role?: string;
	source?: string;
	enabled?: boolean;
	limit?: number;
	offset?: number;
}

export interface CreateUserRequest {
	username: string;
	email?: string;
	password: string;
	role: 'admin' | 'editor' | 'viewer';
	must_change_pw?: boolean;
}

export interface PatchUserRequest {
	role?: string;
	enabled?: boolean;
	email?: string;
}

// ---- Backends + buckets --------------------------------------------------

export interface Capabilities {
	versioning: boolean;
	object_lock: boolean;
	lifecycle: boolean;
	bucket_policy: boolean;
	cors: boolean;
	tagging: boolean;
	server_side_encrypt: boolean;
	admin_api: '' | 'minio' | 'garage' | 'seaweedfs';
	max_multipart_parts: number;
	max_part_size_bytes: number;
}

export interface Backend {
	id: string;
	name: string;
	capabilities: Capabilities;
	healthy: boolean;
	last_probe_at?: string;
	last_error?: string;
}

// ---- Admin endpoint management -----------------------------------------

/**
 * One row of /api/admin/backends. Mirrors the Go adminBackendDTO. The
 * cleartext secret is never returned — only `secret_set` indicates whether
 * one is stored.
 */
export interface Endpoint {
	id: string;
	name: string;
	type: string;
	endpoint: string;
	region: string;
	path_style: boolean;
	access_key?: string;
	secret_set: boolean;
	enabled: boolean;
	source: 'config' | 'db';
	healthy: boolean;
	last_error?: string;
	last_probe_at?: string;
	created_at?: string;
	updated_at?: string;
}

export interface CreateEndpointRequest {
	id: string;
	name?: string;
	type?: string;
	endpoint: string;
	region?: string;
	path_style?: boolean;
	access_key: string;
	secret_key: string;
	enabled?: boolean;
}

/**
 * All fields optional. Omit secret_key to leave the stored secret in place;
 * include it (even as the empty string) to overwrite.
 */
export interface PatchEndpointRequest {
	name?: string;
	endpoint?: string;
	region?: string;
	path_style?: boolean;
	access_key?: string;
	secret_key?: string;
	enabled?: boolean;
}

export interface TestEndpointRequest {
	type?: string;
	endpoint: string;
	region?: string;
	path_style?: boolean;
	access_key: string;
	secret_key: string;
}

export interface TestEndpointResult {
	healthy: boolean;
	latency_ms: number;
	error?: string;
}

export interface Bucket {
	name: string;
	created_at?: string;
	// True when the per-bucket size-tracking toggle is on. Defaults to
	// true on the server when no override row exists.
	size_tracked: boolean;
	// Cached recursive size for the bucket root, populated only when
	// tracking is on AND the scanner has run at least once. Absent =
	// "not yet known", which the UI shows as a dash rather than 0.
	size_bytes?: number;
	object_count?: number;
	computed_at?: string;
}

export interface BucketSizeTracking {
	enabled: boolean;
	updated_at?: string;
	updated_by?: string;
}

export interface PrefixSize {
	bytes: number;
	count: number;
	prefix: string;
	computed_at: string;
}

// ---- Objects -------------------------------------------------------------

export interface ObjectInfo {
	key: string;
	size: number;
	etag?: string;
	content_type?: string;
	storage_class?: string;
	version_id?: string;
	last_modified?: string;
	metadata?: Record<string, string>;
}

export interface ListObjectsResult {
	prefix: string;
	delimiter: string;
	common_prefixes: string[] | null;
	objects: ObjectInfo[];
	is_truncated: boolean;
	next_continuation_token: string;
}

export interface ListObjectsRequest {
	prefix?: string;
	delimiter?: string;
	token?: string;
	max_keys?: number;
}

export interface ObjectIdentifier {
	key: string;
	version_id?: string;
}

export interface ObjectVersion {
	key: string;
	version_id: string;
	is_latest: boolean;
	is_delete_marker?: boolean;
	size: number;
	etag?: string;
	storage_class?: string;
	last_modified?: string;
}

export interface BulkDeleteResult {
	deleted: { key: string; version_id?: string }[] | null;
	errors: { key: string; code: string; message: string }[] | null;
}

/**
 * Event types streamed by the copy-prefix and delete-prefix endpoints. The
 * server emits one JSON object per line; the API client parses them and hands
 * them to the caller via an onEvent callback.
 */
export type PrefixEvent =
	| { event: 'start'; prefix?: string; src_prefix?: string; dst_prefix?: string }
	| { event: 'object'; key?: string; src?: string; dst?: string; size?: number }
	| {
			event: 'error';
			key?: string;
			src?: string;
			dst?: string;
			code: string;
			message: string;
	  }
	| PrefixDoneEvent;

export interface PrefixDoneEvent {
	event: 'done';
	copied?: number;
	deleted?: number;
	failed: number;
	bytes?: number;
	cancelled?: boolean;
}

// ---- View-side types not sourced from the API ----------------------------

/** What the ObjectTable / icon renderers want — derived from ObjectInfo. */
export type ObjectKind = 'folder' | 'image' | 'video' | 'pdf' | 'text' | 'file';

export interface BrowserItem {
	key: string; // for folders: trailing-slash prefix relative to bucket root
	displayName: string; // last path segment without trailing slash
	kind: ObjectKind;
	size: number | null; // null for folders
	modified: string | null;
	ct?: string;
	etag?: string;
}

export type UploadStatus = 'uploading' | 'paused' | 'done' | 'canceled' | 'error' | 'conflict';

export interface UploadItem {
	id: string;
	name: string;
	kind: ObjectKind;
	progress: number;
	error?: string;
	status: UploadStatus;
}

// ---- Bucket settings -----------------------------------------------------

export interface CORSRule {
	allowed_origins: string[];
	allowed_methods: string[];
	allowed_headers?: string[];
	expose_headers?: string[];
	max_age_seconds?: number;
}

export interface LifecycleRule {
	id?: string;
	prefix?: string;
	enabled: boolean;
	expiration_days?: number;
	noncurrent_expire_days?: number;
	abort_incomplete_days?: number;
	transition_days?: number;
	transition_storage_class?: string;
}

export interface BucketPin {
	backend_id: string;
	bucket: string;
	created_at: string;
}

export interface BucketQuota {
	configured: boolean;
	soft_bytes?: number;
	hard_bytes?: number;
	updated_at?: string;
	updated_by?: string;
	usage_bytes?: number;
	object_count?: number;
	computed_at?: string;
	has_usage: boolean;
}

// ---- Shares --------------------------------------------------------------

export interface Share {
	id: string;
	code: string;
	url: string; // path-only, e.g. "/s/abc123xyz"
	backend_id: string;
	bucket: string;
	key: string;
	created_by: string;
	created_at: string;
	expires_at?: string;
	has_password: boolean;
	max_downloads?: number;
	download_count: number;
	revoked: boolean;
	revoked_at?: string;
	last_accessed_at?: string;
	disposition: 'attachment' | 'inline';
}

export interface CreateShareRequest {
	backend_id: string;
	bucket: string;
	key: string;
	expires_at?: string; // RFC3339
	password?: string;
	max_downloads?: number;
	disposition?: 'attachment' | 'inline';
}

/** Internal Route used by the breadcrumb / sidebar / palette. */
export type Route =
	| { type: 'backends' }
	| { type: 'backend'; backend: string }
	| { type: 'bucket'; backend: string; bucket: string; prefix: string[] }
	| { type: 'shares' }
	| { type: 'search' }
	| { type: 'admin-users' }
	| { type: 'admin-audit' }
	| { type: 'admin-dashboard' }
	| { type: 'admin-health' }
	| { type: 'admin-s3-proxy' }
	| { type: 'me-s3-credentials' };

// ---- S3 proxy (Phase 6 Slice D) ----------------------------------------

/**
 * One row of /api/admin/s3-credentials or /api/me/s3-credentials. Mirrors
 * the Go s3CredDTO. SecretKey is set only on the create response and never
 * persisted client-side.
 */
export interface S3Credential {
	access_key: string;
	secret_key?: string;
	backend_id: string;
	buckets: string[];
	user_id?: string;
	description?: string;
	enabled: boolean;
	expires_at?: string;
	created_at: string;
	created_by?: string;
	updated_at: string;
	updated_by?: string;
	/**
	 * Base proxy endpoint (the value for AWS_ENDPOINT_URL). Comes from
	 * `s3_proxy.public_hostname` when set, otherwise the cluster-internal
	 * `operator.proxy_url`. Empty/absent means neither is configured.
	 */
	endpoint_url?: string;
	/**
	 * Per-bucket access URLs computed with the credential's backend
	 * addressing style — path-style buckets land under the path, virtual-
	 * hosted buckets appear as a subdomain of `endpoint_url`'s host.
	 */
	bucket_urls?: Record<string, string>;
}

export interface CreateS3CredentialRequest {
	backend_id: string;
	buckets: string[];
	description?: string;
	expires_at?: string;
	/** Admin-only — assign the credential to a stowage user for audit attribution. */
	user_id?: string;
}

export interface PatchS3CredentialRequest {
	buckets?: string[];
	description?: string;
	enabled?: boolean;
	expires_at?: string; // empty string clears
}

export type S3CredentialSource = 'sqlite' | 'kubernetes';

/**
 * Merged-view row returned by /api/admin/s3-proxy/credentials. UI-managed
 * (sqlite) rows are mutable through the per-source admin endpoints; operator
 * (kubernetes) rows are read-only — they live in K8s Secrets and rotate via
 * BucketClaim reconciliation.
 */
export interface S3CredentialView {
	access_key: string;
	backend_id: string;
	buckets: string[];
	user_id?: string;
	description?: string;
	enabled: boolean;
	expires_at?: string;
	created_at?: string;
	created_by?: string;
	updated_at?: string;
	updated_by?: string;
	source: S3CredentialSource;
	claim_namespace?: string;
	claim_name?: string;
	endpoint_url?: string;
	bucket_urls?: Record<string, string>;
}

export interface S3AnonymousBinding {
	backend_id: string;
	bucket: string;
	mode: string;
	per_source_ip_rps: number;
	created_at?: string;
	created_by?: string;
	source?: S3CredentialSource;
}

export interface UpsertS3AnonymousBindingRequest {
	backend_id: string;
	bucket: string;
	mode?: string;
	per_source_ip_rps?: number;
}

// ---- Search -------------------------------------------------------------

export interface SearchBucketHit {
	backend_id: string;
	bucket: string;
}

export interface SearchObjectHit {
	backend_id: string;
	bucket: string;
	key: string;
	size: number;
	last_modified?: string;
}

export interface SearchResponse {
	query: string;
	buckets: SearchBucketHit[];
	objects: SearchObjectHit[];
	truncated: boolean;
}

// ---- Backend health -----------------------------------------------------

export interface ProbeRecord {
	at: string;
	healthy: boolean;
	latency_ms: number;
	error?: string;
}

export interface BackendHealth {
	id: string;
	name: string;
	healthy: boolean;
	last_probe_at?: string;
	last_error?: string;
	latency_ms: number;
	history: ProbeRecord[];
}

// ---- Audit log ----------------------------------------------------------

export interface AuditEvent {
	id: number;
	timestamp: string;
	user_id?: string;
	action: string;
	backend?: string;
	bucket?: string;
	key?: string;
	request_id?: string;
	ip?: string;
	user_agent?: string;
	status: string;
	detail?: string; // JSON-encoded blob
}

export interface AuditFilter {
	user?: string;
	action?: string;
	backend?: string;
	bucket?: string;
	status?: string;
	from?: string;
	to?: string;
	limit?: number;
	offset?: number;
}

// ---- Admin dashboard ----------------------------------------------------

export interface DashboardHourlyPoint {
	unix_hour: number;
	requests: number;
	errors: number;
}

export interface DashboardErrorEvent {
	when: string;
	path: string;
	method: string;
	status: number;
	user_id?: string;
	backend?: string;
}

export interface DashboardRequests {
	total_24h: number;
	errors_24h: number;
	hourly: DashboardHourlyPoint[];
	by_backend: Record<string, number>;
	recent_errors: DashboardErrorEvent[];
}

export interface DashboardBackendStorage {
	backend_id: string;
	bytes: number;
	objects: number;
	buckets: number;
}

export interface DashboardTopBucket {
	backend_id: string;
	bucket: string;
	bytes: number;
	objects: number;
}

export interface AdminDashboard {
	requests: DashboardRequests;
	storage: {
		by_backend: DashboardBackendStorage[];
		top_buckets: DashboardTopBucket[];
		cache_note?: string;
	};
}

/** Per-backend cosmetic info — picked locally from the backend type heuristic. */
export type BackendKind =
	| 'garage'
	| 'minio'
	| 'seaweedfs'
	| 'aws'
	| 'b2'
	| 'r2'
	| 'wasabi'
	| 'generic';

export interface BackendKindInfo {
	label: string;
	color: string;
	letter: string;
}
