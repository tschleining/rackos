
/**
 * Successful appliance liveness response.
 *
 * GET /healthz
 */
export interface ApplianceHealthResponse {
  status: "healthy";
}

/**
 * Appliance readiness response.
 *
 * GET /readyz
 *
 * ready: HTTP 200
 * not_ready: HTTP 503
 */
export interface ApplianceReadinessResponse {
  status: "ready" | "not_ready";
}

/**
 * Appliance software identification.
 *
 * GET /api/v1/version
 */
export interface ApplianceVersionResponse {
  name: string;
  version: string;
}
