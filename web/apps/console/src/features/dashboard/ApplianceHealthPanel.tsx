
import {
  Activity,
  ArrowRight,
} from "lucide-react";

import { Link } from "react-router";

import {
  useApplianceHealth,
  useApplianceReadiness,
  useApplianceVersion,
} from "../../hooks/useAppliance";

export function ApplianceHealthPanel() {
  const health = useApplianceHealth();
  const readiness = useApplianceReadiness();
  const version = useApplianceVersion();

  const healthLabel = health.isPaused
    ? "Paused"
    : health.isError
      ? "Unavailable"
      : health.isPending
        ? "Checking..."
        : "Healthy";

  const readinessLabel = readiness.isPaused
    ? "Paused"
    : readiness.isError
      ? "Unknown"
      : readiness.isPending
        ? "Checking..."
        : readiness.data?.status === "ready"
          ? "Ready"
          : "Not ready";

  const versionLabel = version.isError
    ? "Unavailable"
    : version.data?.version
      ?? (version.isPaused ? "Paused" : "Loading...");

  const healthColor =
    healthLabel === "Healthy"
      ? "text-rack-success"
      : healthLabel === "Unavailable"
        ? "text-rack-danger"
        : "text-rack-muted";

  const readinessColor =
    readinessLabel === "Ready"
      ? "text-rack-success"
      : readinessLabel === "Not ready" ||
          readinessLabel === "Unknown"
        ? "text-rack-warning"
        : "text-rack-muted";

  return (
    <section className="rounded-xl border border-rack-border bg-rack-surface p-5 sm:p-6">
      <div className="mb-5 flex items-center gap-3">
        <Activity
          size={21}
          className="text-rack-accent"
          aria-hidden="true"
        />

        <h2 className="text-lg font-medium">
          Appliance status
        </h2>
      </div>

      <dl className="divide-y divide-rack-border text-sm">
        <div className="flex items-center justify-between gap-4 py-4">
          <dt className="text-rack-muted">
            Liveness
          </dt>

          <dd className={healthColor}>
            {healthLabel}
          </dd>
        </div>

        <div className="flex items-center justify-between gap-4 py-4">
          <dt className="text-rack-muted">
            Readiness
          </dt>

          <dd className={readinessColor}>
            {readinessLabel}
          </dd>
        </div>

        <div className="flex items-center justify-between gap-4 py-4">
          <dt className="text-rack-muted">
            Software version
          </dt>

          <dd className="break-all text-right font-mono">
            {versionLabel}
          </dd>
        </div>

        <div className="flex items-center justify-between gap-4 py-4">
          <dt className="text-rack-muted">
            Management access
          </dt>

          <dd className="text-right">
            Local only
          </dd>
        </div>
      </dl>

      <Link
        to="/system"
        className="mt-4 inline-flex items-center gap-2 text-sm font-medium text-rack-accent hover:underline"
      >
        System information

        <ArrowRight
          size={16}
          aria-hidden="true"
        />
      </Link>
    </section>
  );
}
