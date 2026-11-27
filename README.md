### RackOS

A local-first infrastructure monitoring platform for home labs.
Status: Pre-alpha / design and early development. RackOS is a proposed open-source project. The features and interfaces described below are design goals, not available releases. Installation instructions will be published once working builds exist.

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

```text
### Repository Layout

```text
cmd/
  rackosd/              # Appliance daemon
  rackos-agent/         # Host monitoring agent
  rackosctl/            # Administrative CLI

internal/
  appliance/
    agentservice/       # Agent services and inventory ingestion
    config/             # Appliance configuration
    credentials/        # Credential management
    database/           # SQLite persistence and migrations
    devices/            # Device management and repositories
    enrollment/         # Enrollment token management
    grpcserver/         # gRPC agent server
    httpserver/         # HTTP management API
    identity/           # Appliance identity
    inventory/          # Inventory validation and persistence
    pki/                # Public key infrastructure and certificate rotation

  agent/
    client/             # Appliance communication and inventory reporting
    inventory/          # Hardware and platform-specific collectors
    state/              # Persistent local agent state

api/
  proto/
    rackos/
      v1/               # Versioned protobuf definitions and gRPC bindings

docs/
  agent-protocol.md
  device-administration.md
  hardware-inventory.md
  inventory-storage.md
  management-api.md

.github/
  workflows/
    ci.yml              # Continuous integration

.gitignore
go.mod                  # Go module definition
go.sum                  # Go dependency checksums
LICENSE
README.md
```

Tests are maintained alongside their respective implementation files.
Additional directories will be introduced incrementally as their
corresponding components are developed.


Directories may be introduced incrementally rather than as empty placeholders. Go, TypeScript, protobuf and firmware dependencies will be pinned when those components are bootstrapped.

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
- [ ] Display connected agents and basic host metrics in the web console
- [ ] Create the touch-optimized 1U panel interface
- [ ] Add agentless health checks and the first Proxmox connector
- [ ] Package Linux and Windows agents
- [ ] Produce reproducible firmware and test the production update flow
- [ ] Publish documented plugin contracts and example integrations
Building and running
There are no supported install or build instructions yet. Once the first vertical slice is implemented, this section will include verified prerequisites, development commands, and a minimal appliance-plus-agent test environment.
Security
RackOS is intended to monitor sensitive home-lab infrastructure. The initial security model is:
- Read-only metrics collection by default; no general-purpose remote shell.
- Explicit approval before agents acquire credentials.
- Unique cryptographic identities for agents and mutual TLS for agent connections.
- Protected local credential storage, with rotation and revocation.
- No trust based solely on IP address, hostname or discovery announcements.
- Permission-scoped integrations and isolation for untrusted extensions.
- Auditable enrollment and administrative actions.
Please do not publish vulnerability details or secrets in public issues. A private vulnerability-reporting process will be documented before an initial public release.
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