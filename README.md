# Contest Monitor

Contest Monitor is a consent-based, administrator-controlled monitoring service for competitive-programming events. It correlates an uploaded roster of participant IP addresses with limited network metadata observed by the authorized contest host. The service records DNS query names, TLS Server Name Indication values, and plain-HTTP host metadata; it does not decrypt TLS, capture raw packet payloads, bypass Wi-Fi isolation, or monitor devices outside an explicitly authorized contest.

A matching domain is an **investigative signal**, not conclusive proof that a participant used AI. Organizers must apply their contest rules, document the monitoring notice and consent process, preserve due process, and avoid making automated disciplinary decisions from this tool alone.

## Architecture

The project contains a Go/Gin API, a React/Vite single-page application, Firebase Realtime Database storage accessed only through the backend service account, and an optional libpcap/gopacket metadata capture worker. Docker Compose serves the frontend on port `3000` and runs the backend with host networking so the authorized host can observe the host-visible network interface.

| Component | Responsibility | Default exposure |
|---|---|---|
| React/Vite frontend | Authentication UI, contest setup, dashboard, live administrator monitor, LAN sharing instructions | `http://HOST-LAN-IP:3000` |
| Go/Gin backend | JWT authentication, RBAC, roster validation, contest lifecycle, metadata correlation | `127.0.0.1:8081` inside the host network namespace |
| Firebase RTDB | Server-side persistence for users, contests, rosters, and metadata signals | No direct browser access; rules deny client reads and writes |
| libpcap/gopacket | Optional DNS/TLS/HTTP metadata observation on the authorized host | Disabled unless `CAPTURE_ENABLED=true` |

## Safety and privacy controls

Monitoring is disabled by default. A contest cannot be created unless the backend has `CAPTURE_ENABLED=true` and the administrator submits the explicit participant-consent acknowledgement. The recorder stores only an IP address, a normalized service domain, and timestamps. It does not persist packet payloads. Telemetry and roster endpoints require an administrator JWT; ordinary authenticated users can view the contest list but cannot read participant telemetry.

The detector intentionally avoids broad substring matching. It recognizes known AI-service domains as exact hostnames or subdomains and does not classify general reference sites such as Stack Overflow as AI services. Network administrators should still expect false positives, missed detections, encrypted DNS limitations, VPN effects, shared or changing IP addresses, and router client-isolation behavior.

## Quick start with Docker Compose

The following commands assume Docker and Docker Compose are installed on the authorized contest host. Place the Firebase service-account JSON file at `secrets/firebase_credentials.json`; do not commit it or put it in the frontend build context.

```bash
cp .env.example .env
mkdir -p secrets
cp /secure/location/firebase_credentials.json secrets/firebase_credentials.json
openssl rand -base64 48
# Put the generated value into JWT_SECRET in .env.
# Set FIREBASE_DATABASE_URL and CAPTURE_ENABLED=true only on the authorized host.
docker compose up --build -d
```

Before the first contest, deploy `firebase.database.rules.json` to the Realtime Database so browser clients cannot access the database directly. The backend uses the service account, not client-side Firebase credentials. Rotate any service-account key that was ever included in an archive or repository.

After the containers start, open `http://HOST-LAN-IP:3000` on the host and on authorized devices connected to the same Wi-Fi. The host machine must remain powered on, the firewall must allow TCP port `3000` from the contest LAN, and the access point must permit client-to-client communication. The application cannot override AP isolation, captive portals, VPN routing, or operating-system packet-capture permissions.

The frontend is also configured for LAN development without Docker:

```bash
cd frontend
npm ci
npm run dev -- --host 0.0.0.0
```

The Vite proxy forwards `/api` to the Go backend at `127.0.0.1:8081`. Run the backend separately with the environment variables described below.

## Configuration

| Variable | Required | Description |
|---|---:|---|
| `FIREBASE_DATABASE_URL` | Yes | Firebase Realtime Database URL |
| `FIREBASE_CREDENTIALS_FILE` | Yes | Mounted service-account JSON path; Compose uses `/run/secrets/firebase_credentials` |
| `JWT_SECRET` | Yes | Random secret with at least 32 characters |
| `CAPTURE_ENABLED` | Yes for monitoring | Must be `true` on the authorized capture host; defaults to `false` |
| `SNIFF_IFACE` | No | Comma-separated interface names; empty selects usable non-loopback interfaces |
| `ALLOWED_ORIGINS` | No | Comma-separated development origins; same-origin Compose traffic does not require CORS |
| `PORT` | No | Backend port, default `8081` |
| `VITE_API_URL` | No | Browser API base path; keep `/api` behind nginx or the Vite proxy |

The backend requires a service-account credential file or an equivalent `FIREBASE_CREDENTIALS_JSON` environment value. A mounted file is preferred because it avoids putting credentials into process arguments or browser-visible assets.

## Roster CSV format

The first row must begin with `team_name,ip`. Additional columns are treated as member names. Each participant IP must be a valid IPv4 or IPv6 address and may appear only once in a contest.

```csv
team_name,ip,member1,member2
Team Alpha,192.168.1.22,Ada Lovelace,Grace Hopper
Team Beta,192.168.1.23,Alan Turing,Katherine Johnson
```

Uploads are limited to 5 MiB. Names and member data are length-limited and invalid rows are rejected before anything is written to Firebase.

## API surface

| Method | Path | Access | Purpose |
|---|---|---|---|
| `POST` | `/register` | Public, rate-limited | Create a user account |
| `POST` | `/login` | Public, rate-limited | Issue a 12-hour JWT |
| `GET` | `/health` | Public | Report database reachability and capture enablement without user data |
| `GET` | `/contests` | Authenticated | List contests without telemetry details |
| `POST` | `/host-contest` | Administrator | Create a consented contest and start capture |
| `DELETE` | `/contests/:id` | Administrator | Stop capture and delete contest data |
| `GET` | `/contests/:id/monitor` | Administrator | Read roster status |
| `GET` | `/contests/:id/violations` | Administrator | Read flagged teams and latest signals |
| `GET` | `/contests/:id/ai-hits` | Administrator | Read metadata signal history |

## Verification

Run the following checks from the repository root after changing the code:

```bash
cd backend
gofmt -w *.go
go test ./...
go vet ./...
cd ../frontend
npm ci
npm run lint
npm run build
```

The repository intentionally excludes environment files, service-account credentials, dependency directories, build outputs, and local binaries. Use the example configuration files as templates and supply secrets through the deployment environment.
