# Self-host Phaze Nexus on **phazechat.world** (no Fly.io)

Fly.io was only one way to run the relay. Production on your own domain is: **DNS → your machine → TLS → Nexus** (and optional static web build).

## 1. DNS

At your DNS host (Cloudflare, Namecheap, Route53, etc.):

| Type | Name | Value |
|------|------|--------|
| **A** | `@` (apex) | Your server’s public IPv4 |
| **AAAA** | `@` | Your server’s public IPv6 (recommended) |
| **A** | `www` | Same IPv4 (or CNAME to apex) |

Optional: `turn.phazechat.world` for TURN if you run CoTURN elsewhere.

Clients in this repo already default to **`https://phazechat.world`** / **`wss://phazechat.world/ws`** in the native `PhazeInfra` settings. Once Nexus listens on that host with a valid certificate, desktop and web can talk to it without Fly.

## 2. What runs on the server

- **Phaze Nexus** — Go binary or Docker image from `nexus_server/` (HTTP + WebSocket on `PORT`, default `8080`).
- **Reverse proxy** — Terminates HTTPS on `443` and forwards to Nexus (e.g. `127.0.0.1:8080`). Nexus does not need to bind `443` itself.

Keep the SQLite database on a **persistent volume** or path (`DB_PATH`), not inside an ephemeral container layer.

## 3. TLS + reverse proxy (recommended: Caddy)

Install [Caddy](https://caddyserver.com/) on the VPS. Example `Caddyfile`:

```caddy
phazechat.world, www.phazechat.world {
    encode gzip zstd
    reverse_proxy 127.0.0.1:8080
}
```

Caddy obtains Let’s Encrypt certificates automatically for those hostnames.

**WebSocket:** `reverse_proxy` forwards `Upgrade` and `Connection` by default in modern Caddy — WS to `/ws` works the same as HTTP.

### Nginx (alternative)

```nginx
server {
    listen 443 ssl http2;
    server_name phazechat.world www.phazechat.world;
    ssl_certificate     /etc/letsencrypt/live/phazechat.world/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/phazechat.world/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

Use certbot or your ACME workflow for the certificate paths.

## 4. Run Nexus with Docker Compose

From the repo (or copy `nexus_server/` to the server):

```bash
cd nexus_server
docker compose up -d --build
```

Compose binds Nexus to **localhost only**; Caddy/Nginx on the same host handles public `443`. Adjust `Phaze_ALLOWED_ORIGINS` in `docker-compose.yml` (or override with env) to match the exact origins that load your **web** app (e.g. `https://phazechat.world` if you serve the Vite build from the same host, or your CDN origin).

## 5. Environment variables (high signal)

| Variable | Purpose |
|----------|--------|
| `PORT` | Port Nexus listens on inside the machine (default `8080`). |
| `DB_PATH` | SQLite file path (must persist across restarts). |
| `Phaze_ALLOWED_ORIGINS` | Comma-separated `https://…` origins allowed for browser WebSockets. **Set in production** (empty = dev “allow all”). |
| `PHAZE_ASSET_ROOT` | If templates/public are not next to the binary, set to the directory that contains `templates/` and `public/`. Docker image sets `/app` — no change needed there. |
| `PHAZE_TURN_*` | Optional; see README TURN section. |
| `RESEND_*` / Twilio / Telnyx | Optional mail/SMS/PSTN; see server code and `docs/AUDIT.md`. |

## 6. Web client on the same domain

- **Option A:** Build `web/` (`npm run build`) and configure Caddy/nginx to serve `web/dist` as static files for `https://phazechat.world/` while proxying `/ws` and `/api` to Nexus — requires path-based routing rules so API + WS still hit Nexus.
- **Option B:** Serve only Nexus from apex; host the SPA on a subdomain (e.g. `app.phazechat.world`) with `VITE_NEXUS_WS=wss://phazechat.world/ws` at build time.

Simplest path for a single box: **Nexus already serves** marketing pages from `templates/` and `/public`; the React web beta is usually run separately (Vite dev or static host). Pick one layout and set `Phaze_ALLOWED_ORIGINS` to match where the browser loads the SPA.

## 7. What you remove by leaving Fly

- No `fly.toml`, no `fly deploy`, no Fly volumes CLI.
- You manage: OS updates, firewall (`ufw allow 443/tcp`), backups of `DB_PATH`, and certificate renewal (automatic with Caddy).

## 8. Health check

After DNS propagates:

```bash
curl -fsS https://phazechat.world/health
```

You should see JSON with `"status":"ok"`.

---

*If you later want a managed platform again (Render, Railway, etc.), the same container image and env vars apply; only the control plane changes.*
