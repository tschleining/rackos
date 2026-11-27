import {
  Clock,
  Cpu,
  MemoryStick,
  Monitor,
} from "lucide-react";

import type {
  LucideIcon,
} from "lucide-react";

import type {
  ReactNode,
} from "react";

import type {
  HardwareInventory,
} from "@rackos/api-client";

import {
  formatOperatingSystem,
} from "../devices/deviceList";

import {
  formatDeviceTimestamp,
} from "../devices/deviceDetail";

import {
  formatBytes,
  formatCount,
  formatExactBytes,
} from "../../lib/format";

interface InventorySectionProps {
  title: string;
  icon: LucideIcon;
  children: ReactNode;
}

function InventorySection({
  title,
  icon: Icon,
  children,
}: InventorySectionProps) {
  return (
    <section className="min-w-0 rounded-xl border border-rack-border bg-rack-surface p-5 sm:p-6">
      <div className="mb-5 flex items-center gap-3">
        <Icon
          size={22}
          aria-hidden="true"
          className="text-rack-accent"
        />

        <h3 className="text-lg font-medium">
          {title}
        </h3>
      </div>

      {children}
    </section>
  );
}

interface InventoryFieldProps {
  label: string;
  children: ReactNode;
}

function InventoryField({
  label,
  children,
}: InventoryFieldProps) {
  const unavailable =
    children === undefined ||
    children === null;

  return (
    <div className="flex flex-wrap items-start justify-between gap-3 py-4">
      <dt className="text-sm text-rack-muted">
        {label}
      </dt>

      <dd
        className={[
          "min-w-0 max-w-full break-all text-sm",
          unavailable
            ? "text-rack-muted"
            : "font-medium text-rack-primary",
        ].join(" ")}
      >
        {unavailable
          ? "Unavailable"
          : children}
      </dd>
    </div>
  );
}

function InventoryTimestamp({
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

interface InventoryDetailsProps {
  inventory: HardwareInventory;
}

export function InventoryDetails({
  inventory,
}: InventoryDetailsProps) {
  return (
    <div className="grid items-start gap-6 xl:grid-cols-2">
      <InventorySection
        title="Processor"
        icon={Cpu}
      >
        <p className="text-xs text-rack-muted">
          Processor model
        </p>

        <p className="mt-2 text-xl font-semibold break-words">
          {inventory.cpu_model ?? "Unavailable"}
        </p>

        <div className="my-5 grid grid-cols-2 gap-3">
          <div className="rounded-lg border border-rack-border bg-rack-background p-4">
            <p className="text-xs text-rack-muted">
              Physical cores
            </p>

            <p className="mt-2 text-2xl font-semibold tabular-nums">
              {inventory.physical_cores !== undefined
                ? formatCount(inventory.physical_cores)
                : "—"}
            </p>
          </div>

          <div className="rounded-lg border border-rack-border bg-rack-background p-4">
            <p className="text-xs text-rack-muted">
              Logical CPUs
            </p>

            <p className="mt-2 text-2xl font-semibold tabular-nums">
              {formatCount(inventory.logical_cpus)}
            </p>
          </div>
        </div>

        <dl className="divide-y divide-rack-border">
          <InventoryField label="CPU manufacturer">
            {inventory.cpu_manufacturer}
          </InventoryField>

          <InventoryField label="Architecture">
            {inventory.architecture}
          </InventoryField>

          <InventoryField label="Host logical CPUs">
            {inventory.host_logical_cpus !== undefined
              ? formatCount(inventory.host_logical_cpus)
              : undefined}
          </InventoryField>
        </dl>

        <p className="mt-4 text-xs leading-relaxed text-rack-muted">
          Logical CPU availability may differ from
          the host's total logical CPU count.
        </p>
      </InventorySection>

      <InventorySection
        title="Memory and hardware"
        icon={MemoryStick}
      >
        <p className="text-xs text-rack-muted">
          Installed memory
        </p>

        {inventory.memory_bytes !== undefined ? (
          <>
            <p className="mt-2 text-3xl font-semibold tabular-nums">
              {formatBytes(inventory.memory_bytes)}
            </p>

            <p className="mt-2 font-mono text-xs break-all text-rack-muted">
              {formatExactBytes(
                inventory.memory_bytes,
              )}
            </p>
          </>
        ) : (
          <p className="mt-2 text-xl text-rack-muted">
            Unavailable
          </p>
        )}

        <dl className="mt-5 divide-y divide-rack-border border-t border-rack-border">
          <InventoryField label="Hardware model">
            {inventory.hardware_model}
          </InventoryField>

          <InventoryField label="Collected hostname">
            {inventory.hostname}
          </InventoryField>
        </dl>
      </InventorySection>

      <InventorySection
        title="Operating system"
        icon={Monitor}
      >
        <dl className="divide-y divide-rack-border">
          <InventoryField label="Platform">
            {formatOperatingSystem(inventory.os)}
          </InventoryField>

          <InventoryField label="OS version">
            {inventory.os_version}
          </InventoryField>

          <InventoryField label="Kernel version">
            {inventory.kernel_version}
          </InventoryField>

          <InventoryField label="Boot time">
            {inventory.boot_time !== undefined
              ? (
                <InventoryTimestamp
                  value={inventory.boot_time}
                />
              )
              : undefined}
          </InventoryField>
        </dl>
      </InventorySection>

      <InventorySection
        title="Collection information"
        icon={Clock}
      >
        <dl className="divide-y divide-rack-border">
          <InventoryField label="Device ID">
            <span className="font-mono text-xs">
              {inventory.device_id}
            </span>
          </InventoryField>

          <InventoryField label="Collected at">
            <InventoryTimestamp
              value={inventory.collected_at}
            />
          </InventoryField>

          <InventoryField label="Received at">
            <InventoryTimestamp
              value={inventory.received_at}
            />
          </InventoryField>
        </dl>

        <p className="mt-5 text-xs leading-relaxed text-rack-muted">
          Inventory is a stored hardware snapshot,
          not a live resource measurement. Times are
          displayed in your browser's local timezone.
        </p>
      </InventorySection>
    </div>
  );
}