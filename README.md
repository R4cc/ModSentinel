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

1. Open **Settings** → **PufferPanel** and enter your Base URL, client ID, and client secret.
2. The OAuth client must have the scopes `server.view` and `server.files.view`.
3. During sync ModSentinel scans the `mods/` directory, falling back to `plugins/` if no jars are found.
4. Enabling **Deep Scan** downloads each jar to read embedded metadata; this increases bandwidth and API usage.

See [docs/PUFFERPANEL.md](docs/PUFFERPANEL.md) for details.

## Secret storage & rotation

Modrinth tokens and PufferPanel credentials are never stored in the browser.
They live encrypted in the `secrets` table of `mods.db`, keyed by Cloud KMS or
the `SECRET_KEYSET` environment variable. Use the Settings UI or
`POST /settings/secret/:type` to rotate or **Revoke & Clear** a secret. Modrinth
tokens should be read-only with `project.read` and `version.read` scopes, while
PufferPanel requires `server.view` and `server.files.view`.

Backups must include both `mods.db` and the encryption key material; restoring
with a different keyset will invalidate stored secrets. See
[docs/SECRETS.md](docs/SECRETS.md) for details.

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
docker run -p 8080:8080 nl2109/modsentinel
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
      - ./mods.db:/mods.db
```
