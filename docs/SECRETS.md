# Secret Management

ModSentinel keeps all third-party credentials on the server so they never reach the browser.
This document covers where secrets are stored, how to rotate them, required scopes, and backup caveats.

## Where secrets live

- Secrets are stored in the `secrets` table of the SQLite database at `/data/modsentinel.db`.
- Values are encrypted with AES-256-GCM using a master key generated on first boot. The key is wrapped with a key derived from the `MODSENTINEL_NODE_KEY` environment variable and stored in the `app_settings` table. The node key must be at least 16 characters; lengths under 32 characters trigger a startup warning.
- Only the last four characters and timestamp metadata are queryable through the API; plaintext never leaves the server.

## Rotation & revocation

- Use `POST /settings/secret/:type` to set or replace a secret. The Settings UI offers Save, Test, and **Revoke & Clear** actions.
- Rotating a secret invalidates cached plaintext. Revoking removes the database entry and zeroes in-memory caches.
- Cached secrets expire after roughly ten minutes even without rotation.
- To rotate the node key itself, use the Settings page's **Rewrap** action or run
  `modsentinel admin rewrap --node-key <new>` and update the
  `MODSENTINEL_NODE_KEY` environment variable before restarting.
- OAuth token refreshes replace the encrypted record in one transaction and append a timestamped audit row without storing any token material.

## Required scopes

- **Modrinth token**: must be read-only with `project.read` and `version.read` scopes.
- **PufferPanel credentials**: OAuth client requires `server.view`, `server.files.view`, and `server.files.edit` scopes.

## Backup & restore

- Back up the `/data` directory. The node key is never written to disk, so you must retain the same `MODSENTINEL_NODE_KEY` value to decrypt existing secrets. Startup fails if the node key is missing, too short, or cannot decrypt the wrapped master key.
- Restoring the database with a different node key will render all secrets unusable until they are re-entered.
- Secrets are not logged or exported; database dumps include only encrypted values.
- `/health/secure` returns `{"key_wrapped":true,"kdf":"argon2id","aead":"aes-gcm"}` to confirm a wrapped key is present.
