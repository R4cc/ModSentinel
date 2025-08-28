# ModSentinel

ModSentinel helps you keep track of Minecraft mods. Paste a Modrinth or CurseForge link, pick the Minecraft version and loader, and the app will watch for updates. It can also pull mod lists directly from a PufferPanel server so you always know what each instance is running.

## Features

- Track mods from Modrinth and CurseForge
- Optional PufferPanel sync for remote instances
- Simple web interface powered by Go and htmx
- Runs on SQLite with no external database needed

## Quick start with Docker Compose

1. Create a `docker-compose.yml` in an empty folder:

```yaml
services:
  modsentinel:
    image: nl2109/modsentinel:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
```

2. Start the app:

```bash
docker compose up -d
```

3. Visit [http://localhost:8080](http://localhost:8080) and follow the setup flow.

## Notes

ModSentinel is young software meant for hobby use. Avoid exposing it directly to the public internet.

### Secret storage

All third-party tokens and credentials are kept only on the server and are stored in plaintext inside the SQLite database. Make sure the `/data` directory is protected and regular backups are taken if secrets are important.
