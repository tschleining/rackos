
# RackOS Web Management Console

## Overview

The RackOS Web Management Console provides a local,
read-only interface for monitoring registered
infrastructure and inspecting stored telemetry.

The console is implemented using React, TypeScript,
Vite, Tailwind CSS, TanStack Query, React Router,
Recharts, and Lucide icons.

Production frontend assets are embedded directly into
the RackOS appliance executable. A separate Node.js
runtime or frontend server is not required in production.

## Implemented Capabilities

- Appliance health, readiness, and version information.
- Registered device overview and connectivity status.
- Device search, filtering, and sorting.
- Individual device identity and heartbeat information.
- Hardware inventory visualization.
- CPU and memory utilization.
- Mounted filesystem capacity and utilization.
- Disk activity counters and throughput.
- Network interface counters and throughput.
- Historical performance charts.
- Historical time-range and resource selection.
- Automatic data refresh.
- Loading, empty, offline, and error states.
- Cached data warnings.
- Recoverable React rendering error boundaries.
- Responsive browser navigation.

## Architecture

The production console and management API share the
same HTTP origin.

    Browser
       |
       v
    RackOS appliance HTTP server
       |
       +-- Embedded React console
       +-- Read-only management API
       +-- SQLite persistence

The frontend does not access SQLite directly.

All appliance information is retrieved through the
versioned management API.

The shared TypeScript API client handles request
validation, response validation, cancellation,
timeouts, historical pagination, and error classification.

## Repository Structure

    web/
      apps/
        console/
          src/
          test/
          e2e/
      packages/
        api-client/
      package.json
      package-lock.json

    internal/appliance/
      httpserver/
      webconsole/

    scripts/
      build-appliance.sh
      test-console-server.sh

The frontend uses npm workspaces. The API client is
independent of React and can be reused by future
RackOS interfaces.

## Development Requirements

- Go version specified in go.mod.
- Node.js 22.12 or later, compatible with the locked
  frontend dependencies.
- npm.
- Git.

Node.js 24 is used by continuous integration.

Node.js is required to build the frontend but is not
required to run an integrated appliance executable.

## Integrated Appliance Build

From the repository root:

```bash
./scripts/build-appliance.sh
```

The build script installs locked frontend dependencies,
validates TypeScript, builds the production frontend,
stages the generated assets, and compiles the Go
appliance with the webconsole build tag.

The resulting executable is:

    bin/rackosd-web

Start the integrated appliance:

```bash
./bin/rackosd-web
```

Open the default management address:

    http://127.0.0.1:8080

The console uses the appliance's configured HTTP address.

Frontend assets are compiled into the executable.
Rebuild the integrated appliance after modifying
frontend source files.

Generated frontend assets and compiled executables
are excluded from version control.

## Backend-Only Builds

The existing backend-only build remains supported:

```bash
go build -o bin/rackosd ./cmd/rackosd
```

Backend-only builds do not require Node.js or generated
frontend assets.

The management API remains available, but the embedded
browser console is not included.

The appliance reports embedded console availability
during startup.

## Frontend Development

Install the locked frontend dependencies:

```bash
npm --prefix web ci
```

Start the backend-only appliance:

```bash
./bin/rackosd
```

In a separate terminal, start Vite:

```bash
npm --prefix web run dev
```

Open:

    http://127.0.0.1:5173

Vite proxies management API requests to the local
appliance at port 8080.

For an appliance running on another HTTP port:

```bash
RACKOS_DEV_API_PORT=18080 npm --prefix web run dev
```

The development server binds to loopback and is not
intended for remote access or production deployment.

## Application Routes

| Route | Purpose |
| --- | --- |
| / | Infrastructure overview |
| /devices | Registered devices |
| /devices/:deviceId | Device identity and status |
| /devices/:deviceId/inventory | Hardware inventory |
| /devices/:deviceId/performance | Latest and historical performance |
| /system | Appliance information |

Production HTTP routing supports direct navigation
and browser refresh on application routes.

Unknown management API endpoints and missing static
assets return HTTP errors rather than frontend HTML.

The Performance page is loaded separately from the
initial application bundle.

## Administrator Authentication

The web console implements an administrator authentication interface using the RackOS authentication API.

Authentication state is managed independently of React Router and TanStack Query.

When authentication is enabled, protected application components are not mounted until the current administrator session has been verified.

The authentication boundary supports the following states:

| State | Console behavior |
| --- | --- |
| Checking | Verify the current session |
| Anonymous | Display administrator login |
| Authenticating | Prevent overlapping login submissions |
| Authenticated | Mount the protected management application |
| Signing out | Hide protected content while revoking the session |
| Unavailable | Lock management access and allow session verification to be retried |

The original application URL is preserved during authentication. Protected deep links become available after successful login without requiring an external redirect.

### Session Lifecycle

Administrator sessions have a 30-minute inactivity timeout and a 12-hour absolute lifetime.

The appliance remains authoritative over session expiration.

The browser verifies authentication when session deadlines are reached, when the browser regains focus, and when a hidden tab becomes visible.

Unauthorized protected management responses trigger session verification.

Authentication generations prevent delayed responses from modifying newer authentication states.

### Administrator Activity

Trusted pointer, click, and non-repeating keyboard events in a visible authenticated console qualify as administrator activity.

Activity renewal attempts are limited to one per 60 seconds per authentication provider instance.

Dashboard polling, automatic refresh, background timers, synthetic input events, and ordinary session inspection do not renew administrator activity.

Activity renewal does not extend the absolute session lifetime.

Logout and session verification supersede outstanding activity renewal operations.

### Cross-Tab Synchronization

Browser tabs exchange session invalidation hints using BroadcastChannel.

The channel transmits only a constant session-change notification. Credentials, authentication tokens, cookies, and session metadata are never transmitted.

Receiving tabs verify their current session with the appliance rather than trusting messages as authentication decisions.

Browsers without BroadcastChannel support retain session deadline verification and management request invalidation behavior.

### Management Query Isolation

Each authenticated generation owns an independent TanStack Query client.

When a generation ends, outstanding protected queries are cancelled and the retired query and mutation caches are cleared.

Successful activity renewal preserves the current authentication generation and management query cache.

Authentication credentials and authentication mutations remain outside TanStack Query.

### Authentication Build Configuration

Frontend authentication is temporarily controlled by a build-time setting:

```bash
VITE_RACKOS_AUTHENTICATION=true npm --prefix web run build
```

The default build remains compatible with the existing appliance daemon:

```bash
npm --prefix web run build
```

The enabled build requires HTTPS and a compatible authenticated management API.

There is no automatic runtime fallback to unauthenticated management access.

The build setting is temporary integration scaffolding and must be removed when mandatory daemon authentication enforcement is introduced.

The Vite development API proxy remains read-only and does not forward administrator credentials or authentication cookies.

See [Management Authentication](management-authentication.md) for the complete authentication API contract.

## Data Refresh

| Information | Automatic refresh |
| --- | --- |
| Appliance health | 15 seconds |
| Appliance readiness | 15 seconds |
| Device list | 15 seconds |
| Device details | 15 seconds |
| Hardware inventory | 5 minutes |
| Latest performance | 30 seconds |
| Historical performance | 60 seconds |

Periodic refresh applies while the relevant query is
active and the browser tab is in the foreground.

Appliance version information is cached and can be
refreshed manually.

The console does not change agent collection intervals.

Device connectivity status is determined by the
appliance using heartbeat information.

An offline device may still have previously collected
inventory and performance measurements available.

## Historical Performance

Supported historical time ranges are:

- Last 15 minutes.
- Last hour.
- Last 6 hours.
- Last 24 hours.

The default range is one hour.

Historical queries use bounded cursor-based pagination,
with up to four pages of 25 samples per refresh.

If additional samples exist beyond the retrieval limit,
the console identifies the displayed history as incomplete.

Charts use actual collection timestamps.

Missing measurements are represented as unavailable
rather than zero. Large gaps between samples are not
connected into continuous measurement lines.

Resource-specific charts support selection of available
filesystems, disks, and network interfaces.

Historical chart queries do not modify stored telemetry
or override appliance retention settings.

## Measurement Accuracy

Unsigned and signed 64-bit values are represented as
decimal strings in the management API.

The frontend preserves exact integer values using
BigInt where necessary.

Human-readable capacity and throughput displays use
binary units, including KiB, MiB, GiB, and TiB.

Chart values are numerical representations intended
for visualization rather than exact counter storage.

Unavailable optional measurements remain distinct
from measured zero values.

## Reliability

The console distinguishes:

- Initial loading.
- Background refresh.
- Successful empty responses.
- Paused connections.
- Failed requests.
- Retained cached information.
- Offline agents.
- Removed devices.
- Unexpected rendering failures.

Failed requests can retain previously retrieved
information with appropriate warnings.

Unexpected rendering failures are contained by React
error boundaries, allowing interface recovery or
navigation to another page.

## Security

The management console is currently local-only
and read-only.

Management HTTPS is implemented and remains optional in the current appliance configuration.

Administrator authentication endpoints, persistent sessions, and the browser authentication interface are implemented.

Live administrator authentication enforcement by the appliance daemon remains pending.

The default console must not be treated as an authenticated production management interface.

Do not expose the console through a reverse proxy,
port forwarding, or other remote access mechanism.

Implemented browser-facing protections include:

- Explicit loopback management addresses.
- Host and Origin validation.
- Fetch Metadata validation.
- Same-origin API requests.
- No cross-origin management access.
- Restricted HTTP methods.
- Content Security Policy.
- Frame embedding restrictions.
- MIME type sniffing restrictions.
- Referrer restrictions.
- Sanitized management error responses.
- Disabled static directory listing.
- Restricted embedded asset types.

Production JavaScript and stylesheet resources are
served from the appliance's origin.

Inline style attributes are permitted to support
dynamic chart positioning. Inline JavaScript is
not permitted by the production CSP.

The HTML entry point is served with no-store caching.

Content-hashed frontend assets use private immutable
caching.

These controls are not a substitute for administrator
authentication or encrypted remote management.

## Testing

Validate frontend types, unit tests, component tests,
and production compilation:

```bash
npm --prefix web run typecheck
npm --prefix web run test

npm --prefix web exec \
  --workspace=@rackos/console \
  -- vitest run

npm --prefix web run build
```

Build the integrated appliance:

```bash
./scripts/build-appliance.sh
```

Install the Playwright Chromium browser:

```bash
cd web
npm exec --workspace=@rackos/console -- \
  playwright install chromium
cd ..
```

Run browser integration tests:

```bash
npm --prefix web run test:e2e
```

Browser tests start an isolated appliance installation
using temporary storage and dedicated loopback ports.

The real appliance tests exercise management API
responses, application navigation, routing, and
management security.

The historical performance browser test supplies
controlled telemetry responses to verify production
chart rendering independently of a running agent.

The browser suite does not require enrollment or
access to the development appliance database.

### Authentication Browser Tests

Build the authentication-enabled production frontend:

```bash
VITE_RACKOS_AUTHENTICATION=true npm --prefix web run build
```

Run the isolated authentication browser suite:

```bash
npm --prefix web exec \
  --workspace=@rackos/console \
  -- playwright test \
  --config=playwright.auth.config.ts
```

The authentication browser suite intercepts requests to a reserved test origin and serves the actual production frontend assets with controlled API responses.

It validates:

- Protected deep links and administrator login.
- Authentication infrastructure failures.
- Trusted browser activity and renewal throttling.
- Unauthorized management request handling.
- Cross-tab authentication synchronization.

The tests do not contact a running appliance or require real administrator credentials.

They do not validate actual TLS transport, authentication cookie handling, password verification, or persistent session transactions.

Restore the default frontend build after testing:

```bash
npm --prefix web run build
```

## Continuous Integration

GitHub Actions validates both build configurations.

The backend-only job verifies:

- Go dependency integrity.
- Go formatting.
- Static analysis.
- Tests with race detection.
- Backend-only compilation.

The integrated console job verifies:

- Locked frontend dependency installation.
- TypeScript validation.
- Frontend unit and component tests.
- Production frontend compilation.
- Integrated appliance compilation.
- Integrated Go tests with race detection.
- Playwright Chromium browser tests.
- Authentication-enabled production frontend compilation.
- Administrator authentication component regression tests.
- Isolated authentication Chromium browser tests.
- Verified HTTPS appliance browser tests.

## Current Limitations

The management console is a pre-alpha development
interface.

The following capabilities are not yet implemented:

- Remote browser management.
- Interactive agent enrollment and approval.
- Device administration through the browser.
- Alert management.
- Configurable dashboards and widgets.
- Native integration management.
- Touchscreen panel interface.
- Mandatory administrator authentication enforcement by the appliance daemon.
- Complete authenticated browser-to-appliance integration.
- Mandatory management HTTPS.
- Operation-specific administrator authorization.

Long historical time ranges may be incomplete due
to bounded retrieval.

Automated browser validation currently targets Chromium.

## Related Documentation

- [Management API](management-api.md)
- [Device Administration](device-administration.md)
- [Hardware Inventory](hardware-inventory.md)
- [Performance Metrics](performance-metrics.md)
- [Performance Storage](performance-storage.md)
- [Historical Performance Retention](performance-retention.md)
