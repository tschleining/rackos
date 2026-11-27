import { APIError } from "@rackos/api-client";

export function isDeviceNotFound(
  error: unknown,
): boolean {
  return (
    error instanceof APIError &&
    error.kind === "http" &&
    error.status === 404 &&
    error.serverMessage === "device not found"
  );
}

export function describeAPIError(
  error: unknown,
): string {
  if (!(error instanceof APIError)) {
    return "An unexpected request error occurred.";
  }

  switch (error.kind) {
    case "network":
      return "Unable to connect to the RackOS appliance.";

    case "timeout":
      return "The appliance did not respond before the request timed out.";

    case "cancelled":
      return "The request was cancelled.";

    case "invalid-request":
      return "The console attempted to make an invalid request.";

    case "invalid-response":
      return "The appliance returned an unexpected response. Verify console and appliance compatibility.";

    case "http":
      switch (error.status) {
        case 400:
          return "The appliance rejected the request.";

        case 401:
        case 403:
          return "Management access was denied.";

        case 404:
          return isDeviceNotFound(error)
            ? "The requested device is no longer registered."
            : "The requested resource was not found.";

        case 429:
          return "The appliance is receiving too many requests.";

        case 502:
        case 503:
        case 504:
          return "The management service is temporarily unavailable.";

        default:
          return "The management service returned an unsuccessful response.";
      }
  }
}