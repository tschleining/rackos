export type APIErrorKind =
  | "http"
  | "network"
  | "timeout"
  | "cancelled"
  | "invalid-request"
  | "invalid-response";

export class APIError extends Error {
  readonly kind: APIErrorKind;
  readonly status: number | null;
  readonly serverMessage: string | null;

  readonly retryAfterSeconds: number | null;

  constructor(
    kind: APIErrorKind,
    message: string,
    status: number | null = null,
    serverMessage: string | null = null,
    retryAfterSeconds: number | null = null,
  ) {
    super(message);

    this.name = "APIError";
    this.kind = kind;
    this.status = status;
    this.serverMessage = serverMessage;

    this.retryAfterSeconds =
      retryAfterSeconds;
  }
}