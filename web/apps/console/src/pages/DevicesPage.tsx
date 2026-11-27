
import {
  useMemo,
  useState,
} from "react";

import {
  CircleAlert,
  CircleCheck,
  RefreshCw,
  Search,
  Server,
} from "lucide-react";

import {
  PageHeader,
} from "../components/PageHeader";

import {
  QueryFailureNotice,
} from "../components/StatusNotice";

import {
  RefreshButton,
} from "../components/RefreshButton";

import {
  DeviceStatCard,
} from "../features/dashboard/DeviceStatCard";

import {
  DeviceFilters,
} from "../features/devices/DeviceFilters";

import {
  DeviceTable,
} from "../features/devices/DeviceTable";

import {
  defaultDeviceSort,
  selectDevices,
} from "../features/devices/deviceList";

import type {
  DeviceSort,
  DeviceSortField,
  DeviceStatusFilter,
} from "../features/devices/deviceList";

import {
  useDevices,
} from "../hooks/useDevices";

export function DevicesPage() {
  const devices = useDevices();

  const [search, setSearch] =
    useState("");

  const [status, setStatus] =
    useState<DeviceStatusFilter>("all");

  const [sort, setSort] =
    useState<DeviceSort>(defaultDeviceSort);

  const data = devices.data;

  const total = data?.total;

  const online = data?.devices.filter(
    (device) => device.status === "online",
  ).length;

  const offline = data?.devices.filter(
    (device) => device.status === "offline",
  ).length;

  const visibleDevices = useMemo(
    () =>
      selectDevices(
        data?.devices ?? [],
        {
          search,
          status,
          sort,
        },
      ),
    [data, search, status, sort],
  );

  function changeSort(
    field: DeviceSortField,
  ) {
    setSort((previous) => {
      if (previous.field === field) {
        return {
          field,
          direction:
            previous.direction === "asc"
              ? "desc"
              : "asc",
        };
      }

      return {
        field,
        direction:
          field === "last_seen_at"
            ? "desc"
            : "asc",
      };
    });
  }

  function clearFilters() {
    setSearch("");
    setStatus("all");
  }

  return (
    <>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <PageHeader
          title="Devices"
          description="Monitor registered infrastructure and connected agents."
        />

        <RefreshButton
          isFetching={devices.isFetching}
          isPaused={devices.isPaused}
          onRefresh={() => {
            void devices.refetch();
          }}
        />
      </div>

      <QueryFailureNotice
        query={devices}
        resource="registered devices"
        hasData={data !== undefined}
      />

      <div className="grid gap-4 sm:grid-cols-3 sm:gap-6">
        <DeviceStatCard
          label="Registered devices"
          value={total}
          description="All registered infrastructure"
          icon={Server}
        />

        <DeviceStatCard
          label="Online"
          value={online}
          description="Devices currently reporting heartbeats"
          icon={CircleCheck}
          tone="success"
        />

        <DeviceStatCard
          label="Offline"
          value={offline}
          description="Devices with expired or invalid heartbeats"
          icon={CircleAlert}
          tone="danger"
        />
      </div>

      <section className="mt-6 min-w-0 rounded-xl border border-rack-border bg-rack-surface">
        <div className="p-5 sm:p-6">
          <h2 className="mb-5 text-lg font-medium">
            Registered infrastructure
          </h2>

          <DeviceFilters
            search={search}
            status={status}
            onSearchChange={setSearch}
            onStatusChange={setStatus}
            onClear={clearFilters}
          />
        </div>

        {!data ? (
          <div
            role="status"
            className="border-t border-rack-border px-6 py-14 text-center"
          >
            {devices.isPending &&
            !devices.isPaused ? (
              <RefreshCw
                size={32}
                aria-hidden="true"
                className="mx-auto mb-4 text-rack-muted motion-safe:animate-spin"
              />
            ) : (
              <Server
                size={32}
                aria-hidden="true"
                className="mx-auto mb-4 text-rack-muted"
              />
            )}

            <p className="text-sm font-medium">
              {devices.isPaused
                ? "Connection paused"
                : devices.isError
                  ? "Device information unavailable"
                  : "Loading devices..."}
            </p>

            <p className="mt-2 text-sm text-rack-muted">
              Device information will appear after
              a successful appliance request.
            </p>
          </div>
        ) : data.devices.length === 0 ? (
          <div className="border-t border-rack-border px-6 py-14 text-center">
            <Server
              size={36}
              aria-hidden="true"
              className="mx-auto mb-4 text-rack-muted"
            />

            <p className="font-medium">
              No registered devices
            </p>

            <p className="mx-auto mt-2 max-w-md text-sm leading-relaxed text-rack-muted">
              Enroll a RackOS agent using the
              command-line utilities to begin
              monitoring your infrastructure.
            </p>
          </div>
        ) : visibleDevices.length === 0 ? (
          <div className="border-t border-rack-border px-6 py-14 text-center">
            <Search
              size={36}
              aria-hidden="true"
              className="mx-auto mb-4 text-rack-muted"
            />

            <p className="font-medium">
              No matching devices
            </p>

            <p className="mt-2 text-sm text-rack-muted">
              Adjust your search or status filter.
            </p>

            <button
              type="button"
              onClick={clearFilters}
              className="mt-5 rounded-lg border border-rack-border px-4 py-2 text-sm font-medium text-rack-accent hover:border-rack-accent"
            >
              Clear filters
            </button>
          </div>
        ) : (
          <DeviceTable
            devices={visibleDevices}
            sort={sort}
            onSort={changeSort}
          />
        )}

        <footer className="flex flex-wrap items-center justify-between gap-3 border-t border-rack-border px-5 py-4 text-xs text-rack-muted sm:px-6">
          <p role="status">
            {data
              ? `Showing ${visibleDevices.length} of ${data.total} devices`
              : "Device count unavailable"}
          </p>

          {data && (
            <p>
              Heartbeat times are shown in your
              browser's local timezone.
            </p>
          )}
        </footer>
      </section>

      <footer className="mt-6 flex flex-wrap items-center justify-between gap-2 text-xs text-rack-muted">
        <p>
          Automatically refreshes every 15 seconds
          while the console is active.
        </p>

        {devices.dataUpdatedAt > 0 && (
          <p>
            Last successful refresh:{" "}

            <time
              dateTime={new Date(
                devices.dataUpdatedAt,
              ).toISOString()}
            >
              {new Date(
                devices.dataUpdatedAt,
              ).toLocaleString()}
            </time>
          </p>
        )}
      </footer>
    </>
  );
}
