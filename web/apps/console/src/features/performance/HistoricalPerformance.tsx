import {
  useMemo,
  useState,
} from "react";

import {
  CircleAlert,
  RefreshCw,
} from "lucide-react";

import {
  APIError,
} from "@rackos/api-client";

import type {
  DeviceID,
  PerformanceRecord,
} from "@rackos/api-client";

import {
  usePerformanceHistory,
} from "../../hooks/usePerformance";

import {
  formatDeviceTimestamp,
} from "../devices/deviceDetail";

import {
  MetricPanel,
} from "./MetricPanel";

import {
  HistoryChart,
} from "./HistoryChart";

import {
  buildResourceHistory,
  buildUtilizationHistory,
  DEFAULT_HISTORY_DURATION,
  getResourceOptions,
  HISTORY_RANGES,
} from "./history";

import type {
  ResourceKind,
} from "./history";

interface ResourceConfig {
  title: string;
  label: string;
  firstLabel: string;
  secondLabel?: string;
  unit: "percent" | "bytes-per-second";
}

const resourceConfig: Record<
  ResourceKind,
  ResourceConfig
> = {
  filesystem: {
    title: "Filesystem utilization",
    label: "Filesystem",
    firstLabel: "Used capacity",
    unit: "percent",
  },

  disk: {
    title: "Disk throughput",
    label: "Storage device",
    firstLabel: "Read",
    secondLabel: "Write",
    unit: "bytes-per-second",
  },

  network: {
    title: "Network throughput",
    label: "Network interface",
    firstLabel: "Receive",
    secondLabel: "Transmit",
    unit: "bytes-per-second",
  },
};

interface HistoryDataProps {
  samples: readonly PerformanceRecord[];
  fromMillis: number;
  toMillis: number;
}

interface ResourceHistoryProps
  extends HistoryDataProps {
  kind: ResourceKind;
}

function ResourceHistoryPanel({
  samples,
  fromMillis,
  toMillis,
  kind,
}: ResourceHistoryProps) {
  const config = resourceConfig[kind];

  const [selectedID, setSelectedID] =
    useState("");

  const options = useMemo(
    () => getResourceOptions(samples, kind),
    [samples, kind],
  );

  const activeOption =
    options.find(
      (option) => option.id === selectedID,
    ) ?? options[0];

  const activeID = activeOption?.id;

  const points = useMemo(
    () =>
      activeID === undefined
        ? []
        : buildResourceHistory(
            samples,
            kind,
            activeID,
          ),
    [samples, kind, activeID],
  );

  return (
    <MetricPanel title={config.title}>
      {activeOption === undefined ? (
        <p className="py-8 text-center text-sm text-rack-muted">
          No matching resources were reported
          in the retrieved history.
        </p>
      ) : (
        <>
          <div className="mb-5">
            <label
              htmlFor={`history-${kind}`}
              className="mb-2 block text-xs font-medium text-rack-muted"
            >
              {config.label}
            </label>

            <select
              id={`history-${kind}`}
              value={activeOption.id}
              onChange={(event) => {
                setSelectedID(
                  event.target.value,
                );
              }}
              className="h-11 w-full max-w-lg rounded-lg border border-rack-border bg-rack-background px-3 text-sm text-rack-primary"
            >
              {options.map((option) => (
                <option
                  key={option.id}
                  value={option.id}
                >
                  {option.label}
                </option>
              ))}
            </select>
          </div>

          <HistoryChart
            points={points}
            fromMillis={fromMillis}
            toMillis={toMillis}
            firstLabel={config.firstLabel}
            {...(
              config.secondLabel !== undefined
                ? {
                    secondLabel:
                      config.secondLabel,
                  }
                : {}
            )}
            unit={config.unit}
          />
        </>
      )}
    </MetricPanel>
  );
}

function HistoricalCharts({
  samples,
  fromMillis,
  toMillis,
}: HistoryDataProps) {
  const utilization = useMemo(
    () => buildUtilizationHistory(samples),
    [samples],
  );

  return (
    <div className="space-y-6">
      <MetricPanel
        title="CPU and memory utilization"
        description="Measured system-wide resource utilization over time."
      >
        <HistoryChart
          points={utilization}
          fromMillis={fromMillis}
          toMillis={toMillis}
          firstLabel="CPU"
          secondLabel="Memory"
          unit="percent"
        />
      </MetricPanel>

      <ResourceHistoryPanel
        kind="filesystem"
        samples={samples}
        fromMillis={fromMillis}
        toMillis={toMillis}
      />

      <div className="grid min-w-0 items-start gap-6 2xl:grid-cols-2">
        <ResourceHistoryPanel
          kind="disk"
          samples={samples}
          fromMillis={fromMillis}
          toMillis={toMillis}
        />

        <ResourceHistoryPanel
          kind="network"
          samples={samples}
          fromMillis={fromMillis}
          toMillis={toMillis}
        />
      </div>
    </div>
  );
}

export function HistoricalPerformance({
  deviceID,
}: {
  deviceID: DeviceID;
}) {
  const [durationMs, setDurationMs] =
    useState<number>(
      DEFAULT_HISTORY_DURATION,
    );

  const history = usePerformanceHistory(
    deviceID,
    durationMs,
  );

  const deviceRemoved =
    history.isError &&
    history.error instanceof APIError &&
    history.error.kind === "http" &&
    history.error.status === 404 &&
    history.error.serverMessage ===
      "device not found";

  const data = deviceRemoved
    ? undefined
    : history.data;

  const refreshFailed =
    history.isError || history.isPaused;

  return (
    <section
      id="historical-performance"
      aria-labelledby="historical-heading"
      className="mt-10 min-w-0 border-t border-rack-border pt-8"
    >
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2
            id="historical-heading"
            className="text-xl font-semibold"
          >
            Historical performance
          </h2>

          <p className="mt-2 text-sm leading-relaxed text-rack-muted">
            Resource utilization and activity
            across previously stored measurements.
          </p>
        </div>

        <div className="flex flex-wrap items-end gap-3">
          <div>
            <label
              htmlFor="history-range"
              className="mb-2 block text-xs font-medium text-rack-muted"
            >
              Time range
            </label>

            <select
              id="history-range"
              value={durationMs}
              onChange={(event) => {
                const duration = Number(
                  event.target.value,
                );

                if (
                  HISTORY_RANGES.some(
                    (range) =>
                      range.durationMs ===
                        duration,
                  )
                ) {
                  setDurationMs(duration);
                }
              }}
              className="h-11 rounded-lg border border-rack-border bg-rack-surface px-3 text-sm"
            >
              {HISTORY_RANGES.map((range) => (
                <option
                  key={range.durationMs}
                  value={range.durationMs}
                >
                  {range.label}
                </option>
              ))}
            </select>
          </div>

          <button
            type="button"
            disabled={
              history.isFetching ||
              history.isPaused
            }
            onClick={() => {
              void history.refetch();
            }}
            className="inline-flex min-h-11 items-center gap-2 rounded-lg border border-rack-border bg-rack-surface px-4 py-2 text-sm text-rack-muted hover:text-rack-primary disabled:cursor-not-allowed disabled:opacity-50"
          >
            <RefreshCw
              size={16}
              aria-hidden="true"
              className={
                history.isFetching
                  ? "motion-safe:animate-spin"
                  : ""
              }
            />

            {history.isFetching
              ? "Refreshing..."
              : "Refresh history"}
          </button>
        </div>
      </div>

      {refreshFailed && data && (
        <div
          role="alert"
          className="mb-6 rounded-lg border border-rack-warning/30 bg-rack-warning/5 p-4 text-sm text-rack-warning"
        >
          {history.isPaused
            ? "Historical refresh is paused. Showing the last successfully retrieved range."
            : "Unable to refresh historical performance. Showing the last successfully retrieved range."}
        </div>
      )}

      {data?.hasMore && (
        <div
          role="status"
          className="mb-6 rounded-lg border border-rack-warning/30 bg-rack-warning/5 p-4 text-sm leading-relaxed text-rack-warning"
        >
          Additional historical samples exist within
          this time range. Only the most recent
          retrieved portion is displayed because
          the request limit was reached. Select a
          shorter time range for more complete coverage.
        </div>
      )}

      {deviceRemoved ? (
        <p
          role="alert"
          className="rounded-xl border border-rack-border bg-rack-surface p-6 text-sm text-rack-warning"
        >
          Historical measurements are unavailable
          because this device is no longer registered.
        </p>
      ) : data ? (
        <>
          <div className="mb-5 flex flex-wrap gap-x-6 gap-y-2 text-xs text-rack-muted">
            <span>
              {data.samples.length.toLocaleString()}
              {" samples retrieved"}
            </span>

            <span>
              {data.pagesFetched}
              {" pages retrieved"}
            </span>

            <span>
              Requested range:{" "}
              {formatDeviceTimestamp(
                new Date(
                  data.fromMillis,
                ).toISOString(),
              )}
              {" – "}
              {formatDeviceTimestamp(
                new Date(
                  data.toMillis,
                ).toISOString(),
              )}
            </span>
          </div>

          {data.samples.length === 0 ? (
            <div className="rounded-xl border border-rack-border bg-rack-surface p-12 text-center">
              <h3 className="font-medium">
                No historical samples
              </h3>

              <p className="mt-2 text-sm text-rack-muted">
                No stored measurements match the
                selected time range.
              </p>
            </div>
          ) : (
            <HistoricalCharts
              samples={data.samples}
              fromMillis={data.fromMillis}
              toMillis={data.toMillis}
            />
          )}
        </>
      ) : history.isPaused ? (
        <p
          role="status"
          className="rounded-xl border border-rack-border bg-rack-surface p-6 text-sm text-rack-warning"
        >
          Historical retrieval is paused.
        </p>
      ) : history.isError ? (
        <div
          role="alert"
          className="rounded-xl border border-rack-border bg-rack-surface p-8 text-center"
        >
          <CircleAlert
            size={32}
            aria-hidden="true"
            className="mx-auto mb-4 text-rack-danger"
          />

          <p className="font-medium">
            Historical performance unavailable
          </p>

          <p className="mt-2 text-sm text-rack-muted">
            Unable to retrieve the selected range
            from the appliance.
          </p>
        </div>
      ) : (
        <div
          role="status"
          className="rounded-xl border border-rack-border bg-rack-surface p-8 text-center"
        >
          <RefreshCw
            size={32}
            aria-hidden="true"
            className="mx-auto mb-4 text-rack-muted motion-safe:animate-spin"
          />

          <p className="text-sm text-rack-muted">
            Loading historical performance...
          </p>
        </div>
      )}

      <footer className="mt-6 space-y-2 text-xs leading-relaxed text-rack-muted">
        <p>
          History refreshes every 60 seconds while
          the console is active. Chart times use
          your browser's local timezone.
        </p>

        <p>
          Gaps indicate unavailable measurements or
          interruptions in reporting.
        </p>

        {history.dataUpdatedAt > 0 && (
          <p>
            Last successful historical retrieval:{" "}
            {formatDeviceTimestamp(
              new Date(
                history.dataUpdatedAt,
              ).toISOString(),
            )}
          </p>
        )}
      </footer>
    </section>
  );
}