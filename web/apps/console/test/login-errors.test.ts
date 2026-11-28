import {
  describe,
  expect,
  it,
} from "vitest";

import {
  APIError,
} from "@rackos/api-client";

import {
  describeLoginFailure,
} from "../src/features/auth/loginErrors";

describe("Login error handling", () => {
  it("returns a generic credential failure", () => {
    const error = new APIError(
      "http",
      "Private diagnostic message",
      401,
      "Administrator does not exist",
    );

    expect(
      describeLoginFailure(error),
    ).toEqual({
      message:
        "Incorrect username or password.",

      retryAfterSeconds: 0,
    });
  });

  it("preserves valid rate-limit intervals", () => {
    const error = new APIError(
      "http",
      "Rate limited",
      429,
      null,
      42,
    );

    expect(
      describeLoginFailure(error),
    ).toMatchObject({
      retryAfterSeconds: 42,
    });
  });

  it.each([
    null,
    -1,
    1.5,
    Number.NaN,
    Number.POSITIVE_INFINITY,
  ])(
    "uses a safe fallback for retry interval %s",
    (seconds) => {
      const error = new APIError(
        "http",
        "Rate limited",
        429,
        null,
        seconds,
      );

      expect(
        describeLoginFailure(error),
      ).toMatchObject({
        retryAfterSeconds: 60,
      });
    },
  );

  it("bounds excessively large retry intervals", () => {
    const error = new APIError(
      "http",
      "Rate limited",
      429,
      null,
      999999,
    );

    expect(
      describeLoginFailure(error),
    ).toMatchObject({
      retryAfterSeconds: 60,
    });
  });

  it("uses a minimum positive retry interval", () => {
    const error = new APIError(
      "http",
      "Rate limited",
      429,
      null,
      0,
    );

    expect(
      describeLoginFailure(error),
    ).toMatchObject({
      retryAfterSeconds: 1,
    });
  });

  it("distinguishes session capacity failures", () => {
    const error = new APIError(
      "http",
      "Session limit",
      409,
    );

    expect(
      describeLoginFailure(error),
    ).toEqual({
      message:
        "The maximum number of administrator sessions has been reached.",

      retryAfterSeconds: 0,
    });
  });

  it("distinguishes infrastructure failures", () => {
    const error = new APIError(
      "http",
      "Private database error",
      503,
    );

    expect(
      describeLoginFailure(error),
    ).toEqual({
      message:
        "The authentication service is temporarily unavailable.",

      retryAfterSeconds: 0,
    });
  });

  it("does not expose unexpected error details", () => {
    const failure = describeLoginFailure(
      new Error(
        "private-authentication-detail",
      ),
    );

    expect(failure).toEqual({
      message:
        "Unable to sign in. Please try again.",

      retryAfterSeconds: 0,
    });
  });
});