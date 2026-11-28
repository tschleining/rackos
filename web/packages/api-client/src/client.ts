import type {
  AdministratorSession,
  ApplianceHealthResponse,
  ApplianceReadinessResponse,
  ApplianceVersionResponse,
  Device,
  DeviceID,
  DeviceListResponse,
  DeviceSummaryResponse,
  HardwareInventory,
  HistoryCursor,
  LoginCredentials,
  PerformanceHistoryQuery,
  PerformanceHistoryResponse,
  PerformanceRecord,
} from "./index";

import { APIError } from "./errors";

import {
  isAdministratorSession,
  isDevice,
  isDeviceID,
  isDeviceList,
  isDeviceSummary,
  isErrorResponse,
  isHardwareInventory,
  isHealthResponse,
  isHistoryCursor,
  isPerformanceHistory,
  isPerformanceRecord,
  isReadinessResponse,
  isVersionResponse,
  timestampMillis,
} from "./validation";

const DEFAULT_TIMEOUT_MS = 10000;
const DEFAULT_HISTORY_LIMIT = 10;
const MAX_HISTORY_LIMIT = 25;
const DEFAULT_HISTORY_PAGES = 4;
const MAX_HISTORY_PAGES = 10;

export interface APIClientOptions {
  fetch?: typeof fetch;
  timeoutMs?: number;

  /** Include browser session cookies in ordinary management requests. */
  authenticatedManagement?: boolean;

  /** Capture a session-scoped observer when a protected request begins. */
  captureManagementUnauthorized?: () => (() => void) | undefined;
}

export interface RequestOptions {
  signal?: AbortSignal;
}

export interface HistoryRangeOptions extends RequestOptions {
  maxPages?: number;
}

export interface PerformanceHistoryRange {
  samples: PerformanceRecord[];
  pagesFetched: number;
  hasMore: boolean;
  nextCursor: HistoryCursor | null;
}

type ResponseGuard<T> = (value: unknown, status: number) => value is T;

interface RequestPolicy {
  method?: "GET" | "POST";
  authentication?: boolean;
  body?: string;
}

function parseRetryAfter(value: string | null): number | null {
  if (value === null || !/^[0-9]+$/.test(value)) {
    return null;
  }

  const seconds = Number(value);

  if (!Number.isSafeInteger(seconds) || seconds < 0) {
    return null;
  }

  return seconds;
}

function invalidRequest(message: string): APIError {
  return new APIError("invalid-request", message);
}

function validateDeviceID(deviceID: DeviceID): void {
  if (!isDeviceID(deviceID)) {
    throw invalidRequest("Invalid device ID");
  }
}

function devicePath(deviceID: DeviceID): string {
  validateDeviceID(deviceID);
  return `/api/v1/devices/${deviceID}`;
}

function buildHistoryQuery(query: PerformanceHistoryQuery): string {
  const parameters = new URLSearchParams();
  let fromMillis: number | null = null;
  let toMillis: number | null = null;

  if (query.from !== undefined) {
    fromMillis = timestampMillis(query.from, true);
    if (fromMillis === null) {
      throw invalidRequest("Invalid history lower time bound");
    }
    parameters.set("from", query.from);
  }

  if (query.to !== undefined) {
    toMillis = timestampMillis(query.to, true);
    if (toMillis === null) {
      throw invalidRequest("Invalid history upper time bound");
    }
    parameters.set("to", query.to);
  }

  if (fromMillis !== null && toMillis !== null && fromMillis >= toMillis) {
    throw invalidRequest("History lower bound must precede upper bound");
  }

  if (query.limit !== undefined) {
    if (!Number.isInteger(query.limit) || query.limit < 1 || query.limit > MAX_HISTORY_LIMIT) {
      throw invalidRequest("History page limit must be between 1 and 25");
    }
    parameters.set("limit", String(query.limit));
  }

  if (query.cursor !== undefined) {
    if (!isHistoryCursor(query.cursor)) {
      throw invalidRequest("Invalid history cursor");
    }
    parameters.set("cursor", query.cursor);
  }

  const encoded = parameters.toString();
  return encoded ? `?${encoded}` : "";
}

function isMissingMeasurement(error: unknown, expectedMessage: string): boolean {
  return (
    error instanceof APIError &&
    error.kind === "http" &&
    error.status === 404 &&
    error.serverMessage === expectedMessage
  );
}

function isPublicManagementPath(path: string): boolean {
  return (
    path === "/healthz" ||
    path === "/readyz" ||
    path === "/api/v1/version"
  );
}

/** Authentication observers must not replace the original request error. */
function notifyManagementUnauthorized(notify: (() => void) | undefined): void {
  try {
    notify?.();
  } catch {
    // Preserve the original HTTP failure.
  }
}

/**
 * Same-origin RackOS management and administrator authentication API client.
 * Authentication tokens are managed exclusively by the browser's HttpOnly
 * cookie mechanism.
 */
export class RackOSAPIClient {
  private readonly fetcher: typeof fetch;
  private readonly timeoutMs: number;
  private readonly authenticatedManagement: boolean;
  private readonly captureManagementUnauthorized: APIClientOptions["captureManagementUnauthorized"];

  constructor(options: APIClientOptions = {}) {
    const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;

    if (!Number.isInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > 60000) {
      throw invalidRequest("Request timeout must be between 1 and 60000 milliseconds");
    }

    this.timeoutMs = timeoutMs;
    this.authenticatedManagement = options.authenticatedManagement === true;
    this.captureManagementUnauthorized = options.captureManagementUnauthorized;
    this.fetcher = options.fetch ?? globalThis.fetch.bind(globalThis);
  }

  /**
   * Execute and validate a same-origin request. Cancellation and timeout
   * protection remain active until the response body is consumed.
   * Authentication mutations are never automatically retried.
   */
  private async request<T>(
    path: string,
    validate: ResponseGuard<T>,
    options: RequestOptions = {},
    acceptedStatuses: readonly number[] = [200],
    policy: RequestPolicy = {},
  ): Promise<T> {
    const callerSignal = options.signal;

    if (callerSignal?.aborted) {
      throw new APIError("cancelled", "Request cancelled");
    }

    const authenticated = policy.authentication === true;
    const includeCredentials = authenticated || this.authenticatedManagement;

    // Do not transmit credentials through a plaintext browser origin.
    // The backend independently enforces HTTPS.
    if (
      includeCredentials &&
      typeof window !== "undefined" &&
      window.location.protocol !== "https:"
    ) {
      throw invalidRequest("Administrator authentication requires HTTPS");
    }

    let notifyUnauthorized: (() => void) | undefined;

    if (this.authenticatedManagement && !authenticated && !isPublicManagementPath(path)) {
      try {
        notifyUnauthorized = this.captureManagementUnauthorized?.();
      } catch {
        // Observer failures must not interfere with management requests.
      }
    }

    const method = policy.method ?? "GET";
    const headers: Record<string, string> = { Accept: "application/json" };

    if (authenticated && method === "POST") {
      headers["X-RackOS-CSRF"] = "1";
    }

    if (policy.body !== undefined) {
      headers["Content-Type"] = "application/json";
    }

    const controller = new AbortController();
    const cancelRequest = () => controller.abort("cancelled");

    callerSignal?.addEventListener("abort", cancelRequest, { once: true });

    const timeout = setTimeout(() => controller.abort("timeout"), this.timeoutMs);

    try {
      const response = await this.fetcher(path, {
        method,
        headers,
        ...(policy.body !== undefined ? { body: policy.body } : {}),
        mode: "same-origin",
        credentials: includeCredentials ? "same-origin" : "omit",
        cache: "no-store",
        redirect: "error",
        signal: controller.signal,
      });

      const contentType = response.headers
        .get("Content-Type")
        ?.split(";")[0]
        ?.trim()
        .toLowerCase();

      const body = await response.text();
      controller.signal.throwIfAborted();

      let data: unknown;

      if (response.status === 204) {
        data = body === "" ? undefined : body;
      } else if (contentType === "application/json") {
        try {
          data = JSON.parse(body);
        } catch {
          // Malformed JSON is handled by response validation.
        }
      }

      if (!acceptedStatuses.includes(response.status)) {
        if (response.status === 401) {
          notifyManagementUnauthorized(notifyUnauthorized);
        }

        // Authentication errors must not retain arbitrary server details.
        const serverMessage = !authenticated && isErrorResponse(data)
          ? data.error
          : null;

        const retryAfterSeconds = response.status === 429
          ? parseRetryAfter(response.headers.get("Retry-After"))
          : null;

        throw new APIError(
          "http",
          authenticated
            ? `Authentication request failed (HTTP ${response.status})`
            : `Management request failed (HTTP ${response.status})`,
          response.status,
          serverMessage,
          retryAfterSeconds,
        );
      }

      if (!validate(data, response.status)) {
        throw new APIError(
          "invalid-response",
          authenticated ? "Invalid authentication API response" : "Invalid management API response",
          response.status,
        );
      }

      return data;
    } catch (error) {
      if (controller.signal.aborted) {
        if (controller.signal.reason === "timeout") {
          throw new APIError(
            "timeout",
            authenticated ? "Authentication request timed out" : "Management request timed out",
          );
        }
        throw new APIError("cancelled", "Request cancelled");
      }

      if (error instanceof APIError) {
        throw error;
      }

      throw new APIError("network", "Unable to communicate with the RackOS appliance");
    } finally {
      clearTimeout(timeout);
      callerSignal?.removeEventListener("abort", cancelRequest);
    }
  }

  // Administrator authentication

  login(credentials: LoginCredentials, options: RequestOptions = {}): Promise<AdministratorSession> {
    return this.request("/api/v1/auth/login", isAdministratorSession, options, [200], {
      method: "POST",
      authentication: true,
      body: JSON.stringify({ username: credentials.username, password: credentials.password }),
    });
  }

  async logout(options: RequestOptions = {}): Promise<void> {
    await this.request("/api/v1/auth/logout", (value): value is undefined => value === undefined, options, [204], {
      method: "POST",
      authentication: true,
    });
  }

  getSession(options: RequestOptions = {}): Promise<AdministratorSession> {
    return this.request("/api/v1/auth/session", isAdministratorSession, options, [200], {
      authentication: true,
    });
  }

  recordActivity(options: RequestOptions = {}): Promise<AdministratorSession> {
    return this.request("/api/v1/auth/activity", isAdministratorSession, options, [200], {
      method: "POST",
      authentication: true,
    });
  }

  // Appliance information

  getHealth(options: RequestOptions = {}): Promise<ApplianceHealthResponse> {
    return this.request("/healthz", isHealthResponse, options);
  }

  getReadiness(options: RequestOptions = {}): Promise<ApplianceReadinessResponse> {
    return this.request(
      "/readyz",
      (value, status): value is ApplianceReadinessResponse =>
        isReadinessResponse(value) &&
        ((status === 200 && value.status === "ready") ||
          (status === 503 && value.status === "not_ready")),
      options,
      [200, 503],
    );
  }

  getVersion(options: RequestOptions = {}): Promise<ApplianceVersionResponse> {
    return this.request("/api/v1/version", isVersionResponse, options);
  }

  // Registered devices

  getDevices(options: RequestOptions = {}): Promise<DeviceListResponse> {
    return this.request("/api/v1/devices", isDeviceList, options);
  }

  getDeviceSummary(options: RequestOptions = {}): Promise<DeviceSummaryResponse> {
    return this.request("/api/v1/devices/summary", isDeviceSummary, options);
  }

  async getDevice(deviceID: DeviceID, options: RequestOptions = {}): Promise<Device> {
    return this.request(
      devicePath(deviceID),
      (value): value is Device => isDevice(value) && value.id === deviceID,
      options,
    );
  }

  // Hardware inventory

  async getInventory(deviceID: DeviceID, options: RequestOptions = {}): Promise<HardwareInventory | null> {
    try {
      return await this.request(
        `${devicePath(deviceID)}/inventory`,
        (value): value is HardwareInventory => isHardwareInventory(value) && value.device_id === deviceID,
        options,
      );
    } catch (error) {
      if (isMissingMeasurement(error, "no inventory snapshot found")) {
        return null;
      }
      throw error;
    }
  }

  // Latest performance

  async getLatestPerformance(deviceID: DeviceID, options: RequestOptions = {}): Promise<PerformanceRecord | null> {
    try {
      return await this.request(
        `${devicePath(deviceID)}/performance/latest`,
        (value): value is PerformanceRecord => isPerformanceRecord(value) && value.device_id === deviceID,
        options,
      );
    } catch (error) {
      if (isMissingMeasurement(error, "no performance samples found")) {
        return null;
      }
      throw error;
    }
  }

  // Single historical page

  async getPerformanceHistory(
    deviceID: DeviceID,
    query: PerformanceHistoryQuery = {},
    options: RequestOptions = {},
  ): Promise<PerformanceHistoryResponse> {
    const path = `${devicePath(deviceID)}/performance/history`;
    const queryString = buildHistoryQuery(query);
    const limit = query.limit ?? DEFAULT_HISTORY_LIMIT;

    return this.request(
      path + queryString,
      (value): value is PerformanceHistoryResponse =>
        isPerformanceHistory(value) && value.device_id === deviceID && value.count <= limit,
      options,
    );
  }

  /**
   * Retrieve a bounded number of historical pages. Preserve API ordering and
   * deduplicate samples without converting timestamps or counters.
   */
  async getPerformanceHistoryRange(
    deviceID: DeviceID,
    query: Omit<PerformanceHistoryQuery, "cursor"> = {},
    options: HistoryRangeOptions = {},
  ): Promise<PerformanceHistoryRange> {
    validateDeviceID(deviceID);

    const maxPages = options.maxPages ?? DEFAULT_HISTORY_PAGES;

    if (!Number.isInteger(maxPages) || maxPages < 1 || maxPages > MAX_HISTORY_PAGES) {
      throw invalidRequest("History page budget must be between 1 and 10");
    }

    const baseQuery: PerformanceHistoryQuery = { limit: query.limit ?? MAX_HISTORY_LIMIT };

    if (query.from !== undefined) {
      baseQuery.from = query.from;
    }
    if (query.to !== undefined) {
      baseQuery.to = query.to;
    }

    const samples: PerformanceRecord[] = [];
    const seenSamples = new Set<string>();
    const seenCursors = new Set<string>();
    let pagesFetched = 0;
    let nextCursor: HistoryCursor | null = null;

    for (let pageNumber = 0; pageNumber < maxPages; pageNumber++) {
      const pageQuery: PerformanceHistoryQuery = { ...baseQuery };

      if (nextCursor !== null) {
        pageQuery.cursor = nextCursor;
      }

      const page = await this.getPerformanceHistory(deviceID, pageQuery, options);
      pagesFetched++;

      for (const sample of page.samples) {
        if (!seenSamples.has(sample.sample_id)) {
          seenSamples.add(sample.sample_id);
          samples.push(sample);
        }
      }

      nextCursor = page.next_cursor;
      if (nextCursor === null) {
        break;
      }

      if (seenCursors.has(nextCursor)) {
        throw new APIError("invalid-response", "Repeated historical continuation cursor");
      }
      seenCursors.add(nextCursor);
    }

    return { samples, pagesFetched, hasMore: nextCursor !== null, nextCursor };
  }
}
