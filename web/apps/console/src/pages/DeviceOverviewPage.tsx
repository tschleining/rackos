import {
  Activity,
  ArrowRight,
  Clock,
  Cpu,
  Server,
} from "lucide-react";

import type {
  LucideIcon,
} from "lucide-react";

import { Link } from "react-router";

import {
  DeviceStatusBadge,
} from "../components/DeviceStatusBadge";

import {
  formatOperatingSystem,
} from "../features/devices/deviceList";

import {
  formatDeviceTimestamp,
  useDeviceDetail,
} from "../features/devices/deviceDetail";

import type {
  ReactNode,
} from "react";

interface InformationRowProps {
  label: string;
  children: ReactNode;
}

function InformationRow({
  label,
  children,
}: InformationRowProps) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3 py-4">
      <dt className="text-sm text-rack-muted">
        {label}
      </dt>

      <dd className="min-w-0 max-w-full text-sm font-medium break-all">
        {children}
      </dd>
    </div>
  );
}

interface DeviceSectionLinkProps {
  title: string;
  description: string;
  destination: string;
  icon: LucideIcon;
}

function DeviceSectionLink({
  title,
  description,
  destination,
  icon: Icon,
}: DeviceSectionLinkProps) {
  return (
    <Link
      to={destination}
      className="group block rounded-xl border border-rack-border bg-rack-surface p-6 transition-colors hover:border-rack-accent motion-reduce:transition-none"
    >
      <div className="mb-4 flex items-center justify-between">
        <Icon
          size={26}
          aria-hidden="true"
          className="text-rack-accent"
        />

        <ArrowRight
          size={19}
          aria-hidden="true"
          className="text-rack-muted group-hover:text-rack-accent"
        />
      </div>

      <h3 className="font-medium">
        {title}
      </h3>

      <p className="mt-2 text-sm leading-relaxed text-rack-muted">
        {description}
      </p>
    </Link>
  );
}

function DeviceTimestamp({
  value,
}: {
  value: string;
}) {
  return (
    <time
      dateTime={value}
      title={value}
    >
      {formatDeviceTimestamp(value)}
    </time>
  );
}

export function DeviceOverviewPage() {
  const {
    device,
    isStale,
  } = useDeviceDetail();

  const basePath =
    `/devices/${encodeURIComponent(device.id)}`;

  return (
    <>
      <div className="mb-6">
        <h2 className="text-xl font-semibold">
          Device overview
        </h2>

        <p className="mt-2 text-sm text-rack-muted">
          Registered identity, agent connectivity
          and monitoring information.
        </p>
      </div>

      <div className="grid items-start gap-6 xl:grid-cols-2">
        <section className="min-w-0 rounded-xl border border-rack-border bg-rack-surface p-5 sm:p-6">
          <div className="mb-4 flex items-center gap-3">
            <Server
              size={21}
              className="text-rack-accent"
              aria-hidden="true"
            />

            <h3 className="text-lg font-medium">
              Device information
            </h3>
          </div>

          <dl className="divide-y divide-rack-border">
            <InformationRow label="Hostname">
              {device.hostname}
            </InformationRow>

            <InformationRow label="Device ID">
              <span className="font-mono text-xs">
                {device.id}
              </span>
            </InformationRow>

            <InformationRow label="Operating system">
              {formatOperatingSystem(device.os)}
            </InformationRow>

            <InformationRow label="Architecture">
              {device.architecture}
            </InformationRow>

            <InformationRow label="Agent version">
              <span className="font-mono">
                {device.agent_version}
              </span>
            </InformationRow>

            <InformationRow label="Registered">
              <DeviceTimestamp
                value={device.created_at}
              />
            </InformationRow>

            <InformationRow label="Last updated">
              <DeviceTimestamp
                value={device.updated_at}
              />
            </InformationRow>
          </dl>
        </section>

        <section className="min-w-0 rounded-xl border border-rack-border bg-rack-surface p-5 sm:p-6">
          <div className="mb-4 flex items-center gap-3">
            <Clock
              size={21}
              className="text-rack-accent"
              aria-hidden="true"
            />

            <h3 className="text-lg font-medium">
              Connectivity
            </h3>
          </div>

          <dl className="divide-y divide-rack-border">
            <InformationRow label="Agent status">
              <DeviceStatusBadge
                status={device.status}
              />
            </InformationRow>

            <InformationRow label="Last recorded heartbeat">
              <DeviceTimestamp
                value={device.last_seen_at}
              />
            </InformationRow>

            <InformationRow label="Default heartbeat interval">
              30 seconds
            </InformationRow>

            <InformationRow label="Offline threshold">
              90 seconds
            </InformationRow>
          </dl>

          <div
            className={[
              "mt-5 rounded-lg border p-4 text-sm leading-relaxed",
              isStale
                ? "border-rack-warning/30 text-rack-warning"
                : "border-rack-border text-rack-muted",
            ].join(" ")}
          >
            {isStale
              ? "The current connectivity status could not be refreshed. The displayed status is the last known value."
              : device.status === "online"
                ? "The appliance considers this agent online based on its most recent heartbeat."
                : "The appliance considers this agent offline. Previously collected inventory and performance measurements may still be available."}
          </div>
        </section>
      </div>

      <section className="mt-8">
        <h2 className="mb-4 text-lg font-medium">
          Device monitoring
        </h2>

        <div className="grid gap-4 sm:grid-cols-2">
          <DeviceSectionLink
            title="Hardware inventory"
            description="View processor information, memory capacity, hardware specifications and operating system details."
            destination={`${basePath}/inventory`}
            icon={Cpu}
          />

          <DeviceSectionLink
            title="Performance"
            description="Inspect CPU utilization, memory usage, storage activity, network throughput and historical measurements."
            destination={`${basePath}/performance`}
            icon={Activity}
          />
        </div>
      </section>

      <p className="mt-6 text-xs text-rack-muted">
        Timestamps are displayed in your browser's
        local timezone.
      </p>
    </>
  );
}