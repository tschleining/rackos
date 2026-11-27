### RackOS

A local-first infrastructure monitoring platform for home labs.

**Status:** Pre-alpha / active development.

RackOS currently provides a working Go-based appliance,
authenticated agent enrollment, heartbeat monitoring,
hardware inventory persistence, native performance
collection, historical performance reporting, configurable
automatic retention, read-only storage administration,
and an embedded browser-based management console.

The management console supports registered device
monitoring, hardware inventory visualization, latest
performance measurements, and historical performance
charts without requiring a separate frontend server
at runtime.

The project remains under active development and is
not yet a supported production release. Planned
hardware, management interfaces, integrations, and
other roadmap features are subject to change.


RackOS is being designed for home lab enthusiasts who want an independent, purpose built monitoring appliance rather than another dashboard hosted on the systems they're trying to monitor. The intended flagship hardware, RackOS One, is a 1U touchscreen appliance for 10-inch mini racks. RackOS software is planned to remain freely available, with hardware sales supporting development.


### Project goals
- Independent monitoring: Core health checks run on the appliance, even when monitored hypervisors or services fail. The appliance itself still requires power and network connectivity to perform remote checks.
- Local first operation: No mandatory cloud account or cloud telemetry service.
- Secure agents: Lightweight Linux and Windows agents initiate outbound connections to RackOS and enroll using verified appliance identity, explicit approval, and per agent credentials.
- Agentless discovery: Discover candidate devices using network checks and supported platform APIs; discovery alone never establishes trust or grants access.
- Extensibility: Connectors, widgets, and (eventually) explicitly authorized actions with versioned plugin contracts.
- Great rack experience: A readable, touch-friendly 1U interface and a full browser based management console.
- Open development: Public source, documented protocols, and community contributions.

### Planned capabilities

Area	Initial direction
Appliance	Embedded Linux, local health checks, telemetry ingestion, alerts, inventory, SQLite storage
Agents	Go binaries for Linux and Windows; CPU, memory, disk, network and uptime telemetry
Enrollment	One-time invitation, verified server trust anchor, admin approval, mutual TLS, certificate renewal and revocation
Discovery	mDNS/DNS-SD for locating RackOS; candidate hosts from network checks, SNMP and selected native integrations
Integrations	Proxmox first; other candidates include TrueNAS, UniFi, and Uptime Kuma
Interfaces	1U touch panel, browser-based management console, documented management API
Plugins	Connectors and widgets initially; advanced collectors and automations as the contracts mature
Firmware	Reproducible embedded Linux builds, signed updates, and a rollback-capable update process


Additional agentless checks and native integrations from RackOS
Agents are intended to send telemetry, not accept unsolicited inbound connections. Remote command execution is out of scope for the initial agent.


### Repository Layout

```text
cmd/
  rackosd/              # Appliance daemon
  rackos-agent/         # Host monitoring agent
  rackosctl/            # Administrative CLI

internal/
  appliance/
    agentservice/       # Authenticated agent RPC services
    config/             # Appliance configuration
    credentials/        # Credential management
    database/           # SQLite persistence, migrations and storage inspection
    devices/            # Device management and repositories
    enrollment/         # Enrollment token management
    grpcserver/         # gRPC agent server
    httpserver/         # Management API and frontend HTTP integration
    identity/           # Appliance identity
    inventory/          # Hardware inventory validation and persistence
    performance/        # Historical performance storage and queries
    pki/                # PKI and certificate rotation
    retention/          # Retention policy and background cleanup
    webconsole/         # Embedded frontend assets and HTTP serving

  agent/
    client/             # Appliance communication and monitoring lifecycle
    inventory/          # Hardware and platform-specific collectors
    metrics/            # Native CPU, memory, storage and network collectors
    state/              # Persistent local agent state

api/
  proto/
    rackos/
      v1/               # Versioned protobuf definitions and gRPC bindings

web/
  apps/
    console/
      src/
        app/            # Application services and query configuration
        components/     # Shared React components
        features/       # Dashboard, devices, inventory and performance
        hooks/          # Management API query hooks
        layouts/        # Application and device layouts
        lib/            # Formatting and API error utilities
        pages/          # Application routes and page components
        styles/         # Application styling
      test/             # Frontend unit and component tests
      e2e/              # Playwright browser tests
      index.html
      package.json
      playwright.config.ts
      tsconfig.json
      vite.config.ts
      vitest.config.ts

  packages/
    api-client/         # Shared TypeScript management API client

  package.json          # Frontend workspace configuration
  package-lock.json     # Locked frontend dependencies

scripts/
  build-appliance.sh    # Integrated appliance build
  test-console-server.sh # Isolated browser-test appliance

docs/
  agent-protocol.md
  device-administration.md
  hardware-inventory.md
  inventory-storage.md
  management-api.md
  performance-metrics.md
  performance-retention.md
  performance-storage.md
  web-console.md

.github/
  workflows/
    ci.yml              # Backend and frontend continuous integration

.gitignore
go.mod                  # Go module definition
go.sum                  # Go dependency checksums
LICENSE
README.md
```

Go tests are maintained alongside their respective
implementation files. Frontend unit, component, and
browser integration tests are maintained within the
frontend workspaces.

Generated assets, compiled executables, runtime
databases, and test reports are excluded from
version control.

Additional directories will be introduced incrementally
as their corresponding components are developed.


### Planned agent onboarding
1. Install a signed RackOS Agent package on a Linux or Windows system.
2. Find an appliance over the local network or supply its hostname/IP address.
3. Verify the appliance's identity using a trust fingerprint obtained through a trusted setup channel; mDNS advertisements are not trusted identities.
4. Submit a short-lived, one-time enrollment invitation and a locally generated certificate-signing request.
5. Approve the pending agent in the management console or on the device.
6. Receive an agent certificate and communicate over mutually authenticated TLS.
7. Continue reporting telemetry, reconnecting and renewing credentials automatically when authorized.
Installers must not put enrollment codes in command-line arguments, shell history or logs. Discovery only creates candidates; it never grants monitoring credentials.
Development roadmap
- [ ] Bootstrap Go appliance daemon and SQLite persistence
- [ ] Implement the versioned agent protocol and local agent collectors
- [ ] Build secure enrollment, approval, renewal and revocation
- [x] Display connected agents and basic host metrics in the web console
- [ ] Create the touch-optimized 1U panel interface
- [ ] Add agentless health checks and the first Proxmox connector
- [ ] Package Linux and Windows agents
- [ ] Produce reproducible firmware and test the production update flow
- [ ] Publish documented plugin contracts and example integrations

### Building and Running

RackOS is currently distributed as development source
code rather than a supported installation package.

#### Development Requirements

- Go version specified in go.mod.
- Git.
- macOS or Linux for native agent development.
- Node.js 22.12 or later, compatible with locked
  frontend dependencies, for web console development.
- npm for frontend dependency management.
- Protocol Buffers compiler and Go generators when
  modifying protocol definitions.

Continuous integration uses Node.js 24.

Node.js is not required for backend-only builds or
for running an integrated appliance executable.

#### Build

```bash
mkdir -p bin

go build -o bin/rackosd ./cmd/rackosd/
go build -o bin/rackos-agent ./cmd/rackos-agent/
go build -o bin/rackosctl ./cmd/rackosctl/
```

#### Run the Appliance

```bash
./bin/rackosd
```

The default appliance services bind to loopback
addresses.

The development management API should not be exposed
to untrusted networks.


#### Build and Run the Integrated Web Console

From the repository root:

```bash
./scripts/build-appliance.sh
./bin/rackosd-web
```

Open the management console in a browser:

    http://127.0.0.1:8080

The integrated executable serves both the React
management console and the read-only management API.

No separate frontend server or Node.js runtime is
required when running the integrated executable.

The management console provides:

- Appliance health and readiness information.
- Registered device monitoring.
- Device search, filtering, and sorting.
- Hardware inventory visualization.
- Latest CPU, memory, storage, and network measurements.
- Historical performance charts.
- Automatic refresh and cached data warnings.

The management interface remains local-only
and read-only.

#### Frontend Development

With the backend appliance running:

```bash
npm --prefix web ci
npm --prefix web run dev
```

Open:

    http://127.0.0.1:5173

Vite proxies management API requests to the
local appliance.

See [Web Management Console](docs/web-console.md)
for architecture, build instructions, security
requirements, and testing documentation.


#### Collect Local Performance Metrics

```bash
./bin/rackos-agent metrics
```

Local metrics collection does not require enrollment
or a running appliance.

#### Management API

The appliance provides a read-only HTTP management
API on its configured loopback address.

The default base URL is:

    http://127.0.0.1:8080

Retrieve registered devices:

```bash
curl -fsS http://127.0.0.1:8080/api/v1/devices
```

Retrieve the latest performance snapshot:

```bash
curl -fsS \
  "http://127.0.0.1:8080/api/v1/devices/$DEVICE_ID/performance/latest"
```

Retrieve historical performance measurements:

```bash
curl -fsS \
  "http://127.0.0.1:8080/api/v1/devices/$DEVICE_ID/performance/history?limit=10"
```

Set `DEVICE_ID` to an actual registered device ID.

Historical retrieval supports time-range filtering
and cursor-based pagination.

Performance responses preserve 64-bit counter
precision and optional measurement availability.

The Management API does not currently implement
administrator authentication or HTTPS.

Do not expose it through a reverse proxy, port
forwarding, or other remote access mechanism.

See [Management API](docs/management-api.md) for
the complete endpoint and response contract.


#### Historical Performance Retention

Automatic historical performance retention is available
but disabled by default.

Enable a 30-day retention policy:

```bash
RACKOS_PERFORMANCE_RETENTION=720h \
RACKOS_PERFORMANCE_CLEANUP_INTERVAL=15m \
RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE=500 \
./bin/rackosd
```

Configuration:

| Environment Variable | Default |
| --- | --- |
| RACKOS_PERFORMANCE_RETENTION | 0s (disabled) |
| RACKOS_PERFORMANCE_CLEANUP_INTERVAL | 15m |
| RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE | 500 |

Retention durations support 24 hours through 365 days.

Expiration is based on appliance receipt timestamps.

Enabling retention may immediately remove existing
historical measurements that exceed the configured
retention duration.

Back up important historical data before enabling or
reducing retention.

Automatic cleanup does not remove device registrations,
agent credentials, or hardware inventory.

See [Historical Performance Retention](docs/performance-retention.md)
for the complete configuration and expiration contract.

#### Storage Administration

Inspect historical performance storage and SQLite
database utilization:

```bash
./bin/rackosctl storage stats --data-dir ./data
```

The storage report includes historical sample counts,
payload sizes, timestamp ranges, SQLite file sizes,
and reusable page capacity.

The command opens an existing appliance database
in read-only mode without applying migrations.

Automatic retention reduces logical historical storage
but does not necessarily shrink SQLite database files.

See [Historical Performance Retention](docs/performance-retention.md)
for storage report interpretation.


#### Validation

```bash
go mod verify
go vet ./...
go test -race -count=1 ./...
go build ./...
```

See the documentation directory for agent enrollment,
device administration, protocol details, and
historical performance storage.


### Security

RackOS is intended to monitor sensitive home-lab
infrastructure.

The currently implemented security controls include:

- Read-only agent metrics collection.
- No general-purpose remote shell.
- Short-lived, one-time enrollment invitations.
- Verified appliance identity and TLS 1.3 transport.
- Individual agent bearer credentials.
- Protected local credential storage.
- Appliance certificate renewal.
- Agent credential revocation.
- Authenticated and validated telemetry ingestion.
- Transactional performance persistence.
- Loopback-only read-only HTTP management access.
- No trust based solely on IP addresses, hostnames, or discovery announcements.
- Browser Host and Origin validation.
- Fetch Metadata request validation.
- Same-origin management API communication.
- Restricted production Content Security Policy.
- Browser frame embedding restrictions.
- Restricted static asset serving.
- Sanitized frontend request error presentation.

The Management API currently operates without
administrator authentication or HTTPS.

Remote management access must remain disabled.

Planned security capabilities include:

- Administrator authentication.
- Management HTTPS.
- Explicit interactive agent approval.
- Mutually authenticated agent TLS.
- Automatic agent credential renewal.
- Permission-scoped integrations.
- Isolation for untrusted extensions.
- Auditable administrative actions.

Please do not publish vulnerability details or
secrets in public issues.

A private vulnerability-reporting process will be
documented before an initial public release.

Contributing
Contributions, design discussions, integration proposals and documentation improvements are welcome as the project takes shape. Until contributor guidance is published, please open a discussion or issue before starting substantial changes. Compatibility and security contracts should be reviewed before external plugin interfaces are stabilized.
Open source and commercial hardware
The intended model is open-source software funded by finished RackOS hardware. The software may be run on compatible third-party hardware; the goal of RackOS One is a polished, tested, easily maintained appliance. Hardware designs, product branding, and trademarks may have separate terms and are not automatically licensed by the software's Apache-2.0 license.
License
RackOS-authored software in this repository is intended to be licensed under the Apache License, Version 2.0, unless a component explicitly indicates otherwise. Third-party dependencies and embedded firmware components retain their own licenses and attribution obligations.

The RackOS name is a working name and requires a trademark/name clearance review before commercialization.

### Implemented Features
- Native mounted filesystem discovery on Linux and macOS.
- Filesystem capacity, utilization, and reserved space monitoring.
- Cumulative disk read and write activity monitoring.
- Disk throughput and operations-per-second measurements.
- Unified CPU, memory, and storage performance snapshots.
- CGO-free native macOS storage collection through IOKit.
- Disk counter regression and device change handling.
- Native network interface monitoring on Linux and macOS.
- Physical, virtual, bridge, VPN, and loopback interface support.
- Network interface metadata and optional link speeds.
- Cumulative network byte, packet, error, and drop counters.
- Network throughput and packets-per-second measurements.
- Network interface identity and counter regression handling.
- Unified CPU, memory, storage, and network performance snapshots.
- Versioned Protocol Buffers performance reporting.
- Authenticated performance reporting over verified TLS.
- Automatic periodic performance collection.
- Transactional SQLite historical performance storage.
- Cryptographically random performance sample identities.
- Idempotent performance reporting and retry handling.
- Original receipt timestamp preservation.
- Out-of-order historical performance ingestion.
- Bounded exponential reporting retry policies.
- Pending performance report expiration.
- Concurrent heartbeat, inventory, and performance workers.
- Coordinated monitoring worker shutdown.
- Historical persistence across appliance restarts.
- Automatic agent reconnection without reenrollment.
- Credential revocation enforcement for performance reporting.
- Cascading historical record deletion during device removal.
- Native performance reporting end-to-end tests.
- Latest collected performance snapshot retrieval.
- Device-specific historical performance queries.
- Inclusive and exclusive historical time bounds.
- Deterministic historical sample ordering.
- Indexed SQLite historical queries.
- Bounded historical page sizes.
- Keyset-based historical pagination.
- Versioned Base64URL continuation cursors.
- Device-bound and time-range-bound cursors.
- Historical cursor persistence across appliance restarts.
- Strict HTTP performance query validation.
- Precision-preserving protobuf JSON serialization.
- Optional measurement availability preservation.
- Empty historical response array guarantees.
- Stored historical payload integrity validation.
- Read-only performance management HTTP endpoints.
- Sanitized management API error responses.
- SQLite-backed performance API integration tests.
- Configurable receipt-based historical performance retention.
- Opt-in retention with non-destructive upgrade defaults.
- Strict retention policy and configuration validation.
- Indexed and bounded historical sample deletion.
- Independent transactional cleanup batches.
- Background retention scheduling and startup cleanup.
- Context-aware cleanup cancellation and batch deadlines.
- Automatic recovery from temporary cleanup failures.
- Structured retention execution logging.
- Coordinated retention worker and appliance shutdown.
- Historical cursor compatibility after sample expiration.
- Preservation of unrelated appliance database records.
- Logical historical performance storage statistics.
- SQLite database, WAL, and SHM file inspection.
- SQLite reusable page capacity reporting.
- Read-only database access without automatic migrations.
- Administrative storage statistics CLI.
- Retention repository and worker regression tests.
- SQLite-backed retention integration tests.
- Appliance retention lifecycle integration tests.
- Read-only hardware inventory management HTTP endpoint.
- Shared TypeScript management API models.
- Validated TypeScript management API client.
- Request cancellation and timeout handling.
- React browser-based management console.
- Responsive application navigation and layouts.
- Appliance health and readiness visualization.
- Registered device dashboard and connectivity counts.
- Device search, filtering, and sorting.
- Individual device detail views.
- Hardware inventory visualization.
- Latest CPU and memory utilization visualization.
- Filesystem capacity and utilization visualization.
- Disk and network counter and throughput visualization.
- Historical CPU and memory performance charts.
- Resource-specific historical performance charts.
- Historical time-range selection.
- Bounded historical sample retrieval.
- Missing measurement and historical gap preservation.
- Precision-preserving BigInt measurement formatting.
- Automatic management data refresh.
- Loading, empty, error, and offline interface states.
- Cached data warning presentation.
- Recoverable React error boundaries.
- Embedded production frontend assets.
- Single-executable appliance and management console.
- Backend-only build compatibility.
- Browser application deep-link support.
- Production security headers.
- Content-hashed static asset caching.
- Lazy-loaded performance visualization components.
- Reproducible integrated appliance build script.
- Frontend unit and component tests.
- Isolated appliance browser integration tests.
- Production historical chart rendering tests.
- Backend-only and integrated appliance CI configuration.