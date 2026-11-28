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
    "2026-11-27T12:00:00Z",

  expires_at:
    "2026-11-28T00:00:00Z",

  idle_expires_at:
    "2026-11-27T12:30:00Z",
};

const renewedSession: AdministratorSession = {
  ...session,

  last_activity_at:
    "2026-11-27T12:10:00Z",

  idle_expires_at:
    "2026-11-27T12:40:00Z",
};

function httpError(
  status: number,
): APIError {
  return new APIError(
    "http",
    "Synthetic authentication error",
    status,
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
      vi.fn<AuthClient["getSession"]>()
        .mockResolvedValue(session),

    login:
      vi.fn<AuthClient["login"]>()
        .mockResolvedValue(session),

    logout:
      vi.fn<AuthClient["logout"]>()
        .mockResolvedValue(undefined),

    recordActivity:
      vi.fn<AuthClient["recordActivity"]>()
        .mockResolvedValue(renewedSession),
  };
}

async function renderAuth(
  client: AuthClient,
  expectedState = "authenticated",
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

  const result = renderHook(
    () => useAuth(),
    {
      wrapper: Wrapper,
    },
  );

  await waitFor(() => {
    expect(
      result.result.current.state.status,
    ).toBe(expectedState);
  });

  return result;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("Administrator activity renewal", () => {
  it("updates activity metadata without changing authentication generation", async () => {
    const client = createClient();

    const { result } =
      await renderAuth(client);

    const previousGeneration =
      result.current.sessionGeneration;

    await act(async () => {
      await result.current.recordActivity();
    });

    expect(
      result.current.state,
    ).toEqual({
      status: "authenticated",
      session: renewedSession,
    });

    expect(
      result.current.sessionGeneration,
    ).toBe(previousGeneration);

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();
  });

  it("throttles renewal attempts to once per minute", async () => {
    const client = createClient();

    const clock = vi.spyOn(
      performance,
      "now",
    ).mockReturnValue(1000);

    const { result } =
      await renderAuth(client);

    await act(async () => {
      await result.current.recordActivity();
    });

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();

    clock.mockReturnValue(60_999);

    await act(async () => {
      for (let index = 0; index < 20; index++) {
        await result.current.recordActivity();
      }
    });

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();

    clock.mockReturnValue(61_000);

    await act(async () => {
      await result.current.recordActivity();
    });

    expect(
      client.recordActivity,
    ).toHaveBeenCalledTimes(2);
  });

  it("does not admit overlapping activity requests", async () => {
    const client = createClient();

    const clock = vi.spyOn(
      performance,
      "now",
    ).mockReturnValue(1000);

    const pending =
      deferred<AdministratorSession>();

    client.recordActivity.mockReturnValue(
      pending.promise,
    );

    const { result } =
      await renderAuth(client);

    let operation!: Promise<void>;

    act(() => {
      operation =
        result.current.recordActivity();
    });

    // Even when the throttle expires,
    // the outstanding request retains
    // exclusive activity ownership.
    clock.mockReturnValue(61_000);

    await act(async () => {
      await result.current.recordActivity();
    });

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();

    await act(async () => {
      pending.resolve(renewedSession);
      await operation;
    });

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();
  });

  it("does not renew anonymous sessions", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(401),
    );

    const { result } =
      await renderAuth(
        client,
        "anonymous",
      );

    await act(async () => {
      await result.current.recordActivity();
    });

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();
  });

  it("does not renew unavailable sessions", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(503),
    );

    const { result } =
      await renderAuth(
        client,
        "unavailable",
      );

    await act(async () => {
      await result.current.recordActivity();
    });

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();
  });

  it("verifies the session after unauthorized renewal", async () => {
    const client = createClient();

    const { result } =
      await renderAuth(client);

    client.recordActivity.mockRejectedValue(
      httpError(401),
    );

    client.getSession.mockRejectedValueOnce(
      httpError(401),
    );

    await act(async () => {
      await result.current.recordActivity();
    });

    expect(
      result.current.state,
    ).toEqual({
      status: "anonymous",
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();
  });

  it("fails closed when renewal and verification are unavailable", async () => {
    const client = createClient();

    const { result } =
      await renderAuth(client);

    client.recordActivity.mockRejectedValue(
      httpError(503),
    );

    client.getSession.mockRejectedValueOnce(
      httpError(503),
    );

    await act(async () => {
      await result.current.recordActivity();
    });

    expect(
      result.current.state,
    ).toEqual({
      status: "unavailable",
    });

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();
  });

  it("does not automatically retry an uncertain activity mutation", async () => {
    const client = createClient();

    const { result } =
      await renderAuth(client);

    client.recordActivity.mockRejectedValue(
      new APIError(
        "timeout",
        "Synthetic timeout",
      ),
    );

    client.getSession.mockResolvedValueOnce(
      renewedSession,
    );

    await act(async () => {
      await result.current.recordActivity();
    });

    expect(
      result.current.state,
    ).toEqual({
      status: "authenticated",
      session: renewedSession,
    });

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    // Revalidation must not reset
    // the activity attempt budget.
    await act(async () => {
      await result.current.recordActivity();
    });

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();
  });

  const inconsistentResponses: [
    string,
    AdministratorSession,
  ][] = [
    [
      "administrator identity",
      {
        ...renewedSession,
        administrator_id:
          "22222222-2222-4222-8222-222222222222",
      },
    ],
    [
      "session creation timestamp",
      {
        ...renewedSession,
        created_at:
          "2026-11-27T11:00:00Z",
      },
    ],
    [
      "absolute expiration",
      {
        ...renewedSession,
        expires_at:
          "2026-11-28T01:00:00Z",
      },
    ],
    [
      "backward activity timestamp",
      {
        ...renewedSession,
        last_activity_at:
          "2026-11-27T11:59:00Z",
      },
    ],
    [
      "backward inactivity deadline",
      {
        ...renewedSession,
        idle_expires_at:
          "2026-11-27T12:29:00Z",
      },
    ],
  ];

  it.each(inconsistentResponses)(
    "revalidates after inconsistent %s",
    async (_name, response) => {
      const client = createClient();

      client.recordActivity.mockResolvedValue(
        response,
      );

      const { result } =
        await renderAuth(client);

      await act(async () => {
        await result.current.recordActivity();
      });

      expect(
        client.getSession,
      ).toHaveBeenCalledTimes(2);

      expect(
        result.current.state,
      ).toEqual({
        status: "authenticated",
        session,
      });
    },
  );
});

describe("Activity renewal operation coordination", () => {
  it("allows logout to preempt an outstanding activity request", async () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.recordActivity.mockReturnValue(
      pending.promise,
    );

    const { result } =
      await renderAuth(client);

    let operation!: Promise<void>;

    act(() => {
      operation =
        result.current.recordActivity();
    });

    const signal =
      client.recordActivity.mock.calls[0]
        ?.[0]?.signal;

    expect(signal?.aborted).toBe(false);

    await act(async () => {
      await result.current.logout();
    });

    expect(signal?.aborted).toBe(true);

    expect(
      client.logout,
    ).toHaveBeenCalledOnce();

    expect(
      result.current.state,
    ).toEqual({
      status: "anonymous",
    });

    // Simulate a late response from a
    // transport that ignores cancellation.
    await act(async () => {
      pending.resolve(renewedSession);
      await operation;
    });

    expect(
      result.current.state,
    ).toEqual({
      status: "anonymous",
    });
  });

  it("ignores stale renewal responses after session verification", async () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.recordActivity.mockReturnValue(
      pending.promise,
    );

    const { result } =
      await renderAuth(client);

    let operation!: Promise<void>;

    act(() => {
      operation =
        result.current.recordActivity();
    });

    const signal =
      client.recordActivity.mock.calls[0]
        ?.[0]?.signal;

    const replacement = {
      ...session,

      administrator_id:
        "22222222-2222-4222-8222-222222222222",
    };

    client.getSession.mockResolvedValueOnce(
      replacement,
    );

    await act(async () => {
      await result.current.restoreSession();
    });

    expect(signal?.aborted).toBe(true);

    await act(async () => {
      pending.resolve(renewedSession);
      await operation;
    });

    expect(
      result.current.state,
    ).toEqual({
      status: "authenticated",
      session: replacement,
    });
  });

  it("ignores renewal failures after logout", async () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.recordActivity.mockReturnValue(
      pending.promise,
    );

    const { result } =
      await renderAuth(client);

    let operation!: Promise<void>;

    act(() => {
      operation =
        result.current.recordActivity();
    });

    await act(async () => {
      await result.current.logout();
    });

    await act(async () => {
      pending.reject(httpError(401));
      await operation;
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    expect(
      result.current.state,
    ).toEqual({
      status: "anonymous",
    });
  });

  it("cancels activity renewal on provider unmount", async () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.recordActivity.mockReturnValue(
      pending.promise,
    );

    const {
      result,
      unmount,
    } = await renderAuth(client);

    let operation!: Promise<void>;

    act(() => {
      operation =
        result.current.recordActivity();
    });

    const signal =
      client.recordActivity.mock.calls[0]
        ?.[0]?.signal;

    unmount();

    expect(signal?.aborted).toBe(true);

    pending.resolve(renewedSession);

    await operation;

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();
  });
});