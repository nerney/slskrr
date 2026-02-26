# slskrr

Bridge [slskd](https://github.com/slskd/slskd) (Soulseek) to Radarr, Sonarr, and Prowlarr by exposing a [Newznab](https://newznab.readthedocs.io/) indexer and [SABnzbd](https://sabnzbd.org/) download client API.

## How it works

slskrr sits between your \*arr apps and slskd:

```
Prowlarr / Radarr / Sonarr
        │
        ▼
     slskrr (:6969)
    ┌───┴───┐
    │       │
  /api   /sabnzbd/api
 (Newznab) (SABnzbd)
    │       │
    └───┬───┘
        ▼
      slskd
    (Soulseek)
```

- **Newznab endpoint** (`/api`) — translates search queries into slskd searches and returns results as an NZB-compatible feed.
- **SABnzbd endpoint** (`/sabnzbd/api`) — accepts download requests from Radarr/Sonarr and triggers file transfers through slskd.
- **Health check** (`/health`) — returns `ok`.

## Quick start with Docker Compose

```bash
cp docker-compose.yml docker-compose.override.yml
# edit docker-compose.override.yml with your slskd URL and API key
docker compose up -d
```

## Docker

### Pull from GHCR

```bash
docker pull ghcr.io/nerney/slskrr:latest
```

### Run

```bash
docker run -d \
  -p 6969:6969 \
  -e SLSKD_URL=http://your-slskd:5030 \
  -e SLSKD_API_KEY=your-api-key \
  ghcr.io/nerney/slskrr:latest
```

### Build locally

```bash
docker build -t slskrr .
```

The Dockerfile uses a multistage build — compiles with `golang:1.24-alpine`, then copies the static binary into a `scratch` image. Final image is just the binary + CA certs (~10 MB).

## Build from source

Requires Go 1.24+:

```bash
go build -o slskrr .
```

## Configuration

All configuration is via environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SLSKD_URL` | yes | — | Base URL of your slskd instance |
| `SLSKD_API_KEY` | yes | — | slskd API key |
| `LISTEN_ADDR` | no | `:6969` | Address and port to listen on |
| `API_KEY` | no | — | API key for \*arr authentication |
| `SEARCH_TIMEOUT` | no | `30s` | Max time to wait for search results |
| `DOWNLOAD_DIR` | no | `/downloads/complete` | Path where completed downloads land |

## Usage

```bash
export SLSKD_URL=http://localhost:5030
export SLSKD_API_KEY=your-slskd-api-key
./slskrr
```

slskrr will start on port 6969 by default.

## Configuring your \*arr apps

### Prowlarr (indexer)

1. **Settings → Indexers → Add → Generic Newznab**
2. URL: `http://<slskrr-host>:6969`
3. API Path: `/api`
4. API Key: your `API_KEY` value (if set)

### Radarr / Sonarr (indexer)

1. **Settings → Indexers → Add → Newznab**
2. URL: `http://<slskrr-host>:6969`
3. API Path: `/api`
4. API Key: your `API_KEY` value (if set)

### Radarr / Sonarr (download client)

1. **Settings → Download Clients → Add → SABnzbd**
2. Host: `<slskrr-host>`
3. Port: `6969`
4. URL Base: `/sabnzbd`
5. API Key: your `API_KEY` value (if set)

## Endpoints

| Path | Protocol | Purpose |
|------|----------|---------|
| `/api` | Newznab | Search and RSS feed for indexers |
| `/sabnzbd/api` | SABnzbd | Download client for Radarr/Sonarr |
| `/health` | HTTP | Health check (returns `ok`) |

## Publishing to GHCR

To publish the image to GitHub Container Registry, set up a GitHub Actions workflow or push manually:

```bash
# log in to GHCR
echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin

# build and push
docker build -t ghcr.io/nerney/slskrr:latest -t ghcr.io/nerney/slskrr:v0.0.1 .
docker push ghcr.io/nerney/slskrr --all-tags
```

Or automate it with a GitHub Actions workflow (`.github/workflows/release.yml`):

```yaml
name: Release

on:
  push:
    tags: ["v*"]

jobs:
  docker:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v4
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: docker/metadata-action@v5
        id: meta
        with:
          images: ghcr.io/${{ github.repository }}
          tags: |
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=raw,value=latest
      - uses: docker/build-push-action@v6
        with:
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
```

This will automatically build and push `ghcr.io/nerney/slskrr:v0.0.1`, `:v0.0`, and `:latest` whenever you push a version tag.

## License

MIT
