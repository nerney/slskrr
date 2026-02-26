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

## Pre-built binaries

Pre-built binaries are available in the [`dist/`](dist/) directory:

| File | Platform |
|------|----------|
| `dist/slskrr-linux-amd64` | Linux x86_64 |
| `dist/slskrr-darwin-arm64` | macOS Apple Silicon |
| `dist/slskrr-windows-amd64.exe` | Windows x86_64 |

Download the binary for your platform and make it executable:

```bash
chmod +x slskrr-linux-amd64
```

## Build from source

Requires Go 1.24+:

```bash
go build -o slskrr .
```

Cross-compile for other platforms:

```bash
GOOS=linux   GOARCH=amd64 go build -o dist/slskrr-linux-amd64 .
GOOS=darwin  GOARCH=arm64 go build -o dist/slskrr-darwin-arm64 .
GOOS=windows GOARCH=amd64 go build -o dist/slskrr-windows-amd64.exe .
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

## License

MIT
