# ModSentinel

ModSentinel is a lightweight web app for tracking Minecraft mods.
Paste in a Modrinth or CurseForge link, select the target Minecraft version,
loader (Fabric, Forge, Quilt, etc.) and release channel, and ModSentinel will
watch for updates.

## Features

- Server-rendered Go app using chi, templates and [htmx](https://htmx.org)
- SQLite storage via `modernc.org/sqlite` for CGO-free builds
- Periodic update checks with `gocron`
- Structured logging with `zerolog`

## Development

```bash
go build
./modsentinel
```

The server listens on `:8080`.

## Docker

```bash
docker build -t modsentinel .
docker run -p 8080:8080 modsentinel
```
