import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  ChevronRight,
  Server,
} from "lucide-react";

import { Link } from "react-router";

import type {
  Device,
} from "@rackos/api-client";

import {
  DeviceStatusBadge,
} from "../../components/DeviceStatusBadge";

import {
  formatOperatingSystem,
} from "./deviceList";

import type {
  DeviceSort,
  DeviceSortField,
} from "./deviceList";

interface DeviceTableProps {
  devices: readonly Device[];
  sort: DeviceSort;

  onSort: (
    field: DeviceSortField,
  ) => void;
}

interface SortableColumnProps {
  label: string;
  field: DeviceSortField;
  sort: DeviceSort;

  onSort: (
    field: DeviceSortField,
  ) => void;
}

const dateFormatter =
  new Intl.DateTimeFormat(
    undefined,
    {
      dateStyle: "medium",
      timeStyle: "medium",
    },
  );

function formatTimestamp(
  value: string,
): string {
  const date = new Date(value);

  if (!Number.isFinite(date.getTime())) {
    return "Unavailable";
  }

  return dateFormatter.format(date);
}

function SortableColumn({
  label,
  field,
  sort,
  onSort,
}: SortableColumnProps) {
  const active = sort.field === field;

  const Icon = !active
    ? ArrowUpDown
    : sort.direction === "asc"
      ? ArrowUp
      : ArrowDown;

  const sortLabel = active
    ? sort.direction === "asc"
      ? "ascending"
      : "descending"
    : undefined;

  return (
    <th
      scope="col"
      aria-sort={sortLabel}
      className="px-4 py-3 text-left"
    >
      <button
        type="button"
        onClick={() => onSort(field)}
        className={[
          "inline-flex items-center gap-2 rounded-sm",
          "whitespace-nowrap text-xs font-semibold",
          active
            ? "text-rack-accent"
            : "text-rack-muted hover:text-rack-primary",
        ].join(" ")}
      >
        {label}

        <Icon
          size={14}
          aria-hidden="true"
        />
      </button>
    </th>
  );
}

export function DeviceTable({
  devices,
  sort,
  onSort,
}: DeviceTableProps) {
  return (
    <div
      role="region"
      aria-label="Registered device table"
      tabIndex={0}
      className="overflow-x-auto"
    >
      <table className="w-full min-w-[850px] text-sm">
        <caption className="sr-only">
          Registered RackOS devices. Select a column
          heading to change the sort order.
        </caption>

        <thead className="border-y border-rack-border bg-rack-background/50">
          <tr>
            <SortableColumn
              label="Hostname"
              field="hostname"
              sort={sort}
              onSort={onSort}
            />

            <SortableColumn
              label="Platform"
              field="platform"
              sort={sort}
              onSort={onSort}
            />

            <SortableColumn
              label="Status"
              field="status"
              sort={sort}
              onSort={onSort}
            />

            <SortableColumn
              label="Agent version"
              field="agent_version"
              sort={sort}
              onSort={onSort}
            />

            <SortableColumn
              label="Last seen"
              field="last_seen_at"
              sort={sort}
              onSort={onSort}
            />

            <th
              scope="col"
              className="px-4 py-3"
            >
              <span className="sr-only">
                Device details
              </span>
            </th>
          </tr>
        </thead>

        <tbody className="divide-y divide-rack-border">
          {devices.map((device) => (
            <tr
              key={device.id}
              className="transition-colors hover:bg-rack-background/50 motion-reduce:transition-none"
            >
              <td className="px-4 py-4">
                <div className="flex min-w-44 items-center gap-3">
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-rack-background text-rack-muted">
                    <Server
                      size={18}
                      aria-hidden="true"
                    />
                  </div>

                  <Link
                    to={`/devices/${encodeURIComponent(device.id)}`}
                    title={device.hostname}
                    className="max-w-72 rounded-sm break-all font-medium text-rack-accent hover:underline"
                  >
                    {device.hostname}
                  </Link>
                </div>
              </td>

              <td className="px-4 py-4">
                <p className="break-all font-medium">
                  {formatOperatingSystem(
                    device.os,
                  )}
                </p>

                <p className="mt-1 text-xs break-all text-rack-muted">
                  {device.architecture}
                </p>
              </td>

              <td className="px-4 py-4">
                <DeviceStatusBadge
                  status={device.status}
                />
              </td>

              <td className="px-4 py-4 font-mono text-xs break-all text-rack-muted">
                {device.agent_version}
              </td>

              <td className="px-4 py-4 text-xs whitespace-nowrap text-rack-muted">
                <time
                  dateTime={device.last_seen_at}
                  title={device.last_seen_at}
                >
                  {formatTimestamp(
                    device.last_seen_at,
                  )}
                </time>
              </td>

              <td className="px-4 py-4">
                <Link
                  to={`/devices/${encodeURIComponent(device.id)}`}
                  aria-label={`View details for ${device.hostname}`}
                  className="inline-flex min-h-9 min-w-9 items-center justify-center rounded-md text-rack-muted hover:bg-rack-background hover:text-rack-accent"
                >
                  <ChevronRight
                    size={18}
                    aria-hidden="true"
                  />
                </Link>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}