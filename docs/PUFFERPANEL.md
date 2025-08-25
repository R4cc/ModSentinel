# PufferPanel Integration

This guide explains how to connect ModSentinel to a PufferPanel server and how the sync process works.

## Connecting

1. In PufferPanel, open the account menu in the top‑right, choose **OAuth2 Clients**, and add a new application. Copy the client ID and secret; clients are tied to your account.
2. In ModSentinel, open **Settings** → **PufferPanel** and enter the Base URL (must start with `http://` or `https://` and omit any trailing slash), client ID, secret, and scopes.
3. Grant the application the following scopes:
   - `server.view`
   - `server.files.view`
   - `server.files.edit`
4. Test the connection, then save the credentials. Errors return JSON `{code, message, requestId}`; the request ID also appears in the UI for log correlation.

Once saved, ModSentinel will enable the **Sync from PufferPanel** option when creating or resyncing instances.

## Server listing

`GET /api/servers` returns a paginated object:

```json
{
  "servers": [...],
  "paging": {"next": "/api/servers?page=2"}
}
```

ModSentinel follows `paging.next` until all pages are retrieved or 1,000 servers have been collected.

Listing servers requires the `server.view` scope and reading jar files needs `server.files.view`.

## Jar discovery paths

During sync ModSentinel lists `.jar` files from the server. The expected folder depends on the server type:

1. Fabric/Forge → `mods/`
2. Paper/Spigot → `plugins/`

If the expected directory is missing, the sync returns `{code:"not_found"}` instead of falling back to the other path.

## Deep scan

When the **Deep Scan** setting is enabled the sync handler downloads each jar and reads embedded metadata (for example `fabric.mod.json`). This improves matching accuracy but requires additional bandwidth and API requests. Requests are throttled to avoid overwhelming the panel.
