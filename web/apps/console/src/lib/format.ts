import type {
  UInt64String,
} from "@rackos/api-client";

const binaryUnits = [
  { label: "EiB", divisor: 1024n ** 6n },
  { label: "PiB", divisor: 1024n ** 5n },
  { label: "TiB", divisor: 1024n ** 4n },
  { label: "GiB", divisor: 1024n ** 3n },
  { label: "MiB", divisor: 1024n ** 2n },
  { label: "KiB", divisor: 1024n },
] as const;

/**
 * Format an already validated unsigned 64-bit
 * decimal string as a binary byte capacity.
 *
 * Integer arithmetic preserves precision before
 * rounding the displayed value to two decimal places.
 */
export function formatBytes(
  value: UInt64String,
): string {
  const bytes = BigInt(value);

  for (const unit of binaryUnits) {
    if (bytes < unit.divisor) {
      continue;
    }

    const hundredths =
      (
        bytes * 100n +
        unit.divisor / 2n
      ) / unit.divisor;

    const whole = hundredths / 100n;

    const fraction = (hundredths % 100n)
      .toString()
      .padStart(2, "0")
      .replace(/0+$/, "");

    const formatted = fraction
      ? `${whole}.${fraction}`
      : whole.toString();

    return `${formatted} ${unit.label}`;
  }

  return `${bytes} B`;
}

/**
 * Format the original byte count without
 * rounding or losing integer precision.
 */
export function formatExactBytes(
  value: UInt64String,
): string {
  return `${BigInt(value).toLocaleString()} bytes`;
}

/**
 * Format a count using the browser's locale.
 */
export function formatCount(
  value: number,
): string {
  return value.toLocaleString();
}

const decimalFormatter = new Intl.NumberFormat(
  undefined,
  {
    maximumFractionDigits: 2,
  },
);

export function formatCounter(
  value: UInt64String,
): string {
  return BigInt(value).toLocaleString();
}

export function formatPercentage(
  value: number,
): string {
  return `${decimalFormatter.format(value)}%`;
}

/**
 * Format a measured rate without presenting
 * a small nonzero measurement as zero.
 */
export function formatRate(
  value: number,
  unit: string,
): string {
  if (value > 0 && value < 0.01) {
    return `<${decimalFormatter.format(0.01)} ${unit}`;
  }

  return `${decimalFormatter.format(value)} ${unit}`;
}

/**
 * Floating-point throughput measurements use
 * binary byte units per second.
 */
export function formatByteRate(
  value: number,
): string {
  const units = [
    "B/s",
    "KiB/s",
    "MiB/s",
    "GiB/s",
    "TiB/s",
    "PiB/s",
    "EiB/s",
  ] as const;

  let scaled = value;
  let index = 0;

  while (
    scaled >= 1024 &&
    index < units.length - 1
  ) {
    scaled /= 1024;
    index++;
  }

  return formatRate(
    scaled,
    units[index] ?? "B/s",
  );
}