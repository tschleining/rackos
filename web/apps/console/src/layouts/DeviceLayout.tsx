import {
  Activity,
  ArrowLeft,
  CircleAlert,
  Cpu,
  RefreshCw,
  Server,
} from "lucide-react";

import {
  Link,
  NavLink,
  Outlet,
  useParams,
} from "react-router";

import {
  APIError,
  isDeviceID,
} from "@rackos/api-client";

import {
  DeviceStatusBadge,
} from "../components/DeviceStatusBadge";

import {
  PageHeader,
} from "../components/PageHeader";

import {
  formatOperatingSystem,
} from "../features/devices/deviceList";

import type {
  DeviceDetailContext,
} from "../features/devices/deviceDetail";

import {
  useDevice,
} from "../hooks/useDevices";

interface DeviceMessageProps {
  title: string;
  description: string;
  loading?: boolean;
  onRetry?: () => void;
}

function DeviceMessage({
  title,
  description,
  loading = false,
  onRetry,
}: DeviceMessageProps) {
  const Icon = loading
    ? RefreshCw
    : CircleAlert;

  return (
    <>
      <Link
        to="/devices"
        className="mb-6 inline-flex items-center gap-2 text-sm text-rack-accent hover:underline"
      >
        <ArrowLeft size={16} aria-hidden="true" />
        Back to devices
      </Link>

      <PageHeader title={title} />

      <section
        role={loading ? "status" : "region"}
        aria-label={title}
        className="rounded-xl border border-rack-border bg-rack-surface px-6 py-14 text-center"
      >
        <Icon
          size={36}
          aria-hidden="true"
          className={[
            "mx-auto mb-4 text-rack-muted",
            loading
              ? "motion-safe:animate-spin"
              : "",
          ].join(" ")}
        />

        <p className="mx-auto max-w-lg text-sm leading-relaxed text-rack-muted">
          {description}
        </p>

        {onRetry && (
          <button
            type="button"
            onClick={onRetry}
            className="mt-6 rounded-lg border border-rack-border px-4 py-2 text-sm font-medium text-rack-accent hover:border-rack-accent"
          >
            Try again
          </button>
        )}
      </section>
    </>
  );
}

interface RegisteredDeviceLayoutProps {
  deviceID: string;
}

function RegisteredDeviceLayout({
  deviceID,
}: RegisteredDeviceLayoutProps) {
  const query = useDevice(deviceID);

  const deviceNotFound =
    query.isError &&
    query.error instanceof APIError &&
    query.error.kind === "http" &&
    query.error.status === 404 &&
    query.error.serverMessage ===
      "device not found";

  if (deviceNotFound) {
    return (
      <DeviceMessage
        title="Device not found"
        description="This device is not registered with the RackOS appliance. It may have been removed."
      />
    );
  }

  if (!query.data) {
    if (query.isPaused) {
      return (
        <DeviceMessage
          title="Connection paused"
          description="Device information cannot be retrieved until the connection resumes."
        />
      );
    }

    if (query.isError) {
      return (
        <DeviceMessage
          title="Device information unavailable"
          description="Unable to retrieve this device from the RackOS appliance."
          onRetry={() => {
            void query.refetch();
          }}
        />
      );
    }

    return (
      <DeviceMessage
        title="Loading device"
        description="Retrieving registered device information."
        loading
      />
    );
  }

  const device = query.data;

  const basePath =
    `/devices/${encodeURIComponent(device.id)}`;

  const context: DeviceDetailContext = {
    device,
    dataUpdatedAt: query.dataUpdatedAt,
    isStale: query.isError || query.isPaused,
  };

  const tabs = [
    {
      label: "Overview",
      path: basePath,
      icon: Server,
      end: true,
    },
    {
      label: "Inventory",
      path: `${basePath}/inventory`,
      icon: Cpu,
      end: true,
    },
    {
      label: "Performance",
      path: `${basePath}/performance`,
      icon: Activity,
      end: true,
    },
  ];

  return (
    <>
      <Link
        to="/devices"
        className="mb-6 inline-flex items-center gap-2 text-sm text-rack-accent hover:underline"
      >
        <ArrowLeft size={16} aria-hidden="true" />
        Back to devices
      </Link>

      <header className="mb-6 flex flex-wrap items-center justify-between gap-5">
        <div className="flex min-w-0 items-center gap-4">
          <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-xl border border-rack-border bg-rack-surface text-rack-accent">
            <Server size={28} aria-hidden="true" />
          </div>

          <div className="min-w-0">
            <h1 className="text-2xl font-semibold break-all tracking-tight sm:text-3xl">
              {device.hostname}
            </h1>

            <p className="mt-1 text-sm text-rack-muted">
              {formatOperatingSystem(device.os)}
              {" · "}
              {device.architecture}
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          <div className="flex flex-col items-start gap-1">
            <DeviceStatusBadge
              status={device.status}
            />

            {context.isStale && (
              <span className="text-xs text-rack-warning">
                Last known status
              </span>
            )}
          </div>

          <button
            type="button"
            disabled={
              query.isFetching ||
              query.isPaused
            }
            onClick={() => {
              void query.refetch();
            }}
            className="inline-flex min-h-10 items-center gap-2 rounded-lg border border-rack-border bg-rack-surface px-4 py-2 text-sm text-rack-muted hover:text-rack-primary disabled:cursor-not-allowed disabled:opacity-50"
          >
            <RefreshCw
              size={16}
              aria-hidden="true"
              className={
                query.isFetching
                  ? "motion-safe:animate-spin"
                  : ""
              }
            />

            {query.isFetching
              ? "Refreshing..."
              : "Refresh device"}
          </button>
        </div>
      </header>

      {context.isStale && (
        <div
          role="alert"
          className="mb-6 rounded-lg border border-rack-warning/30 bg-rack-warning/5 p-4 text-sm text-rack-warning"
        >
          {query.isPaused
            ? "Device refresh is paused. Showing last known information."
            : "Unable to refresh device information. Displayed metadata and connectivity status may be outdated."}
        </div>
      )}

      <nav
        aria-label="Device sections"
        className="mb-8 flex flex-wrap gap-2 border-b border-rack-border sm:gap-6"
      >
        {tabs.map((tab) => {
          const Icon = tab.icon;

          return (
            <NavLink
              key={tab.path}
              to={tab.path}
              end={tab.end}
              className={({ isActive }) =>
                [
                  "inline-flex min-h-12 items-center gap-2 border-b-2 px-2 text-sm font-medium",
                  isActive
                    ? "border-rack-accent text-rack-accent"
                    : "border-transparent text-rack-muted hover:text-rack-primary",
                ].join(" ")
              }
            >
              <Icon
                size={17}
                aria-hidden="true"
              />

              {tab.label}
            </NavLink>
          );
        })}
      </nav>

      <Outlet context={context} />

      <footer className="mt-6 flex flex-wrap items-center justify-between gap-2 text-xs text-rack-muted">
        <p>
          Device information refreshes every 15 seconds
          while the console is active.
        </p>

        <p>
          Last successful device refresh:{" "}

          <time
            dateTime={new Date(
              query.dataUpdatedAt,
            ).toISOString()}
          >
            {new Date(
              query.dataUpdatedAt,
            ).toLocaleString()}
          </time>
        </p>
      </footer>
    </>
  );
}

export function DeviceLayout() {
  const { deviceId } = useParams();

  if (!isDeviceID(deviceId)) {
    return (
      <DeviceMessage
        title="Invalid device ID"
        description="RackOS requires a valid registered device identifier. Select a device from the registered devices page."
      />
    );
  }

  return (
    <RegisteredDeviceLayout
      key={deviceId}
      deviceID={deviceId}
    />
  );
}