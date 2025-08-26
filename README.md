# ModSentinel

ModSentinel is a lightweight web app for tracking Minecraft mods.
Paste in a Modrinth or CurseForge link, select the target Minecraft version,
loader (Fabric, Forge, Quilt, etc.) and release channel, and ModSentinel will
watch for updates.

## Disclaimer

This project is almost exclusively **vibe-coded**.  
It has not been thoroughly reviewed, tested, or hardened.

**Do not expose this application directly to the internet.**  
It is intended for local use, experimentation, or personal setups only.  
Use at your own risk.

## Features

- Server-rendered Go app using chi, templates and [htmx](https://htmx.org)
- SQLite storage via `modernc.org/sqlite` for CGO-free builds
- Periodic update checks with `gocron`
- Structured logging with `zerolog`

## PufferPanel integration

ModSentinel can sync mod lists directly from a [PufferPanel](https://pufferpanel.com) server.

### Creating an OAuth2 client

1. Sign in to PufferPanel and open the account menu in the top‑right.
2. Choose **OAuth2 Clients**, add a new application, and copy the generated client ID and secret. Clients are scoped to your account.
3. Grant the application the scopes `server.view`, `server.files.view`, and `server.files.edit`.

### Connecting

1. Open ModSentinel **Settings** → **PufferPanel** and enter your base URL (include `http://` or `https://` and omit the trailing slash), client ID, client secret, and scopes.
2. During sync ModSentinel scans the `mods/` directory, falling back to `plugins/` if no jars are found.
3. Enabling **Deep Scan** downloads each jar to read embedded metadata; this increases bandwidth and API usage.
4. PufferPanel's `/api/servers` endpoint returns an object with `servers` and `paging` fields; ModSentinel walks `paging.next` until all pages (up to 1,000 servers) are fetched.
5. Failed requests surface the backend message and a `requestId` so issues can be traced in server logs.

See [docs/PUFFERPANEL.md](docs/PUFFERPANEL.md) for details.

## Content Security Policy

Development builds allow `'unsafe-inline'` styles for convenience.
Production adds a nonce to runtime style tags so `style-src` can be `'self' 'nonce-<random>'` without `unsafe-inline`.
`connect-src` is restricted to the configured PufferPanel host and `img-src` permits only `data:` and `https:` URLs.

## Error responses

All HTTP errors return JSON with the shape `{code, message, details?, requestId}`.
`details` may be omitted. PufferPanel failures map to the proper 400/401/403/502/500 codes so clients never see empty bodies.

## Secret storage & rotation

Modrinth tokens and PufferPanel credentials are never stored in the browser.
They live encrypted in the `secrets` table of the SQLite database under
`/data/modsentinel.db`. A 32‑byte master key is generated on first boot, then
wrapped with a key derived from the required `MODSENTINEL_NODE_KEY`
environment variable and stored in `app_settings`. The node key must be at
least 16 characters; anything shorter than 32 characters will emit a warning at
startup.
Use the Settings UI or `POST /settings/secret/:type` to rotate or **Revoke &
Clear** a secret. Modrinth tokens should be read‑only with `project.read` and
`version.read` scopes, while PufferPanel requires `server.view`,
`server.files.view`, and `server.files.edit`.

To rotate the node key itself, use the Settings page's **Rewrap** action or run
`modsentinel admin rewrap --node-key <new>` and then update the
`MODSENTINEL_NODE_KEY` environment variable before restarting.

Back up the `/data` directory regularly. The node key itself is never stored;
keep `MODSENTINEL_NODE_KEY` safe and supply the same value when restoring a
backup or stored secrets cannot be decrypted. Startup fails if the node key is
missing, too short, or cannot decrypt existing secrets. See
[docs/SECRETS.md](docs/SECRETS.md) for details.

The `/health/secure` endpoint reports whether a wrapped key is present and the
KDF/AEAD algorithms in use.

## Development

```bash
cd frontend
npm ci
npm run build
cd ..
go build
./modsentinel
```

The server listens on `:8080`.

## Docker

```bash
docker build -t modsentinel .
docker run -p 8080:8080 -v modsentinel-data:/data -e MODSENTINEL_NODE_KEY=changeme nl2109/modsentinel
```

### Docker Compose

```yaml
services:
  modsentinel:
    image: nl2109/modsentinel:latest
    container_name: modsentinel
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
    environment:
      - MODSENTINEL_NODE_KEY=changeme
```

## Deployment notes

ModSentinel can run under any domain, such as `mods.example.com`. The app derives its own API URLs from the current origin, so the hostname does not need to be hardcoded.

If you place the container behind a reverse proxy like nginx or Nginx Proxy Manager:

- Forward both `/` and `/api/*` to the ModSentinel container.
- Pass through the `Authorization` header.
- Avoid `try_files` rules that return 404 for `/api/*` before the request reaches the backend.
