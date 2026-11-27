import type { ReactNode } from "react";

import type {
  UInt64String,
} from "@rackos/api-client";

import {
  formatBytes,
  formatExactBytes,
  formatPercentage,
} from "../../lib/format";

interface MetricPanelProps {
  title: string;
  description?: string;
  children: ReactNode;
}

export function MetricPanel({
  title,
  description,
  children,
}: MetricPanelProps) {
  return (
    <section className="min-w-0 rounded-xl border border-rack-border bg-rack-surface p-5 sm:p-6">
      <h3 className="text-lg font-medium">
        {title}
      </h3>

      {description && (
        <p className="mt-2 text-sm text-rack-muted">
          {description}
        </p>
      )}

      <div className="mt-5">
        {children}
      </div>
    </section>
  );
}

interface MetricRowProps {
  label: string;
  children: ReactNode;
}

export function MetricRow({
  label,
  children,
}: MetricRowProps) {
  const unavailable =
    children === undefined ||
    children === null;

  return (
    <div className="flex flex-wrap items-start justify-between gap-3 py-3">
      <dt className="text-sm text-rack-muted">
        {label}
      </dt>

      <dd
        className={[
          "min-w-0 max-w-full text-sm break-all tabular-nums",
          unavailable
            ? "text-rack-muted"
            : "font-medium",
        ].join(" ")}
      >
        {unavailable
          ? "Unavailable"
          : children}
      </dd>
    </div>
  );
}

interface UsageMeterProps {
  label: string;
  value: number;
}

export function UsageMeter({
  label,
  value,
}: UsageMeterProps) {
  return (
    <div>
      <div className="mb-3 flex items-center justify-between gap-3">
        <span className="text-sm text-rack-muted">
          {label}
        </span>

        <span className="text-2xl font-semibold tabular-nums">
          {formatPercentage(value)}
        </span>
      </div>

      <meter
        min={0}
        max={100}
        value={value}
        aria-label={label}
        className="block h-4 w-full"
      >
        {formatPercentage(value)}
      </meter>

      <div
        aria-hidden="true"
        className="mt-1 flex justify-between text-xs text-rack-muted"
      >
        <span>0%</span>
        <span>100%</span>
      </div>
    </div>
  );
}

export function ByteCapacity({
  value,
}: {
  value: UInt64String;
}) {
  return (
    <span title={formatExactBytes(value)}>
      {formatBytes(value)}
    </span>
  );
}