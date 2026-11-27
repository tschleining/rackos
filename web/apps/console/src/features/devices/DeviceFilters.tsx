import {
  Search,
  X,
} from "lucide-react";

import type {
  DeviceStatusFilter,
} from "./deviceList";

interface DeviceFiltersProps {
  search: string;
  status: DeviceStatusFilter;

  onSearchChange: (value: string) => void;

  onStatusChange: (
    value: DeviceStatusFilter,
  ) => void;

  onClear: () => void;
}

export function DeviceFilters({
  search,
  status,
  onSearchChange,
  onStatusChange,
  onClear,
}: DeviceFiltersProps) {
  const hasFilters =
    search.trim().length > 0 ||
    status !== "all";

  return (
    <div className="flex flex-wrap items-end gap-4">
      <div className="min-w-0 flex-1 basis-full sm:basis-64">
        <label
          htmlFor="device-search"
          className="mb-2 block text-xs font-medium text-rack-muted"
        >
          Search devices
        </label>

        <div className="relative">
          <Search
            size={18}
            aria-hidden="true"
            className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-rack-muted"
          />

          <input
            id="device-search"
            type="search"
            value={search}
            onChange={(event) => {
              onSearchChange(event.target.value);
            }}
            placeholder="Hostname, platform, or device ID"
            className="h-11 w-full rounded-lg border border-rack-border bg-rack-background pr-3 pl-10 text-sm text-rack-primary placeholder:text-rack-muted"
          />
        </div>
      </div>

      <div className="min-w-36 flex-1 sm:flex-none">
        <label
          htmlFor="device-status"
          className="mb-2 block text-xs font-medium text-rack-muted"
        >
          Device status
        </label>

        <select
          id="device-status"
          value={status}
          onChange={(event) => {
            const value = event.target.value;

            if (
              value === "all" ||
              value === "online" ||
              value === "offline"
            ) {
              onStatusChange(value);
            }
          }}
          className="h-11 w-full rounded-lg border border-rack-border bg-rack-background px-3 text-sm text-rack-primary"
        >
          <option value="all">
            All statuses
          </option>

          <option value="online">
            Online
          </option>

          <option value="offline">
            Offline
          </option>
        </select>
      </div>

      {hasFilters && (
        <button
          type="button"
          onClick={onClear}
          className="inline-flex min-h-11 items-center gap-2 rounded-lg px-3 text-sm text-rack-muted hover:bg-rack-background hover:text-rack-primary"
        >
          <X
            size={16}
            aria-hidden="true"
          />

          Clear filters
        </button>
      )}
    </div>
  );
}