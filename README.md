# ModSentinel

ModSentinel monitors Minecraft server mods and keeps them up to date. It can:

- Discover the current mod/plugin set of a server by reading from PufferPanel.
- Track mod metadata and updates via the Modrinth API.
- Surface available updates and apply them according to your chosen loader.

The backend is a Go HTTP API with a React/Vite SPA embedded into the binary. Data is stored in a single SQLite database.

## Architecture

- Backend: Go (chi router), exposed under `/api/*`.
- Frontend: React + Vite, served by the backend from an embedded filesystem. A catch‑all route (`/*`) serves `index.html` so client‑side routing works.
- Storage: SQLite (WAL mode), persisted under `/data` in the container.
- Background jobs: periodic update checks against Modrinth; optional PufferPanel sync tasks.

See the API surface in `docs/openapi.yaml`.

## PufferPanel Integration

- Reads a server’s mod/plugin directory via the PufferPanel API to bootstrap and periodically refresh instance inventories.
- Works alongside Modrinth metadata to determine available versions and channels.

## Deployment (Docker / Compose)

The published image runs as a non‑root user and expects a writable `/data` directory. Use a named volume or a bind‑mounted directory owned by UID 65532.

Example `docker-compose.yml`:

```yaml
services:
  modsentinel:
    image: nl2109/modsentinel:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - APP_ENV=production
      # - ADMIN_TOKEN=change-me   # optional: protects admin endpoints
    volumes:
      - modsentinel-data:/data

volumes:
  modsentinel-data: {}
```

Start it:

```bash
docker compose up -d
```

Ingress/proxy note: route both `/api/*` and `/*` to the service so the SPA is served for non‑API routes.

## Configuration

Environment variables:

- `APP_ENV`: `production` (recommended in containers) or `development`.
- `ADMIN_TOKEN` (optional): if set, admin endpoints require `Authorization: Bearer <token>`.
- `MODSENTINEL_MODRINTH_TOKEN` (optional): seeds a Modrinth token on startup for authenticated API usage; can also be configured via the settings API.

Secrets (tokens/credentials) are stored in the SQLite DB. Back up `/data` regularly if these are important for your setup.

## First‑Run Flow

1. Open the UI at `/` and set the Modrinth token (and optionally PufferPanel credentials) in Settings.
2. Create an instance by linking a PufferPanel server or specifying loader/name directly.
3. Add or discover mods. The scheduler checks for updates periodically and surfaces them in the dashboard. You can then apply updates from the UI.

## Troubleshooting

- 404s on UI routes: ensure the frontend assets are embedded (use the provided image) and your proxy forwards `/*` to the app.
- Read‑only DB errors: the container runs as UID 65532; make sure `/data` is writable (named volume recommended). On SELinux hosts, add `:Z` to bind mounts.
