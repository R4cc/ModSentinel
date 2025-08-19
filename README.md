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
