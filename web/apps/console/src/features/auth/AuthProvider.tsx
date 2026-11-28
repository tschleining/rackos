import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PropsWithChildren,
} from "react";

import {
  APIError,
  type AdministratorSession,
  type LoginCredentials,
} from "@rackos/api-client";

import {
  AuthContext,
} from "./AuthContext";

import type {
  AuthClient,
  AuthContextValue,
  AuthState,
} from "./types";

import {
  openSessionChannel,
  type SessionChannel,
} from "./sessionChannel";

const ACTIVITY_INTERVAL_MS = 60_000;

interface AuthProviderProps
  extends PropsWithChildren {
  client: AuthClient;
}

interface AuthOperation {
  controller: AbortController;
}

const initialState: AuthState = {
  status: "checking",
};

function isUnauthorized(
  error: unknown,
): boolean {
  return (
    error instanceof APIError &&
    error.kind === "http" &&
    error.status === 401
  );
}

function isRejectedLogin(
  error: unknown,
): boolean {
  if (!(error instanceof APIError)) {
    return false;
  }

  if (error.kind === "invalid-request") {
    return true;
  }

  if (
    error.kind !== "http" ||
    error.status === null
  ) {
    return false;
  }

  return [
    400,
    401,
    403,
    409,
    413,
    415,
    429,
  ].includes(error.status);
}

function cancelledOperation(): APIError {
  return new APIError(
    "cancelled",
    "Authentication operation cancelled",
  );
}

function operationUnavailable(): APIError {
  return new APIError(
    "invalid-request",
    "Authentication operation is unavailable",
  );
}

function normalizeAuthError(
  error: unknown,
): APIError {
  if (error instanceof APIError) {
    return new APIError(
      error.kind,
      "Authentication operation failed",
      error.status,
      null,
      error.retryAfterSeconds,
    );
  }

  return new APIError(
    "network",
    "Authentication operation failed",
  );
}

/**
 * Activity renewal must preserve the
 * existing public session identity and
 * absolute lifetime.
 */
function isValidActivityResponse(
  previous: AdministratorSession,
  current: AdministratorSession,
): boolean {
  return (
    current.administrator_id ===
      previous.administrator_id &&

    current.created_at ===
      previous.created_at &&

    current.expires_at ===
      previous.expires_at &&

    Date.parse(current.last_activity_at) >=
      Date.parse(previous.last_activity_at) &&

    Date.parse(current.idle_expires_at) >=
      Date.parse(previous.idle_expires_at)
  );
}

export function AuthProvider({
  client,
  children,
}: AuthProviderProps) {
  const [state, setState] =
    useState<AuthState>(initialState);

  const [
    sessionGeneration,
    setSessionGeneration,
  ] = useState(0);

  const stateRef =
    useRef<AuthState>(initialState);

  const generation = useRef(0);

  const activeOperation =
    useRef<AuthOperation | null>(null);

  const activeActivity =
    useRef<AuthOperation | null>(null);

  const lastActivityAttempt =
    useRef(Number.NEGATIVE_INFINITY);

  const sessionChannel =
    useRef<SessionChannel | null>(null);

  const peerVerificationPending =
    useRef(false);

  const mounted = useRef(false);

  const publish = useCallback(
    (next: AuthState) => {
      if (
        stateRef.current.status !==
        next.status
      ) {
        generation.current++;
      }

      stateRef.current = next;

      setSessionGeneration(
        generation.current,
      );

      setState(next);
    },
    [],
  );

  const isCurrent = useCallback(
    (operation: AuthOperation): boolean =>
      mounted.current &&
      activeOperation.current === operation &&
      !operation.controller.signal.aborted,
    [],
  );

  /**
   * Relinquish activity ownership before
   * aborting its request.
   */
  const cancelActivity = useCallback(
    () => {
      const operation =
        activeActivity.current;

      activeActivity.current = null;

      operation?.controller.abort();
    },
    [],
  );

  /**
   * Authentication operations have priority
   * over activity renewal.
   */
  const begin = useCallback(
    (
      next: AuthState,
    ): AuthOperation | null => {
      if (
        !mounted.current ||
        activeOperation.current !== null
      ) {
        return null;
      }

      const operation: AuthOperation = {
        controller: new AbortController(),
      };

      activeOperation.current = operation;

      cancelActivity();

      publish(next);

      return operation;
    },
    [cancelActivity, publish],
  );

  const finish = useCallback(
    (operation: AuthOperation): boolean => {
      if (
        activeOperation.current !== operation
      ) {
        return false;
      }

      activeOperation.current = null;

      if (
        !mounted.current ||
        !peerVerificationPending.current
      ) {
        return false;
      }

      peerVerificationPending.current = false;

      return true;
    },
    [],
  );

  /**
   * Read-only inspection of the current
   * browser authentication session.
   */
const restoreSession = useCallback(
  async function restore(): Promise<void> {
    const operation = begin({
      status: "checking",
    });

    if (operation === null) {
      return;
    }

    try {
      const session =
        await client.getSession({
          signal:
            operation.controller.signal,
        });

      if (
        !isCurrent(operation) ||
        peerVerificationPending.current
      ) {
        return;
      }

      publish({
        status: "authenticated",
        session,
      });
    } catch (error) {
      if (
        !isCurrent(operation) ||
        peerVerificationPending.current
      ) {
        return;
      }

      publish({
        status: isUnauthorized(error)
          ? "anonymous"
          : "unavailable",
      });
    } finally {
      if (finish(operation)) {
        // A peer reported a session change
        // while this inspection was running.
        //
        // Do not expose the potentially
        // obsolete result. Inspect again.
        await restore();
      }
    }
  },
  [
    begin,
    client,
    finish,
    isCurrent,
    publish,
  ],
);

  /**
   * Capture a generation-scoped management
   * authentication failure observer.
   */
  const captureManagementUnauthorized =
    useCallback(
      (): (() => void) | undefined => {
        if (
          !mounted.current ||
          stateRef.current.status !==
            "authenticated"
        ) {
          return undefined;
        }

        const capturedGeneration =
          generation.current;

        return () => {
          if (
            !mounted.current ||
            generation.current !==
              capturedGeneration ||
            stateRef.current.status !==
              "authenticated" ||
            activeOperation.current !== null
          ) {
            return;
          }

          void restoreSession();
        };
      },
      [restoreSession],
    );

  /**
   * Authenticate an anonymous administrator.
   *
   * Credentials remain outside React
   * authentication state and query caches.
   */
const login = useCallback(
  async (
    credentials: LoginCredentials,
  ): Promise<void> => {
    if (
      stateRef.current.status !==
      "anonymous"
    ) {
      throw operationUnavailable();
    }

    const operation = begin({
      status: "authenticating",
    });

    if (operation === null) {
      throw operationUnavailable();
    }

    let notifyPeers = false;

    try {
      const session = await client.login(
        credentials,
        {
          signal:
            operation.controller.signal,
        },
      );

      notifyPeers = true;

      if (!isCurrent(operation)) {
        throw cancelledOperation();
      }

      if (!peerVerificationPending.current) {
        publish({
          status: "authenticated",
          session,
        });
      }
    } catch (error) {
      if (!isRejectedLogin(error)) {
        // An uncertain response may still
        // have changed the browser cookie.
        notifyPeers = true;
      }

      if (!isCurrent(operation)) {
        throw cancelledOperation();
      }

      publish({
        status: isRejectedLogin(error)
          ? "anonymous"
          : "unavailable",
      });

      throw normalizeAuthError(error);
    } finally {
      if (notifyPeers) {
        sessionChannel.current?.notify();
      }

      if (finish(operation)) {
        await restoreSession();
      }
    }
  },
  [
    begin,
    client,
    finish,
    isCurrent,
    publish,
    restoreSession,
  ],
);

  /**
   * Revoke the current administrator session.
   *
   * An outstanding activity request never
   * prevents logout from starting.
   */
const logout = useCallback(
  async (): Promise<void> => {
    if (
      stateRef.current.status !==
      "authenticated"
    ) {
      throw operationUnavailable();
    }

    const operation = begin({
      status: "signing-out",
    });

    if (operation === null) {
      throw operationUnavailable();
    }

    try {
      await client.logout({
        signal:
          operation.controller.signal,
      });

      if (!isCurrent(operation)) {
        throw cancelledOperation();
      }

      publish({
        status: "anonymous",
      });
    } catch (error) {
      if (!isCurrent(operation)) {
        throw cancelledOperation();
      }

      if (isUnauthorized(error)) {
        publish({
          status: "anonymous",
        });

        return;
      }

      publish({
        status: "unavailable",
      });

      throw normalizeAuthError(error);
    } finally {
      // Even an uncertain logout may have
      // revoked the session at the appliance.
      sessionChannel.current?.notify();

      if (finish(operation)) {
        await restoreSession();
      }
    }
  },
  [
    begin,
    client,
    finish,
    isCurrent,
    publish,
    restoreSession,
  ],
);

  /**
   * Record qualifying administrator activity.
   *
   * Admission is synchronous and uses a
   * monotonic clock.
   *
   * Rejected activity events are discarded,
   * never queued for later execution.
   */
  const recordActivity = useCallback(
    async (): Promise<void> => {
      const previous = stateRef.current;

      if (
        !mounted.current ||
        previous.status !==
          "authenticated" ||
        activeOperation.current !== null ||
        activeActivity.current !== null
      ) {
        return;
      }

      const now = performance.now();

      if (
        now - lastActivityAttempt.current <
        ACTIVITY_INTERVAL_MS
      ) {
        return;
      }

      const operation: AuthOperation = {
        controller: new AbortController(),
      };

      const capturedGeneration =
        generation.current;

      // Consume the attempt budget before
      // sending the request, including failed
      // or cancelled renewal attempts.
      lastActivityAttempt.current = now;

      activeActivity.current = operation;

      const isCurrentActivity = (): boolean =>
        mounted.current &&
        activeActivity.current === operation &&
        activeOperation.current === null &&
        generation.current ===
          capturedGeneration &&
        stateRef.current.status ===
          "authenticated" &&
        !operation.controller.signal.aborted;

      let requiresVerification = false;

      try {
        const session =
          await client.recordActivity({
            signal:
              operation.controller.signal,
          });

        if (!isCurrentActivity()) {
          return;
        }

        if (
          !isValidActivityResponse(
            previous.session,
            session,
          )
        ) {
          // The browser cookie may have
          // changed in another browser tab.
          requiresVerification = true;
        } else {
          // Updating activity metadata does
          // not create a new authentication
          // generation or management cache.
          publish({
            status: "authenticated",
            session,
          });
        }
      } catch {
        if (isCurrentActivity()) {
          requiresVerification = true;
        }
      } finally {
        if (
          activeActivity.current === operation
        ) {
          activeActivity.current = null;
        }
      }

      if (
        requiresVerification &&
        mounted.current &&
        generation.current ===
          capturedGeneration &&
        stateRef.current.status ===
          "authenticated" &&
        activeOperation.current === null
      ) {
        // Verify the actual session rather
        // than retrying an uncertain mutation.
        await restoreSession();
      }
    },
    [
      client,
      publish,
      restoreSession,
    ],
  );

useEffect(() => {
  mounted.current = true;

  const channel = openSessionChannel(
    () => {
      if (!mounted.current) {
        return;
      }

      if (
        activeOperation.current !== null
      ) {
        // Never interrupt login or logout.
        // Coalesce peer notifications into
        // one subsequent verification.
        peerVerificationPending.current =
          true;

        return;
      }

      void restoreSession();
    },
  );

  sessionChannel.current = channel;

  void restoreSession();

  return () => {
    mounted.current = false;

    generation.current++;

    peerVerificationPending.current = false;

    sessionChannel.current = null;

    channel?.close();

    const operation =
      activeOperation.current;

    activeOperation.current = null;

    cancelActivity();

    operation?.controller.abort();
  };
}, [
  cancelActivity,
  restoreSession,
]);

  const context = useMemo<AuthContextValue>(
    () => ({
      state,
      sessionGeneration,
      restoreSession,
      login,
      logout,
      recordActivity,
      captureManagementUnauthorized,
    }),
    [
      state,
      sessionGeneration,
      restoreSession,
      login,
      logout,
      recordActivity,
      captureManagementUnauthorized,
    ],
  );

  return (
    <AuthContext.Provider value={context}>
      {children}
    </AuthContext.Provider>
  );
}