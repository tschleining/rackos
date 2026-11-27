import type {
  Device,
  DeviceStatus,
} from "@rackos/api-client";

export type DeviceStatusFilter =
  | "all"
  | DeviceStatus;

export type DeviceSortField =
  | "hostname"
  | "platform"
  | "status"
  | "agent_version"
  | "last_seen_at";

export type SortDirection =
  | "asc"
  | "desc";

export interface DeviceSort {
  field: DeviceSortField;
  direction: SortDirection;
}

export interface DeviceListOptions {
  search: string;
  status: DeviceStatusFilter;
  sort: DeviceSort;
}

export const defaultDeviceSort: DeviceSort = {
  field: "hostname",
  direction: "asc",
};

const collator = new Intl.Collator(
  undefined,
  {
    numeric: true,
    sensitivity: "base",
  },
);

export function formatOperatingSystem(
  os: string,
): string {
  switch (os.toLowerCase()) {
    case "darwin":
      return "macOS";

    case "linux":
      return "Linux";

    case "windows":
      return "Windows";

    case "freebsd":
      return "FreeBSD";

    default:
      return os;
  }
}

function matchesSearch(
  device: Device,
  search: string,
): boolean {
  const terms = search
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);

  if (terms.length === 0) {
    return true;
  }

  const searchable = [
    device.hostname,
    device.id,
    device.os,
    formatOperatingSystem(device.os),
    device.architecture,
    device.agent_version,
  ]
    .join(" ")
    .toLowerCase();

  return terms.every(
    (term) => searchable.includes(term),
  );
}

function compareDevices(
  first: Device,
  second: Device,
  field: DeviceSortField,
): number {
  switch (field) {
    case "hostname":
      return collator.compare(
        first.hostname,
        second.hostname,
      );

    case "platform":
      return collator.compare(
        `${formatOperatingSystem(first.os)} ${first.architecture}`,
        `${formatOperatingSystem(second.os)} ${second.architecture}`,
      );

    case "status":
      return collator.compare(
        first.status,
        second.status,
      );

    case "agent_version":
      return collator.compare(
        first.agent_version,
        second.agent_version,
      );

    case "last_seen_at":
      return (
        Date.parse(first.last_seen_at) -
        Date.parse(second.last_seen_at)
      );
  }
}

export function selectDevices(
  devices: readonly Device[],
  options: DeviceListOptions,
): Device[] {
  const filtered = devices.filter(
    (device) =>
      (
        options.status === "all" ||
        device.status === options.status
      ) &&
      matchesSearch(
        device,
        options.search,
      ),
  );

  return filtered.sort(
    (first, second) => {
      const comparison = compareDevices(
        first,
        second,
        options.sort.field,
      );

      if (comparison !== 0) {
        return options.sort.direction === "asc"
          ? comparison
          : -comparison;
      }

      // Preserve deterministic ordering when
      // multiple devices have the same value.
      return first.id.localeCompare(second.id);
    },
  );
}