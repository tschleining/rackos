
# Device Administration

RackOS provides local administrative commands through
`rackosctl` for managing registered devices and their
associated agent credentials.

## Commands

| Command | Description |
| --- | --- |
| `devices list` | List registered devices |
| `devices show --id ID` | Retrieve device information |
| `devices revoke --id ID` | Revoke an agent credential |
| `devices remove --id ID --confirm` | Permanently remove a device |

## Data Directory

Administrative commands operate on an existing appliance
database.

The default data directory is `./data`.

The directory can be overridden using the `RACKOS_DATA_DIR`
environment variable or the `--data-dir` command-line flag.

Commands reject missing appliance databases rather than
initializing a new installation.

## List Devices

```bash
rackosctl devices list
```

Returns a JSON array of registered devices, including their
identities, platform information, agent versions, timestamps,
and current online or offline status.

An empty device list returns an empty JSON array.

## Retrieve Device Information

```bash
rackosctl devices show --id DEVICE_ID
```

Returns a JSON representation of the specified device.

## Revoke Agent Credentials

Stop the appliance before performing this operation.

```bash
rackosctl devices revoke --id DEVICE_ID
```

Revocation invalidates the device's existing agent credential.

The device registration and historical timestamps are
preserved.

Subsequent authenticated agent requests are rejected.

The agent terminates when authentication is rejected and
requires administrator intervention.

The device becomes offline once its last successful heartbeat
is at least 90 seconds old.

Credential revocation cannot currently be reversed.

## Remove a Device

Stop the appliance before performing this operation.

```bash
rackosctl devices remove --id DEVICE_ID --confirm
```

Device removal permanently deletes the registration and
its associated agent credential.

The `--confirm` flag is required to prevent accidental
removal.

SQLite foreign key cascading ensures that associated
credentials are deleted with the device registration.

Removal cannot be undone.

## Re-enrollment

An agent whose credential has been revoked or whose
registration has been removed requires fresh enrollment
to reconnect.

A new enrollment token and a new agent state directory
are required.

Re-enrollment creates a new device identity.

Existing revoked registrations may be removed separately.

## Security

- Administrative write operations are available through
  the local CLI only.
- Administrative access relies on operating system
  filesystem permissions.
- Credential secrets and hashes are excluded from device
  information output.
- Device removal requires explicit confirmation.
- Database operations use parameterized SQL.
- Administrative commands have bounded database operation
  deadlines.
- The management REST API remains read-only.

## Current Limitations

- No administrator accounts or role-based access control.
- No administrative audit log.
- No credential restoration or rotation command.
- No bulk device administration.
- No remote administrative write operations.
- Appliance shutdown before destructive operations is
  recommended but not automatically enforced.
- Revocation does not immediately override heartbeat-based
  device status.
