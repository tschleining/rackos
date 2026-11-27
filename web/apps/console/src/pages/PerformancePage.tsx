import {
  useEffect,
  useState,
} from "react";

import {
  Activity,
  CircleAlert,
  RefreshCw,
} from "lucide-react";

import { APIError } from "@rackos/api-client";

import {
  PerformanceDetails,
} from "../features/performance/PerformanceDetails";

import {
  MetricPanel,
  MetricRow,
} from "../features/performance/MetricPanel";

import {
  formatDeviceTimestamp,
  useDeviceDetail,
} from "../features/devices/deviceDetail";

import {
  useLatestPerformance,
} from "../hooks/usePerformance";

import {
  HistoricalPerformance,
} from "../features/performance/HistoricalPerformance";

const STALE_SAMPLE_MS = 3 * 60_000;
const FUTURE_CLOCK_TOLERANCE_MS = 30_000;

interface PerformanceMessageProps {
  title: string;
  description: string;
  loading?: boolean;
  error?: boolean;
  onRetry?: () => void;
}

function PerformanceMessage({
  title,
  description,
  loading = false,
  error = false,
  onRetry,
}: PerformanceMessageProps) {
  const Icon = loading
    ? RefreshCw
    : error
      ? CircleAlert
      : Activity;

  return (
    <section
      role={error ? "alert" : "status"}
      className="rounded-xl border border-rack-border bg-rack-surface px-6 py-14 text-center"
    >
      <Icon
        size={36}
        aria-hidden="true"
        className={[
          "mx-auto mb-4",
          error
            ? "text-rack-danger"
            : "text-rack-muted",
          loading
            ? "motion-safe:animate-spin"
            : "",
        ].join(" ")}
      />

      <h3 className="font-medium">
        {title}
      </h3>

      <p className="mx-auto mt-2 max-w-lg text-sm leading-relaxed text-rack-muted">
        {description}
      </p>

      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="mt-5 rounded-lg border border-rack-border px-4 py-2 text-sm font-medium text-rack-accent hover:border-rack-accent"
        >
          Try again
        </button>
      )}
    </section>
  );
}

export function PerformancePage() {
  const { device, isStale } = useDeviceDetail();

  const performance =
    useLatestPerformance(device.id);

  const [clock, setClock] =
    useState(() => Date.now());

  useEffect(() => {
    const interval = window.setInterval(
      () => setClock(Date.now()),
      15_000,
    );

    return () => {
      window.clearInterval(interval);
    };
  }, []);

  const deviceRemoved =
    performance.isError &&
    performance.error instanceof APIError &&
    performance.error.kind === "http" &&
    performance.error.status === 404 &&
    performance.error.serverMessage ===
      "device not found";

  const record = deviceRemoved
    ? null
    : performance.data;

  const now = Math.max(
    clock,
    performance.dataUpdatedAt,
  );

  const sampleAge = record
    ? now - Date.parse(record.collected_at)
    : null;

  const sampleIsOld =
    sampleAge !== null &&
    sampleAge >= STALE_SAMPLE_MS;

  const sampleIsAhead =
    sampleAge !== null &&
    sampleAge < -FUTURE_CLOCK_TOLERANCE_MS;

  const refreshFailed =
    performance.isError ||
    performance.isPaused;

  return (
    <>
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
            <div>
            <h2 className="text-xl font-semibold">
                Latest performance
            </h2>

            <p className="mt-2 text-sm leading-relaxed text-rack-muted">
                Resource utilization and activity from
                the most recently collected performance
                snapshot stored by the appliance.
            </p>

            <a
                href="#historical-performance"
                className="mt-3 inline-block text-sm font-medium text-rack-accent hover:underline"
            >
                View historical performance
            </a>
            </div>

        <button
          type="button"
          disabled={
            performance.isFetching ||
            performance.isPaused
          }
          onClick={() => {
            void performance.refetch();
          }}
          className="inline-flex min-h-10 items-center gap-2 rounded-lg border border-rack-border bg-rack-surface px-4 py-2 text-sm text-rack-muted hover:border-rack-accent hover:text-rack-primary disabled:cursor-not-allowed disabled:opacity-50"
        >
          <RefreshCw
            size={16}
            aria-hidden="true"
            className={
              performance.isFetching
                ? "motion-safe:animate-spin"
                : ""
            }
          />

          {performance.isFetching
            ? "Refreshing..."
            : "Refresh performance"}
        </button>
      </div>

      {!isStale && device.status === "offline" && (
        <div
          role="status"
          className="mb-6 rounded-lg border border-rack-warning/30 bg-rack-warning/5 p-4 text-sm leading-relaxed text-rack-warning"
        >
          This agent is offline. Stored performance
          measurements remain available, but new
          measurements require a successful agent report.
        </div>
      )}

      {record && refreshFailed && (
        <div
          role="alert"
          className="mb-6 rounded-lg border border-rack-warning/30 bg-rack-warning/5 p-4 text-sm text-rack-warning"
        >
          {performance.isPaused
            ? "Performance refresh is paused. Showing the last successfully retrieved sample."
            : "Unable to refresh performance. Showing the last successfully retrieved sample."}
        </div>
      )}

      {record && sampleIsOld && (
        <div
          role="status"
          className="mb-6 rounded-lg border border-rack-warning/30 bg-rack-warning/5 p-4 text-sm leading-relaxed text-rack-warning"
        >
          The latest retrieved performance sample is
          at least three minutes old. These measurements
          may not represent current resource utilization.
        </div>
      )}

      {record && sampleIsAhead && (
        <div
          role="status"
          className="mb-6 rounded-lg border border-rack-warning/30 bg-rack-warning/5 p-4 text-sm leading-relaxed text-rack-warning"
        >
          The sample collection timestamp is ahead of
          your browser's clock. Measurement freshness
          cannot be established reliably.
        </div>
      )}

      {record ? (
        <div className="space-y-6">
          <MetricPanel title="Sample information">
            <dl className="divide-y divide-rack-border">
              <MetricRow label="Collected at">
                <time
                  dateTime={record.collected_at}
                  title={record.collected_at}
                >
                  {formatDeviceTimestamp(
                    record.collected_at,
                  )}
                </time>
              </MetricRow>

              <MetricRow label="Received at">
                <time
                  dateTime={record.received_at}
                  title={record.received_at}
                >
                  {formatDeviceTimestamp(
                    record.received_at,
                  )}
                </time>
              </MetricRow>

              <MetricRow label="Sample ID">
                <span className="font-mono text-xs">
                  {record.sample_id}
                </span>
              </MetricRow>

              <MetricRow label="Payload version">
                {record.payload_version}
              </MetricRow>
            </dl>
          </MetricPanel>

          <PerformanceDetails
            snapshot={record.snapshot}
          />
        </div>
      ) : deviceRemoved ? (
        <PerformanceMessage
          title="Device no longer registered"
          description="Performance measurements are unavailable for this device."
          error
        />
      ) : performance.isPaused ? (
        <PerformanceMessage
          title="Connection paused"
          description="Performance retrieval will resume when the connection becomes available."
        />
      ) : performance.isError ? (
        <PerformanceMessage
          title="Performance unavailable"
          description="Unable to retrieve performance measurements from the appliance."
          error
          onRetry={() => {
            void performance.refetch();
          }}
        />
      ) : performance.isPending ? (
        <PerformanceMessage
          title="Loading performance"
          description="Retrieving the latest stored performance sample."
          loading
        />
        
      ) : (
        <PerformanceMessage
          title="No performance samples"
          description="No performance measurements are currently stored for this device. Measurements will appear after a successful agent report."
        />
      )}

      <footer className="mt-6 flex flex-wrap items-center justify-between gap-3 text-xs text-rack-muted">
        <p>
          Latest performance refreshes every 30 seconds
          while this page is active.
        </p>

        {performance.dataUpdatedAt > 0 && (
          <p>
            Last successful lookup:{" "}

            <time
              dateTime={new Date(
                performance.dataUpdatedAt,
              ).toISOString()}
            >
              {new Date(
                performance.dataUpdatedAt,
              ).toLocaleString()}
            </time>
          </p>
        )}
      </footer>
      
      <HistoricalPerformance deviceID={device.id} />
    </>
  );
}