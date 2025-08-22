# PufferPanel Integration

This guide explains how to connect ModSentinel to a PufferPanel server and how the sync process works.

## Connecting

1. Open **Settings** → **PufferPanel**.
2. Enter the Base URL of your panel along with a client ID and secret.
3. Grant the application the following scopes:
   - `server.view`
   - `server.files.view`
4. Test the connection, then save the credentials.

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

During sync ModSentinel lists `.jar` files from the server. It checks the following directories in order:

1. `mods/`
2. `plugins/` (fallback)

The first path with results is used.

## Deep scan

When the **Deep Scan** setting is enabled the sync handler downloads each jar and reads embedded metadata (for example `fabric.mod.json`). This improves matching accuracy but requires additional bandwidth and API requests. Requests are throttled to avoid overwhelming the panel.

