import {
  APIError,
} from "@rackos/api-client";

export interface LoginFailure {
  message: string;
  retryAfterSeconds: number;
}

export function describeLoginFailure(
  error: unknown,
): LoginFailure {
  const failure = (
    message: string,
    retryAfterSeconds = 0,
  ): LoginFailure => ({
    message,
    retryAfterSeconds,
  });

  if (!(error instanceof APIError)) {
    return failure(
      "Unable to sign in. Please try again.",
    );
  }

  if (error.kind === "http") {
    switch (error.status) {
      case 400:
      case 413:
      case 415:
        return failure(
          "The sign-in request is invalid. Check your credentials.",
        );

      case 401:
        return failure(
          "Incorrect username or password.",
        );

      case 403:
        return failure(
          "Sign-in was blocked by appliance security policy.",
        );

      case 409:
        return failure(
          "The maximum number of administrator sessions has been reached.",
        );

      case 429: {
        const seconds =
          error.retryAfterSeconds;

        const retryAfterSeconds =
          seconds !== null &&
          Number.isSafeInteger(seconds) &&
          seconds >= 0
            ? Math.max(
                1,
                Math.min(seconds, 60),
              )
            : 60;

        return failure(
          "Too many sign-in attempts. Please wait before trying again.",
          retryAfterSeconds,
        );
      }

      case 502:
      case 503:
      case 504:
        return failure(
          "The authentication service is temporarily unavailable.",
        );

      default:
        return failure(
          "The appliance was unable to complete sign-in.",
        );
    }
  }

  switch (error.kind) {
    case "network":
      return failure(
        "Unable to connect to the RackOS appliance.",
      );

    case "timeout":
      return failure(
        "The sign-in request timed out.",
      );

    case "cancelled":
      return failure(
        "The sign-in request was cancelled.",
      );

    case "invalid-request":
      return failure(
        "Unable to submit the sign-in request.",
      );

    case "invalid-response":
      return failure(
        "The appliance returned an unexpected authentication response.",
      );
  }
}