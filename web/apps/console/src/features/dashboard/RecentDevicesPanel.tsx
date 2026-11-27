
import {
  ArrowRight,
  Server,
} from "lucide-react";

import { Link } from "react-router";

import type {
  Device,
} from "@rackos/api-client";

import {
  DeviceStatusBadge,
} from "../../components/DeviceStatusBadge";

interface RecentDevicesPanelProps {
  devices: readonly Device[] | undefined;
}

const MAX_RECENT_DEVICES = 5;

const dateFormatter = new Intl.DateTimeFormat(
  undefined,
  {
    dateStyle: "medium",
    timeStyle: "medium",
  },
);

function formatTimestamp(value: string): string {
  const timestamp = new Date(value);

  if (!Number.isFinite(timestamp.getTime())) {
    return "Unavailable";
  }

  return dateFormatter.format(timestamp);
}

function sortRecentDevices(
  devices: readonly Device[],
): Device[] {
  return [...devices]
    .sort((first, second) => {
      const difference =
        Date.parse(second.last_seen_at) -
        Date.parse(first.last_seen_at);

      if (difference !== 0) {
        return difference;
      }

      return first.id.localeCompare(second.id);
    })
    .slice(0, MAX_RECENT_DEVICES);
}

export function RecentDevicesPanel({
  devices,
}: RecentDevicesPanelProps) {
  const recentDevices = devices
    ? sortRecentDevices(devices)
    : undefined;

  return (
    <section className="min-w-0 rounded-xl border border-rack-border bg-rack-surface p-5 sm:p-6">
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-medium">
          Recent device heartbeats
        </h2>

        <Link
          to="/devices"
          className="inline-flex items-center gap-2 text-sm font-medium text-rack-accent hover:underline"
        >
          View all

          <ArrowRight
            size={16}
            aria-hidden="true"
          />
        </Link>
      </div>

      {recentDevices === undefined ? (
        <div className="py-10 text-center">
          <Server
            size={32}
            className="mx-auto mb-4 text-rack-muted"
            aria-hidden="true"
          />

          <p className="text-sm text-rack-muted">
            Device information is not available yet.
          </p>
        </div>
      ) : recentDevices.length === 0 ? (
        <div className="py-10 text-center">
          <Server
            size={32}
            className="mx-auto mb-4 text-rack-muted"
            aria-hidden="true"
          />

          <p className="text-sm font-medium">
            No registered devices
          </p>

          <p className="mt-2 text-sm text-rack-muted">
            Enroll a RackOS agent to begin monitoring
            your infrastructure.
          </p>
        </div>
      ) : (
        <ul className="divide-y divide-rack-border">
          {recentDevices.map((device) => (
            <li key={device.id}>
              <Link
                to={`/devices/${encodeURIComponent(device.id)}`}
                className="flex flex-wrap items-center gap-3 rounded-md px-1 py-4 transition-colors hover:bg-rack-background motion-reduce:transition-none sm:flex-nowrap"
              >
                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-rack-background text-rack-muted">
                  <Server
                    size={20}
                    aria-hidden="true"
                  />
                </div>

                <div className="min-w-0 flex-1">
                  <p className="break-all text-sm font-medium">
                    {device.hostname}
                  </p>

                  <p className="mt-1 text-xs text-rack-muted">
                    {device.os} · {device.architecture}
                  </p>

                  <p className="mt-1 text-xs text-rack-muted">
                    Last seen:{" "}

                    <time
                      dateTime={device.last_seen_at}
                      title={device.last_seen_at}
                    >
                      {formatTimestamp(
                        device.last_seen_at,
                      )}
                    </time>
                  </p>
                </div>

                <DeviceStatusBadge
                  status={device.status}
                />
              </Link>
            </li>
          ))}
        </ul>
      )}

      <p className="mt-4 border-t border-rack-border pt-4 text-xs text-rack-muted">
        Up to five devices, ordered by their most
        recent recorded heartbeat. Times are shown
        in your browser's local timezone.
      </p>
    </section>
  );
}
