import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import {
  formatByteRate,
  formatPercentage,
} from "../../lib/format";

import type {
  HistoryPoint,
} from "./history";

interface HistoryChartProps {
  points: HistoryPoint[];

  fromMillis: number;
  toMillis: number;

  firstLabel: string;
  secondLabel?: string;

  unit: "percent" | "bytes-per-second";
}

const axisDateFormatter =
  new Intl.DateTimeFormat(
    undefined,
    {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    },
  );

const tooltipDateFormatter =
  new Intl.DateTimeFormat(
    undefined,
    {
      dateStyle: "medium",
      timeStyle: "medium",
    },
  );

function formatMeasurement(
  value: number,
  unit: HistoryChartProps["unit"],
): string {
  return unit === "percent"
    ? formatPercentage(value)
    : formatByteRate(value);
}

export function HistoryChart({
  points,
  fromMillis,
  toMillis,
  firstLabel,
  secondLabel,
  unit,
}: HistoryChartProps) {
  const hasMeasurements = points.some(
    (point) =>
      point.first !== null ||
      (
        secondLabel !== undefined &&
        point.second !== null
      ),
  );

  if (!hasMeasurements) {
    return (
      <div className="flex min-h-64 items-center justify-center rounded-lg border border-rack-border bg-rack-background p-6 text-center">
        <p className="text-sm text-rack-muted">
          No measurements available for this
          resource in the retrieved history.
        </p>
      </div>
    );
  }

  return (
    <div className="min-w-0">
      <div className="mb-4 flex flex-wrap gap-4 text-xs">
        <span className="inline-flex items-center gap-2 text-rack-muted">
          <span
            aria-hidden="true"
            className="h-0.5 w-5 bg-rack-accent"
          />

          {firstLabel}
        </span>

        {secondLabel && (
          <span className="inline-flex items-center gap-2 text-rack-muted">
            <span
              aria-hidden="true"
              className="w-5 border-t-2 border-dashed border-rack-success"
            />

            {secondLabel}
          </span>
        )}
      </div>

      <ResponsiveContainer
        width="100%"
        height={280}
        minWidth={0}
      >
        <LineChart
          data={points}
          margin={{
            top: 12,
            right: 16,
            bottom: 12,
            left: 8,
          }}
          accessibilityLayer
        >
          <CartesianGrid
            stroke="#303b4a"
            strokeDasharray="3 4"
            vertical={false}
          />

          <XAxis
            type="number"
            dataKey="timestamp"
            scale="time"
            domain={[fromMillis, toMillis]}
            allowDataOverflow
            minTickGap={32}
            tick={{
              fill: "#94a3b8",
              fontSize: 11,
            }}
            tickFormatter={(value: number) =>
              axisDateFormatter.format(value)
            }
            axisLine={{
              stroke: "#303b4a",
            }}
            tickLine={false}
          />

          <YAxis
            domain={
              unit === "percent"
                ? [0, 100]
                : [0, "auto"]
            }
            width={78}
            tick={{
              fill: "#94a3b8",
              fontSize: 11,
            }}
            tickFormatter={(value: number) =>
              formatMeasurement(value, unit)
            }
            axisLine={false}
            tickLine={false}
          />

          <Tooltip
            filterNull
            isAnimationActive={false}
            contentStyle={{
              backgroundColor: "#1a222e",
              borderColor: "#303b4a",
              borderRadius: "8px",
              color: "#f1f5f9",
            }}
            labelStyle={{
              color: "#94a3b8",
            }}
            labelFormatter={(label) => {
              const timestamp = Number(label);

              return Number.isFinite(timestamp)
                ? tooltipDateFormatter.format(
                    timestamp,
                  )
                : "Unknown time";
            }}
            formatter={(value) =>
              typeof value === "number"
                ? formatMeasurement(value, unit)
                : "Unavailable"
            }
          />

          <Line
            type="linear"
            dataKey="first"
            name={firstLabel}
            stroke="#60a5fa"
            strokeWidth={2}
            dot={{ r: 2 }}
            activeDot={{ r: 4 }}
            connectNulls={false}
            isAnimationActive={false}
          />

          {secondLabel && (
            <Line
              type="linear"
              dataKey="second"
              name={secondLabel}
              stroke="#4ade80"
              strokeWidth={2}
              strokeDasharray="5 3"
              dot={{ r: 2 }}
              activeDot={{ r: 4 }}
              connectNulls={false}
              isAnimationActive={false}
            />
          )}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}