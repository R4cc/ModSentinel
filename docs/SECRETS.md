# Secret Management

ModSentinel keeps all third-party credentials on the server so they never reach the browser.
This document covers where secrets are stored, how to rotate them, required scopes, and backup caveats.

## Where secrets live

- Secrets are stored in the `secrets` table of the SQLite database at `/data/modsentinel.db`.
- Values are stored in plaintext. Only the last four characters and timestamp metadata are queryable through the API; plaintext never leaves the server.

## Rotation & revocation

- Use `POST /settings/secret/:type` to set or replace a secret. The Settings UI offers Save, Test, and **Revoke & Clear** actions.
- Rotating a secret invalidates cached plaintext. Revoking removes the database entry and zeroes in-memory caches.
- Cached secrets expire after roughly ten minutes even without rotation.
- OAuth token refreshes replace the stored record in one transaction and append a timestamped audit row without storing any token material.

## Required scopes

- **Modrinth token**: must be read-only with `project.read` and `version.read` scopes.
- **PufferPanel credentials**: OAuth client requires `server.view`, `server.files.view`, and `server.files.edit` scopes.

## Backup & restore

- Back up the `/data` directory. Secrets are not logged or exported; database dumps include only stored values.
