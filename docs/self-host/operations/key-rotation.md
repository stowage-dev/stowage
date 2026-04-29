---
type: how-to
---

# Rotating `STOWAGE_SECRET_KEY`

The AES-256 root key seals every endpoint secret and every virtual
credential at rest. It does **not** seal user passwords (those use
argon2id) or session cookies (those are HMAC-signed with a separate,
ephemeral key).

## When to rotate

- Suspected key compromise.
- Periodic rotation policy (annual, biennial).
- Personnel turnover where someone with key access has left.

## Caveat: there is no automated key migration today

The current implementation does not have a "rotate the master key,
re-seal everything" command. Rotating means re-creating every sealed
secret in place. The plan for an automated migration is on the
[roadmap](../../explanations/roadmap.md), but the manual procedure
below works.

## Manual rotation procedure

### 1. Stop Stowage and back up

```sh
sudo systemctl stop stowage
sudo cp /var/lib/stowage/stowage.db /backup/stowage-pre-rotation.db
```

### 2. Generate a new key

```sh
openssl rand -hex 32 > /etc/stowage/secret-new.key
chmod 0600 /etc/stowage/secret-new.key
```

### 3. Read out the old endpoint credentials

You'll need the access keys + secret keys of every UI-managed
endpoint. They're sealed in SQLite, so extract them with the *current*
key in place. The fastest way is via the dashboard — log in, go to
`/admin/endpoints`, copy each row's credentials offline.

For virtual credentials minted via `/admin/s3-credentials`, you'll
need to re-mint them and hand the new credentials to tenants.

### 4. Replace the key

```sh
sudo mv /etc/stowage/secret.key /etc/stowage/secret-old.key
sudo mv /etc/stowage/secret-new.key /etc/stowage/secret.key
```

### 5. Reset sealed state in SQLite

Delete the existing rows that were sealed under the old key:

```sql
-- These tables hold rows sealed under the master key.
-- Adjust names if the schema has evolved past v1.0.
DELETE FROM endpoint_secrets;
DELETE FROM s3_credentials;
DELETE FROM s3_anonymous_bindings;
```

### 6. Start Stowage with the new key

```sh
sudo systemctl start stowage
```

### 7. Re-create the endpoints and credentials

In `/admin/endpoints`, recreate each UI-managed endpoint with the
credentials you copied in step 3.

In `/admin/s3-proxy`, mint fresh virtual credentials and hand them to
the tenants who held the old ones.

### 8. Destroy the old key

After verifying everything works:

```sh
shred -u /etc/stowage/secret-old.key
```

## Why this is manual

A re-sealing migration would have to:

- Decrypt every sealed row with the old key.
- Re-seal every row with the new key.
- Persist the rotation atomically so a crash mid-rotation is
  recoverable.

Doing that safely without a downtime window is non-trivial. The
manual procedure makes the rotation visible and auditable, at the
cost of operator time. Until the automated tool ships, treat key
rotation like a planned maintenance window.

## Operational hygiene

- Store the new key offline immediately. If you generated it on the
  Stowage host and lost the host, you've lost the new key too.
- Keep the old key for 24h in a sealed envelope (or its digital
  equivalent) in case you need to roll back during the rotation
  window.
- Audit `backend.create` / `backend.update` rows after the rotation
  to confirm everything came back the way you expected.
