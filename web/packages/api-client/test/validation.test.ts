
import {
  describe,
  expect,
  it,
} from "vitest";

import {
  isHardwareInventory,
  isPerformanceRecord,
  isUInt64String,
  timestampMillis,
} from "../src/validation";

import {
  minimalInventory,
  performanceRecord,
} from "./contracts.type-test";

describe("64-bit integer validation", () => {
  it("accepts exact unsigned integer strings", () => {
    for (const value of [
      "0",
      "9007199254740993",
      "18446744073709551615",
    ]) {
      expect(
        isUInt64String(value),
      ).toBe(true);
    }
  });

  it("rejects invalid unsigned integer strings", () => {
    for (const value of [
      "",
      "-1",
      "01",
      "1.5",
      "1e3",
      " 100",
      "18446744073709551616",
      100,
      null,
    ]) {
      expect(
        isUInt64String(value),
      ).toBe(false);
    }
  });
});

describe("Timestamp validation", () => {
  it("accepts nanosecond precision responses", () => {
    expect(
      timestampMillis(
        "2026-11-27T12:00:00.123456789Z",
      ),
    ).toBe(
      Date.parse(
        "2026-11-27T12:00:00.123Z",
      ),
    );
  });

  it("accepts equivalent time bounds", () => {
    expect(
      timestampMillis(
        "2026-11-27T07:00:00-05:00",
        true,
      ),
    ).toBe(
      timestampMillis(
        "2026-11-27T12:00:00Z",
        true,
      ),
    );
  });

  it("rejects invalid calendar dates", () => {
    expect(
      timestampMillis(
        "2026-02-30T12:00:00Z",
      ),
    ).toBeNull();
  });

  it("rejects sub-millisecond query bounds", () => {
    expect(
      timestampMillis(
        "2026-11-27T12:00:00.123456Z",
        true,
      ),
    ).toBeNull();
  });
});

describe("Optional measurements", () => {
  it("accepts unavailable inventory measurements", () => {
    expect(
      isHardwareInventory(
        minimalInventory,
      ),
    ).toBe(true);
  });

  it("rejects null inventory memory capacity", () => {
    expect(
      isHardwareInventory({
        ...minimalInventory,
        memory_bytes: null,
      }),
    ).toBe(false);
  });

  it("rejects null performance collections", () => {
    expect(
      isPerformanceRecord({
        ...performanceRecord,

        snapshot: {
          ...performanceRecord.snapshot,

          network: {
            ...performanceRecord.snapshot.network,
            interfaces: null,
          },
        },
      }),
    ).toBe(false);
  });
});
