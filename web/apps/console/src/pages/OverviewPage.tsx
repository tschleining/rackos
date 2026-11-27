
import {
  CircleCheck,
  CircleAlert,
  Server,
} from "lucide-react";

import {
  PageHeader,
} from "../components/PageHeader";

import {
  DeviceStatCard,
} from "../features/dashboard/DeviceStatCard";

import {
  ApplianceHealthPanel,
} from "../features/dashboard/ApplianceHealthPanel";

import {
  RecentDevicesPanel,
} from "../features/dashboard/RecentDevicesPanel";

import {
  useDevices,
} from "../hooks/useDevices";

import {
  useApplianceHealth,
  useApplianceReadiness,
  useApplianceVersion,
} from "../hooks/useAppliance";

import {
  QueryFailureNotice,
} from "../components/StatusNotice";

import {
  RefreshButton,
} from "../components/RefreshButton";

export function OverviewPage() {
  const devices = useDevices();

  const health = useApplianceHealth();
  const readiness = useApplianceReadiness();
  const version = useApplianceVersion();

  const data = devices.data;

  const total = data?.total;

  const online = data?.devices.filter(
    (device) => device.status === "online",
  ).length;

  const offline = data?.devices.filter(
    (device) => device.status === "offline",
  ).length;

  const refreshing =
    devices.isFetching ||
    health.isFetching ||
    readiness.isFetching ||
    version.isFetching;

  const paused =
    devices.isPaused ||
    health.isPaused ||
    readiness.isPaused ||
    version.isPaused;

  async function refreshDashboard() {
    await Promise.all([
      devices.refetch(),
      health.refetch(),
      readiness.refetch(),
      version.refetch(),
    ]);
  }

  return (
    <>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <PageHeader
          title="Overview"
          description="Infrastructure status at a glance."
        />

        <RefreshButton
          isFetching={refreshing}
          isPaused={paused}
          onRefresh={() => {
            void refreshDashboard();
          }}
        />
      </div>

<QueryFailureNotice
  query={devices}
  resource="registered devices"
  hasData={data !== undefined}
/>

{devices.isPending &&
  !devices.isPaused && (
    <p
      role="status"
      className="mb-6 text-sm text-rack-muted"
    >
      Loading device information...
    </p>
  )}

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

      <div className="mt-6 grid items-start gap-6 xl:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
        <ApplianceHealthPanel />

        <RecentDevicesPanel
          devices={data?.devices}
        />
      </div>

      <footer className="mt-6 flex flex-wrap items-center justify-between gap-2 text-xs text-rack-muted">
        <p>
          Automatically refreshes every 15 seconds
          while the console is active.
        </p>

        {devices.dataUpdatedAt > 0 && (
          <p>
            Last successful device refresh:{" "}

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
