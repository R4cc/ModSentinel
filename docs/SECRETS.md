# Secret Management

ModSentinel keeps all third-party credentials on the server so they never reach the browser.
This document covers where secrets are stored, how to rotate them, required scopes, and backup caveats.

## Where secrets live
- Secrets are stored in the `secrets` table of `mods.db`.
- Values are encrypted with AES-256-GCM using a keyset loaded from Cloud KMS or the `SECRET_KEYSET` environment variable.
- Only the last four characters and timestamp metadata are queryable through the API; plaintext never leaves the server.

## Rotation & revocation
- Use `POST /settings/secret/:type` to set or replace a secret. The Settings UI offers Save, Test, and **Revoke & Clear** actions.
- Rotating a secret bumps the key id and invalidates cached plaintext. Revoking removes the database entry and zeroes in-memory caches.
- Cached secrets expire after roughly ten minutes even without rotation.

## Required scopes
- **Modrinth token**: must be read-only with `project.read` and `version.read` scopes.
- **PufferPanel credentials**: OAuth client requires `server.view` and `server.files.view` scopes.

## Backup & restore
- Back up both `mods.db` and the encryption key material; without the same key, stored secrets cannot be decrypted.
- Restoring the database on a new host with a different keyset will render all secrets unusable until they are re-entered.
- Secrets are not logged or exported; database dumps include only encrypted values.
