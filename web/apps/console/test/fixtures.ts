
import type { Device } from "@rackos/api-client";

export const devices: Device[] = [
  {
    id: "11111111-1111-4111-8111-111111111111",
    hostname: "node-10",
    os: "linux",
    architecture: "amd64",
    agent_version: "0.1.0-dev",
    status: "offline",
    created_at: "2026-11-27T10:00:00Z",
    updated_at: "2026-11-27T10:10:00Z",
    last_seen_at: "2026-11-27T10:10:00Z",
  },
  {
    id: "22222222-2222-4222-8222-222222222222",
    hostname: "node-2",
    os: "linux",
    architecture: "amd64",
    agent_version: "0.1.0-dev",
    status: "online",
    created_at: "2026-11-27T10:00:00Z",
    updated_at: "2026-11-27T10:30:00Z",
    last_seen_at: "2026-11-27T10:30:00Z",
  },
  {
    id: "33333333-3333-4333-8333-333333333333",
    hostname: "workstation",
    os: "darwin",
    architecture: "arm64",
    agent_version: "0.1.0-dev",
    status: "online",
    created_at: "2026-11-27T10:00:00Z",
    updated_at: "2026-11-27T10:20:00Z",
    last_seen_at: "2026-11-27T10:20:00Z",
  },
];
