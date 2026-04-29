// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

/**
 * Maps backend error codes to user-facing copy.
 * Spec §10 ground rule: every user-facing string lives here from day one.
 */
export const errorCopy: Record<string, string> = {
	unauthorized: 'Sign in to continue.',
	forbidden: "You don't have permission to do that.",
	csrf_invalid: 'Your session is out of date. Reload and try again.',
	invalid_credentials: 'Invalid credentials.',
	account_locked: 'Account temporarily locked. Try again in a few minutes.',
	mode_disabled: 'That sign-in method is not enabled.',
	weak_password: 'Password does not meet the policy.',
	username_taken: 'That username is already taken.',
	email_taken: 'That email is already in use.',
	self_delete: 'You cannot delete your own account.',
	not_found: 'Not found.',
	bad_request: 'Bad request.',
	too_large: 'File is too large for a single upload.',
	invalid_key: 'That object key is invalid.',
	invalid_bucket_name:
		'Bucket name is invalid (3–63 chars, lowercase letters, digits, "-" or ".").',
	backend_error: 'The backend rejected the request.',
	internal: 'Something went wrong. Try again.',
	rate_limited: 'Too many attempts. Try again in a few minutes.',
	session_error: 'Could not start a session. Try again.',
	not_local: 'Only local accounts can change passwords here.',
	static_user: 'Static admin users cannot change their password at runtime.',
	not_implemented: 'Coming in a later phase.',
	id_taken: 'An endpoint with this id already exists.',
	yaml_managed:
		'This endpoint is defined in config.yaml. Edit the file and restart Stowage to change it.',
	secret_key_unset:
		'Set the STOWAGE_SECRET_KEY environment variable to manage endpoints from the UI.',
	store_unavailable: 'Endpoint storage is not configured.',
	register_failed: 'The endpoint was saved but could not be registered as live.'
};

export function messageFor(code: string, fallback?: string): string {
	return errorCopy[code] ?? fallback ?? 'Something went wrong.';
}
