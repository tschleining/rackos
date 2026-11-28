// @vitest-environment jsdom
import type {
  PropsWithChildren,
} from "react";

import {
  act,
  cleanup,
  renderHook,
  waitFor,
} from "@testing-library/react";

import {
  afterEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import {
  APIError,
  type AdministratorSession,
} from "@rackos/api-client";

import {
  AuthProvider,
  useAuth,
  type AuthClient,
} from "../src/features/auth";

const session: AdministratorSession = {
  administrator_id:
    "12345678-1234-4000-8000-123456789abc",

  created_at:
    "2026-11-27T12:00:00Z",

  last_activity_at:
    "2026-11-27T12:10:00Z",

  expires_at:
    "2026-11-28T00:00:00Z",

  idle_expires_at:
    "2026-11-27T12:40:00Z",
};

const credentials = {
  username: "admin",
  password: "synthetic-test-password",
};

function httpError(
  status: number,
  retryAfterSeconds: number | null = null,
): APIError {
  return new APIError(
    "http",
    "Synthetic HTTP error",
    status,
    null,
    retryAfterSeconds,
  );
}

function deferred<T>() {
  let resolve!: (
    value: T | PromiseLike<T>,
  ) => void;

  let reject!: (
    reason?: unknown,
  ) => void;

  const promise = new Promise<T>(
    (resolvePromise, rejectPromise) => {
      resolve = resolvePromise;
      reject = rejectPromise;
    },
  );

  return {
    promise,
    resolve,
    reject,
  };
}

function createClient() {
  return {
    getSession:
      vi.fn<AuthClient["getSession"]>(),

    login:
      vi.fn<AuthClient["login"]>(),

    logout:
      vi.fn<AuthClient["logout"]>(),

    recordActivity:
      vi.fn<AuthClient["recordActivity"]>(),
  };
}

function renderAuth(
  client: AuthClient,
  strict = false,
) {
  function Wrapper({
    children,
  }: PropsWithChildren) {
    return (
      <AuthProvider client={client}>
        {children}
      </AuthProvider>
    );
  }

  return renderHook(
    () => useAuth(),
    {
      wrapper: Wrapper,
      reactStrictMode: strict,
    },
  );
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("Authentication session restoration", () => {
  it("starts in the checking state", () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.getSession.mockReturnValue(
      pending.promise,
    );

    const { result } = renderAuth(client);

    expect(result.current.state).toEqual({
      status: "checking",
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();
  });

  it("restores an existing authenticated session", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state,
      ).toEqual({
        status: "authenticated",
        session,
      });
    });

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();

    expect(
      client.login,
    ).not.toHaveBeenCalled();
  });

  it("treats HTTP 401 as anonymous", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(401),
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state,
      ).toEqual({
        status: "anonymous",
      });
    });
  });

  it.each([
    httpError(403),
    httpError(404),
    httpError(503),

    new APIError(
      "network",
      "Network unavailable",
    ),

    new APIError(
      "timeout",
      "Request timed out",
    ),

    new APIError(
      "invalid-response",
      "Invalid session response",
    ),
  ])(
    "fails closed when authentication status is uncertain: %s",
    async (error) => {
      const client = createClient();

      client.getSession.mockRejectedValue(
        error,
      );

      const { result } =
        renderAuth(client);

      await waitFor(() => {
        expect(
          result.current.state,
        ).toEqual({
          status: "unavailable",
        });
      });
    },
  );

  it("allows explicit restoration after an infrastructure failure", async () => {
    const client = createClient();

    client.getSession
      .mockRejectedValueOnce(
        httpError(503),
      )
      .mockResolvedValueOnce(session);

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("unavailable");
    });

    await act(async () => {
      await result.current.restoreSession();
    });

    expect(result.current.state).toEqual({
      status: "authenticated",
      session,
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);
  });

  it("does not start duplicate restoration requests", async () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.getSession.mockReturnValue(
      pending.promise,
    );

    const { result } = renderAuth(client);

    await act(async () => {
      await result.current.restoreSession();
      await result.current.restoreSession();
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    await act(async () => {
      pending.resolve(session);
    });

    expect(
      result.current.state.status,
    ).toBe("authenticated");
  });
});

describe("Administrator login", () => {
  it("transitions from anonymous to authenticated", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(401),
    );

    const pending =
      deferred<AdministratorSession>();

    client.login.mockReturnValue(
      pending.promise,
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("anonymous");
    });

    let operation!: Promise<void>;

    act(() => {
      operation =
        result.current.login(credentials);
    });

    expect(result.current.state).toEqual({
      status: "authenticating",
    });

    expect(
      client.login,
    ).toHaveBeenCalledWith(
      credentials,
      {
        signal: expect.any(AbortSignal),
      },
    );

    await act(async () => {
      pending.resolve(session);
      await operation;
    });

    expect(result.current.state).toEqual({
      status: "authenticated",
      session,
    });

    expect(
      JSON.stringify(result.current.state),
    ).not.toContain(credentials.password);
  });

  it.each([
    400,
    401,
    403,
    409,
    413,
    415,
    429,
  ])(
    "remains anonymous after rejected login HTTP %i",
    async (status) => {
      const client = createClient();

      client.getSession.mockRejectedValue(
        httpError(401),
      );

      client.login.mockRejectedValue(
        httpError(status),
      );

      const { result } =
        renderAuth(client);

      await waitFor(() => {
        expect(
          result.current.state.status,
        ).toBe("anonymous");
      });

      await act(async () => {
        await expect(
          result.current.login(credentials),
        ).rejects.toMatchObject({
          kind: "http",
          status,
        });
      });

      expect(
        result.current.state,
      ).toEqual({
        status: "anonymous",
      });

      expect(
        client.login,
      ).toHaveBeenCalledOnce();
    },
  );

  it("preserves structured rate limiting information", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(401),
    );

    client.login.mockRejectedValue(
      httpError(429, 42),
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("anonymous");
    });

    await act(async () => {
      await expect(
        result.current.login(credentials),
      ).rejects.toMatchObject({
        status: 429,
        retryAfterSeconds: 42,
      });
    });

    expect(
      result.current.state.status,
    ).toBe("anonymous");
  });

  it.each([
    httpError(503),

    new APIError(
      "timeout",
      "Request timed out",
    ),

    new APIError(
      "network",
      "Connection lost",
    ),

    new APIError(
      "invalid-response",
      "Invalid authentication response",
    ),
  ])(
    "requires restoration after an uncertain login outcome: %s",
    async (error) => {
      const client = createClient();

      client.getSession.mockRejectedValue(
        httpError(401),
      );

      client.login.mockRejectedValue(error);

      const { result } =
        renderAuth(client);

      await waitFor(() => {
        expect(
          result.current.state.status,
        ).toBe("anonymous");
      });

      await act(async () => {
        await expect(
          result.current.login(credentials),
        ).rejects.toBeInstanceOf(APIError);
      });

      expect(
        result.current.state,
      ).toEqual({
        status: "unavailable",
      });
    },
  );

  it("rejects overlapping login requests", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(401),
    );

    const pending =
      deferred<AdministratorSession>();

    client.login.mockReturnValue(
      pending.promise,
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("anonymous");
    });

    let first!: Promise<void>;

    act(() => {
      first =
        result.current.login(credentials);
    });

    await act(async () => {
      await expect(
        result.current.login(credentials),
      ).rejects.toMatchObject({
        kind: "invalid-request",
      });

      // Restoration must not interrupt
      // an in-progress login.
      await result.current.restoreSession();
    });

    expect(
      client.login,
    ).toHaveBeenCalledOnce();

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    await act(async () => {
      pending.resolve(session);
      await first;
    });

    expect(
      result.current.state.status,
    ).toBe("authenticated");
  });

  it("rejects login while already authenticated", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    await expect(
      result.current.login(credentials),
    ).rejects.toMatchObject({
      kind: "invalid-request",
    });

    expect(
      client.login,
    ).not.toHaveBeenCalled();
  });
});

describe("Administrator logout", () => {
  it("removes session metadata during logout", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    const pending = deferred<void>();

    client.logout.mockReturnValue(
      pending.promise,
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    let operation!: Promise<void>;

    act(() => {
      operation = result.current.logout();
    });

    expect(result.current.state).toEqual({
      status: "signing-out",
    });

    await act(async () => {
      pending.resolve(undefined);
      await operation;
    });

    expect(result.current.state).toEqual({
      status: "anonymous",
    });

    expect(
      client.logout,
    ).toHaveBeenCalledOnce();
  });

  it("treats an already invalid session as logged out", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    client.logout.mockRejectedValue(
      httpError(401),
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    await act(async () => {
      await result.current.logout();
    });

    expect(result.current.state).toEqual({
      status: "anonymous",
    });
  });

  it("does not report successful logout after a server failure", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    client.logout.mockRejectedValue(
      httpError(503),
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    await act(async () => {
      await expect(
        result.current.logout(),
      ).rejects.toMatchObject({
        status: 503,
      });
    });

    expect(result.current.state).toEqual({
      status: "unavailable",
    });

    // The server session may still exist.
    // An explicit restoration can recover it.
    await act(async () => {
      await result.current.restoreSession();
    });

    expect(result.current.state).toEqual({
      status: "authenticated",
      session,
    });
  });

  it("rejects overlapping logout and login operations", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    const pending = deferred<void>();

    client.logout.mockReturnValue(
      pending.promise,
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    let operation!: Promise<void>;

    act(() => {
      operation = result.current.logout();
    });

    await act(async () => {
      await expect(
        result.current.logout(),
      ).rejects.toMatchObject({
        kind: "invalid-request",
      });

      await expect(
        result.current.login(credentials),
      ).rejects.toMatchObject({
        kind: "invalid-request",
      });

      await result.current.restoreSession();
    });

    expect(
      client.logout,
    ).toHaveBeenCalledOnce();

    expect(
      client.login,
    ).not.toHaveBeenCalled();

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    await act(async () => {
      pending.resolve(undefined);
      await operation;
    });

    expect(
      result.current.state.status,
    ).toBe("anonymous");
  });
});

describe("Authentication provider lifecycle", () => {
  it("cancels session restoration when unmounted", () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.getSession.mockReturnValue(
      pending.promise,
    );

    const { unmount } = renderAuth(client);

    const signal =
      client.getSession.mock.calls[0]?.[0]
        ?.signal;

    expect(signal?.aborted).toBe(false);

    unmount();

    expect(signal?.aborted).toBe(true);
  });

  it("ignores stale restoration results under Strict Mode", async () => {
    const client = createClient();

    const first =
      deferred<AdministratorSession>();

    const second =
      deferred<AdministratorSession>();

    client.getSession
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);

    const { result } =
      renderAuth(client, true);

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    const firstSignal =
      client.getSession.mock.calls[0]?.[0]
        ?.signal;

    const secondSignal =
      client.getSession.mock.calls[1]?.[0]
        ?.signal;

    expect(
      firstSignal?.aborted,
    ).toBe(true);

    expect(
      secondSignal?.aborted,
    ).toBe(false);

    await act(async () => {
      second.reject(httpError(401));
    });

    expect(result.current.state).toEqual({
      status: "anonymous",
    });

    // Simulate a transport that ignores
    // cancellation and returns late.
    await act(async () => {
      first.resolve(session);
    });

    expect(result.current.state).toEqual({
      status: "anonymous",
    });
  });

  it("does not let a stale login complete after unmount", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(401),
    );

    const pending =
      deferred<AdministratorSession>();

    client.login.mockReturnValue(
      pending.promise,
    );

    const { result, unmount } =
      renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("anonymous");
    });

    let operation!: Promise<void>;

    act(() => {
      operation =
        result.current.login(credentials);
    });

    const signal =
      client.login.mock.calls[0]?.[1]
        ?.signal;

    const assertion = expect(
      operation,
    ).rejects.toMatchObject({
      kind: "cancelled",
    });

    unmount();

    expect(signal?.aborted).toBe(true);

    pending.resolve(session);

    await assertion;
  });
});
describe("Management session invalidation", () => {
  it("rechecks authentication after an unauthorized management response", async () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.getSession
      .mockResolvedValueOnce(session)
      .mockReturnValueOnce(
        pending.promise,
      );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    const notify =
      result.current
        .captureManagementUnauthorized();

    expect(notify).toBeDefined();

    act(() => {
      notify?.();
    });

    expect(result.current.state).toEqual({
      status: "checking",
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    await act(async () => {
      pending.reject(httpError(401));
    });

    expect(result.current.state).toEqual({
      status: "anonymous",
    });
  });

  it("coalesces simultaneous unauthorized management responses", async () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.getSession
      .mockResolvedValueOnce(session)
      .mockReturnValueOnce(
        pending.promise,
      );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    const notifications = Array.from(
      { length: 20 },
      () =>
        result.current
          .captureManagementUnauthorized(),
    );

    expect(
      notifications.every(
        (notify) =>
          typeof notify === "function",
      ),
    ).toBe(true);

    act(() => {
      for (const notify of notifications) {
        notify?.();
      }
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(result.current.state).toEqual({
      status: "checking",
    });

    await act(async () => {
      pending.reject(httpError(401));
    });

    expect(result.current.state).toEqual({
      status: "anonymous",
    });

    expect(
      client.logout,
    ).not.toHaveBeenCalled();

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();
  });

  it("fails closed when session revalidation is unavailable", async () => {
    const client = createClient();

    client.getSession
      .mockResolvedValueOnce(session)
      .mockRejectedValueOnce(
        httpError(503),
      );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    const notify =
      result.current
        .captureManagementUnauthorized();

    await act(async () => {
      notify?.();
    });

    expect(result.current.state).toEqual({
      status: "unavailable",
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);
  });

  it("restores a valid session after a stale cookie failure", async () => {
    const client = createClient();

    const replacementSession = {
      ...session,

      created_at:
        "2026-11-27T13:00:00Z",

      last_activity_at:
        "2026-11-27T13:00:00Z",

      expires_at:
        "2026-11-28T01:00:00Z",

      idle_expires_at:
        "2026-11-27T13:30:00Z",
    };

    client.getSession
      .mockResolvedValueOnce(session)
      .mockResolvedValueOnce(
        replacementSession,
      );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    const notify =
      result.current
        .captureManagementUnauthorized();

    await act(async () => {
      notify?.();
    });

    expect(result.current.state).toEqual({
      status: "authenticated",
      session: replacementSession,
    });

    expect(
      client.login,
    ).not.toHaveBeenCalled();

    expect(
      client.logout,
    ).not.toHaveBeenCalled();
  });

  it("ignores notifications from a previous login", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    client.logout.mockResolvedValue(
      undefined,
    );

    // Deliberately reuse identical public
    // session metadata to prove that the
    // generation determines ownership.
    client.login.mockResolvedValue(
      session,
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    const previousNotification =
      result.current
        .captureManagementUnauthorized();

    expect(
      previousNotification,
    ).toBeDefined();

    await act(async () => {
      await result.current.logout();
    });

    expect(result.current.state).toEqual({
      status: "anonymous",
    });

    await act(async () => {
      await result.current.login(
        credentials,
      );
    });

    expect(result.current.state).toEqual({
      status: "authenticated",
      session,
    });

    act(() => {
      previousNotification?.();
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    expect(result.current.state).toEqual({
      status: "authenticated",
      session,
    });
  });

  it("ignores obsolete notifications after successful revalidation", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    const first =
      result.current
        .captureManagementUnauthorized();

    const second =
      result.current
        .captureManagementUnauthorized();

    await act(async () => {
      first?.();
    });

    expect(result.current.state).toEqual({
      status: "authenticated",
      session,
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    act(() => {
      second?.();
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);
  });

  it("does not interrupt an active logout", async () => {
    const client = createClient();

    const pending = deferred<void>();

    client.getSession.mockResolvedValue(
      session,
    );

    client.logout.mockReturnValue(
      pending.promise,
    );

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    const notify =
      result.current
        .captureManagementUnauthorized();

    let operation!: Promise<void>;

    act(() => {
      operation =
        result.current.logout();
    });

    act(() => {
      notify?.();
    });

    expect(result.current.state).toEqual({
      status: "signing-out",
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    await act(async () => {
      pending.resolve(undefined);
      await operation;
    });

    expect(result.current.state).toEqual({
      status: "anonymous",
    });
  });

  it("does not capture notifications while authentication is unresolved", async () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.getSession.mockReturnValue(
      pending.promise,
    );

    const { result } = renderAuth(client);

    expect(
      result.current
        .captureManagementUnauthorized(),
    ).toBeUndefined();

    await act(async () => {
      pending.reject(httpError(401));
    });

    expect(result.current.state).toEqual({
      status: "anonymous",
    });

    expect(
      result.current
        .captureManagementUnauthorized(),
    ).toBeUndefined();
  });

  it("ignores notifications after provider unmount", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    const {
      result,
      unmount,
    } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    const notify =
      result.current
        .captureManagementUnauthorized();

    unmount();

    notify?.();

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();
  });
});