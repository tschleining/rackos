
/**
 * Unsigned 64-bit integer encoded as a decimal string.
 *
 * Preserve the original string or use BigInt when
 * exact arithmetic is required.
 */
export type UInt64String = string;

/**
 * Signed 64-bit integer encoded as a decimal string.
 */
export type Int64String = string;

/**
 * RFC 3339 timestamp returned by the appliance.
 *
 * Original timestamp precision must be preserved.
 */
export type RFC3339Timestamp = string;

/**
 * Canonical registered device identity.
 */
export type DeviceID = string;

/**
 * Device-scoped historical performance sample identity.
 */
export type SampleID = string;

/**
 * Opaque historical continuation token.
 *
 * Clients must not construct or modify cursors.
 */
export type HistoryCursor = string;

/**
 * Standard management API JSON error response.
 *
 * Transport and routing errors may instead return
 * plain text or another response format.
 */
export interface APIErrorResponse {
  error: string;
}
