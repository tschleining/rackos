import {
  afterEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import {
  RackOSAPIClient,
  type AdministratorSession,
} from "@rackos/api-client";

const session: AdministratorSession = {
  administrator_id:
    "12345678-1234-4000-8000-123456789abc",

  created_at: "2026-11-27T12:00:00Z",

  last_activity_at:
    "2026-11-27T12:10:00Z",

  expires_at: "2026-11-28T00:00:00Z",

  idle_expires_at:
    "2026-11-27T12:40:00Z",
};

function jsonResponse(
  body: unknown,
  status = 200,
  headers: Record<string, string> = {},
): Response {
  return new Response(
    JSON.stringify(body),
    {
      status,

      headers: {
        "Content-Type":
          "application/json; charset=utf-8",

        ...headers,
      },
    },
  );
}

function createTestClient(
  authenticatedManagement = false,
) {
  const fetcher = vi.fn<typeof fetch>();

  const api = new RackOSAPIClient({
    fetch: fetcher,
    authenticatedManagement,
  });

  return { api, fetcher };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("Administrator authentication API", () => {
  it("submits credentials using a same-origin POST", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(session),
    );

    const credentials = {
      username: "ADMIN",
      password: " synthetic password ",
    };

    await expect(
      api.login(credentials),
    ).resolves.toEqual(session);

    expect(fetcher).toHaveBeenCalledOnce();

    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/auth/login",
      expect.objectContaining({
        method: "POST",

        mode: "same-origin",
        credentials: "same-origin",
        cache: "no-store",
        redirect: "error",

        headers: {
          Accept: "application/json",

          "Content-Type":
            "application/json",

          "X-RackOS-CSRF": "1",
        },

        body: JSON.stringify(credentials),

        signal: expect.any(AbortSignal),
      }),
    );
  });

  it("does not serialize unexpected credential fields", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(session),
    );

    const credentials = {
      username: "admin",
      password: "synthetic-password",
      unexpected: "not-an-api-field",
    };

    await api.login(credentials);

    const request =
      fetcher.mock.calls[0]?.[1];

    expect(request?.body).toBe(
      JSON.stringify({
        username: credentials.username,
        password: credentials.password,
      }),
    );
  });

  it("restores a session without renewing activity", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(session),
    );

    await expect(
      api.getSession(),
    ).resolves.toEqual(session);

    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/auth/session",
      expect.objectContaining({
        method: "GET",

        credentials: "same-origin",

        headers: {
          Accept: "application/json",
        },
      }),
    );

    expect(
      fetcher.mock.calls[0]?.[1]?.body,
    ).toBeUndefined();
  });

  it("explicitly renews session activity", async () => {
    const { api, fetcher } =
      createTestClient();

    const renewed = {
      ...session,

      last_activity_at:
        "2026-11-27T12:20:00Z",

      idle_expires_at:
        "2026-11-27T12:50:00Z",
    };

    fetcher.mockResolvedValue(
      jsonResponse(renewed),
    );

    await expect(
      api.recordActivity(),
    ).resolves.toEqual(renewed);

    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/auth/activity",
      expect.objectContaining({
        method: "POST",

        credentials: "same-origin",

        headers: {
          Accept: "application/json",
          "X-RackOS-CSRF": "1",
        },
      }),
    );

    expect(
      fetcher.mock.calls[0]?.[1]?.body,
    ).toBeUndefined();
  });

  it("accepts an empty successful logout response", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      new Response(null, {
        status: 204,
      }),
    );

    await expect(
      api.logout(),
    ).resolves.toBeUndefined();

    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/auth/logout",
      expect.objectContaining({
        method: "POST",

        credentials: "same-origin",

        headers: {
          Accept: "application/json",
          "X-RackOS-CSRF": "1",
        },
      }),
    );

    expect(
      fetcher.mock.calls[0]?.[1]?.body,
    ).toBeUndefined();
  });

  it("rejects unexpected successful logout statuses", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse({}, 200),
    );

    await expect(
      api.logout(),
    ).rejects.toMatchObject({
      kind: "http",
      status: 200,
    });
  });

  it.each([
    400,
    401,
    403,
    409,
    413,
    415,
    429,
    503,
  ])(
    "preserves HTTP status %i without exposing server details",
    async (status) => {
      const { api, fetcher } =
        createTestClient();

      fetcher.mockResolvedValue(
        jsonResponse(
          {
            error:
              "private-authentication-detail",
          },
          status,
        ),
      );

      await expect(
        api.login({
          username: "admin",
          password: "synthetic-password",
        }),
      ).rejects.toMatchObject({
        kind: "http",
        status,
        serverMessage: null,

        message:
          `Authentication request failed (HTTP ${status})`,
      });

      expect(
        fetcher,
      ).toHaveBeenCalledOnce();
    },
  );

  it("distinguishes unavailable authentication from an invalid session", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValueOnce(
      jsonResponse(
        { error: "authentication required" },
        401,
      ),
    );

    fetcher.mockResolvedValueOnce(
      jsonResponse(
        {
          error:
            "authentication service unavailable",
        },
        503,
      ),
    );

    await expect(
      api.getSession(),
    ).rejects.toMatchObject({
      kind: "http",
      status: 401,
    });

    await expect(
      api.getSession(),
    ).rejects.toMatchObject({
      kind: "http",
      status: 503,
    });
  });

  it("preserves the login retry interval", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(
        {
          error:
            "too many authentication attempts",
        },
        429,
        {
          "Retry-After": "42",
        },
      ),
    );

    await expect(
      api.login({
        username: "admin",
        password: "synthetic-password",
      }),
    ).rejects.toMatchObject({
      kind: "http",
      status: 429,
      retryAfterSeconds: 42,
    });
  });

  it.each([
    "",
    "-1",
    "1.5",
    "1e3",
    "invalid",
    "99999999999999999999",
  ])(
    "rejects malformed Retry-After value %s",
    async (value) => {
      const { api, fetcher } =
        createTestClient();

      fetcher.mockResolvedValue(
        jsonResponse(
          {},
          429,
          {
            "Retry-After": value,
          },
        ),
      );

      await expect(
        api.login({
          username: "admin",
          password: "synthetic-password",
        }),
      ).rejects.toMatchObject({
        kind: "http",
        status: 429,
        retryAfterSeconds: null,
      });
    },
  );

  it("does not retry failed authentication mutations", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockRejectedValue(
      new TypeError(
        "private-network-detail",
      ),
    );

    await expect(
      api.login({
        username: "admin",
        password: "synthetic-password",
      }),
    ).rejects.toMatchObject({
      kind: "network",

      message:
        "Unable to communicate with the RackOS appliance",
    });

    expect(fetcher).toHaveBeenCalledOnce();
  });

  it("rejects credentials on a plaintext browser origin", async () => {
    vi.stubGlobal("window", {
      location: {
        protocol: "http:",
      },
    });

    const { api, fetcher } =
      createTestClient();

    await expect(
      api.login({
        username: "admin",
        password: "synthetic-password",
      }),
    ).rejects.toMatchObject({
      kind: "invalid-request",
    });

    expect(fetcher).not.toHaveBeenCalled();
  });

  it("permits authentication on an HTTPS browser origin", async () => {
    vi.stubGlobal("window", {
      location: {
        protocol: "https:",
      },
    });

    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(session),
    );

    await expect(
      api.getSession(),
    ).resolves.toEqual(session);
  });

  it("honors cancellation before sending credentials", async () => {
    const { api, fetcher } =
      createTestClient();

    const controller =
      new AbortController();

    controller.abort();

    await expect(
      api.login(
        {
          username: "admin",
          password: "synthetic-password",
        },
        {
          signal: controller.signal,
        },
      ),
    ).rejects.toMatchObject({
      kind: "cancelled",
    });

    expect(fetcher).not.toHaveBeenCalled();
  });

  it("cancels an in-flight authentication request", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockImplementation(
      (_input, init) =>
        new Promise<Response>(
          (_resolve, reject) => {
            init?.signal?.addEventListener(
              "abort",
              () => reject(
                new Error("aborted"),
              ),
              { once: true },
            );
          },
        ),
    );

    const controller =
      new AbortController();

    const request = api.getSession({
      signal: controller.signal,
    });

    controller.abort();

    await expect(
      request,
    ).rejects.toMatchObject({
      kind: "cancelled",
    });
  });

  it("enforces authentication request timeouts", async () => {
    vi.useFakeTimers();

    const fetcher = vi.fn<typeof fetch>();

    const api = new RackOSAPIClient({
      fetch: fetcher,
      timeoutMs: 1000,
    });

    fetcher.mockImplementation(
      (_input, init) =>
        new Promise<Response>(
          (_resolve, reject) => {
            init?.signal?.addEventListener(
              "abort",
              () => reject(
                new Error("aborted"),
              ),
              { once: true },
            );
          },
        ),
    );

    const request = api.getSession();

    const assertion = expect(
      request,
    ).rejects.toMatchObject({
      kind: "timeout",
    });

    await vi.advanceTimersByTimeAsync(
      1000,
    );

    await assertion;

    expect(fetcher).toHaveBeenCalledOnce();
  });
});

describe("Administrator session validation", () => {
  it("accepts an inactivity deadline capped by absolute expiration", async () => {
    const { api, fetcher } =
      createTestClient();

    const capped = {
      ...session,

      last_activity_at:
        "2026-11-27T23:50:00Z",

      idle_expires_at:
        session.expires_at,
    };

    fetcher.mockResolvedValue(
      jsonResponse(capped),
    );

    await expect(
      api.getSession(),
    ).resolves.toEqual(capped);
  });

  const invalidSessions: [
    string,
    unknown,
  ][] = [
    [
      "null response",
      null,
    ],
    [
      "empty response",
      {},
    ],
    [
      "array response",
      [session],
    ],
    [
      "invalid administrator identity",
      {
        ...session,
        administrator_id: "invalid",
      },
    ],
    [
      "invalid timestamp",
      {
        ...session,
        created_at: "invalid",
      },
    ],
    [
      "invalid calendar date",
      {
        ...session,
        created_at:
          "2026-02-30T12:00:00Z",
      },
    ],
    [
      "non-UTC timestamp",
      {
        ...session,
        created_at:
          "2026-11-27T12:00:00+00:00",
      },
    ],
    [
      "sub-millisecond timestamp",
      {
        ...session,
        created_at:
          "2026-11-27T12:00:00.0001Z",
      },
    ],
    [
      "activity before creation",
      {
        ...session,
        last_activity_at:
          "2026-11-27T11:00:00Z",
      },
    ],
    [
      "activity after expiration",
      {
        ...session,
        last_activity_at:
          "2026-11-28T01:00:00Z",
      },
    ],
    [
      "extended absolute lifetime",
      {
        ...session,
        expires_at:
          "2026-11-28T01:00:00Z",
      },
    ],
    [
      "incorrect inactivity deadline",
      {
        ...session,
        idle_expires_at:
          "2026-11-27T13:00:00Z",
      },
    ],
    [
      "unexpected authentication token",
      {
        ...session,
        token: "unexpected-token",
      },
    ],
    [
      "unexpected internal session ID",
      {
        ...session,
        id: "internal-session-id",
      },
    ],
    [
      "unexpected credential revision",
      {
        ...session,
        password_changed_at:
          session.created_at,
      },
    ],
    [
      "missing session timestamp",
      {
        ...session,
        idle_expires_at: undefined,
      },
    ],
  ];

  it.each(invalidSessions)(
    "rejects %s",
    async (_name, response) => {
      const { api, fetcher } =
        createTestClient();

      fetcher.mockResolvedValue(
        jsonResponse(response),
      );

      await expect(
        api.getSession(),
      ).rejects.toMatchObject({
        kind: "invalid-response",
      });
    },
  );

  it("rejects HTML instead of authentication JSON", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      new Response(
        "<html>Unexpected page</html>",
        {
          status: 200,
          headers: {
            "Content-Type": "text/html",
          },
        },
      ),
    );

    await expect(
      api.getSession(),
    ).rejects.toMatchObject({
      kind: "invalid-response",
    });
  });
});

describe("Authenticated management requests", () => {
  it("preserves existing credential-free requests by default", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse({
        devices: [],
        total: 0,
      }),
    );

    await api.getDevices();

    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/devices",
      expect.objectContaining({
        method: "GET",
        credentials: "omit",
      }),
    );
  });

  it("includes browser cookies when authenticated management is enabled", async () => {
    const { api, fetcher } =
      createTestClient(true);

    fetcher.mockResolvedValue(
      jsonResponse({
        devices: [],
        total: 0,
      }),
    );

    await api.getDevices();

    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/devices",
      expect.objectContaining({
        method: "GET",

        credentials: "same-origin",
        mode: "same-origin",
        redirect: "error",
        cache: "no-store",

        headers: {
          Accept: "application/json",
        },
      }),
    );
  });

  it("does not enable authenticated management implicitly after login", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValueOnce(
      jsonResponse(session),
    );

    fetcher.mockResolvedValueOnce(
      jsonResponse({
        devices: [],
        total: 0,
      }),
    );

    await api.login({
      username: "admin",
      password: "synthetic-password",
    });

    await api.getDevices();

    expect(
      fetcher.mock.calls[0]?.[1]?.credentials,
    ).toBe("same-origin");

    expect(
      fetcher.mock.calls[1]?.[1]?.credentials,
    ).toBe("omit");
  });
});
describe("Management authentication coordination", () => {
  function createObservedClient() {
    const fetcher = vi.fn<typeof fetch>();

    const notify = vi.fn();

    const capture = vi.fn(
      () => notify,
    );

    const api = new RackOSAPIClient({
      fetch: fetcher,

      authenticatedManagement: true,

      captureManagementUnauthorized:
        capture,
    });

    return {
      api,
      fetcher,
      capture,
      notify,
    };
  }

  it("reports unauthorized protected management requests", async () => {
    const {
      api,
      fetcher,
      capture,
      notify,
    } = createObservedClient();

    fetcher.mockResolvedValue(
      jsonResponse(
        {
          error:
            "authentication required",
        },
        401,
      ),
    );

    await expect(
      api.getDevices(),
    ).rejects.toMatchObject({
      kind: "http",
      status: 401,
    });

    expect(capture).toHaveBeenCalledOnce();
    expect(notify).toHaveBeenCalledOnce();

    expect(fetcher).toHaveBeenCalledOnce();
  });

  it.each([
    400,
    403,
    404,
    429,
    500,
    503,
  ])(
    "does not invalidate authentication for HTTP %i",
    async (status) => {
      const {
        api,
        fetcher,
        notify,
      } = createObservedClient();

      fetcher.mockResolvedValue(
        jsonResponse(
          { error: "request failed" },
          status,
        ),
      );

      await expect(
        api.getDevices(),
      ).rejects.toMatchObject({
        kind: "http",
        status,
      });

      expect(
        notify,
      ).not.toHaveBeenCalled();
    },
  );

  it("does not report ordinary network failures", async () => {
    const {
      api,
      fetcher,
      notify,
    } = createObservedClient();

    fetcher.mockRejectedValue(
      new TypeError("Connection lost"),
    );

    await expect(
      api.getDevices(),
    ).rejects.toMatchObject({
      kind: "network",
    });

    expect(
      notify,
    ).not.toHaveBeenCalled();
  });

  it("does not report unauthorized authentication endpoints", async () => {
    const {
      api,
      fetcher,
      capture,
      notify,
    } = createObservedClient();

    fetcher.mockImplementation(
      async () =>
        jsonResponse(
          {
            error:
              "authentication required",
          },
          401,
        ),
    );

    await expect(
      api.getSession(),
    ).rejects.toMatchObject({
      status: 401,
    });

    await expect(
      api.login({
        username: "admin",
        password: "synthetic-password",
      }),
    ).rejects.toMatchObject({
      status: 401,
    });

    await expect(
      api.logout(),
    ).rejects.toMatchObject({
      status: 401,
    });

    await expect(
      api.recordActivity(),
    ).rejects.toMatchObject({
      status: 401,
    });

    expect(
      capture,
    ).not.toHaveBeenCalled();

    expect(
      notify,
    ).not.toHaveBeenCalled();
  });

  it("does not report unauthorized public endpoints", async () => {
    const {
      api,
      fetcher,
      capture,
      notify,
    } = createObservedClient();

    fetcher.mockImplementation(
      async () =>
        jsonResponse({}, 401),
    );

    await expect(
      api.getHealth(),
    ).rejects.toMatchObject({
      status: 401,
    });

    await expect(
      api.getReadiness(),
    ).rejects.toMatchObject({
      status: 401,
    });

    await expect(
      api.getVersion(),
    ).rejects.toMatchObject({
      status: 401,
    });

    expect(
      capture,
    ).not.toHaveBeenCalled();

    expect(
      notify,
    ).not.toHaveBeenCalled();
  });

  it("does not observe credential-free management requests", async () => {
    const fetcher = vi.fn<typeof fetch>();

    const notify = vi.fn();

    const capture = vi.fn(
      () => notify,
    );

    const api = new RackOSAPIClient({
      fetch: fetcher,

      captureManagementUnauthorized:
        capture,
    });

    fetcher.mockResolvedValue(
      jsonResponse({}, 401),
    );

    await expect(
      api.getDevices(),
    ).rejects.toMatchObject({
      status: 401,
    });

    expect(
      capture,
    ).not.toHaveBeenCalled();

    expect(
      notify,
    ).not.toHaveBeenCalled();
  });

  it("captures the observer before the response arrives", async () => {
    const fetcher = vi.fn<typeof fetch>();

    const firstObserver = vi.fn();
    const secondObserver = vi.fn();

    let currentObserver =
      firstObserver;

    const capture = vi.fn(
      () => currentObserver,
    );

    const api = new RackOSAPIClient({
      fetch: fetcher,

      authenticatedManagement: true,

      captureManagementUnauthorized:
        capture,
    });

    let resolveResponse!: (
      response: Response,
    ) => void;

    fetcher.mockReturnValue(
      new Promise<Response>(
        (resolve) => {
          resolveResponse = resolve;
        },
      ),
    );

    const request = api.getDevices();

    expect(capture).toHaveBeenCalledOnce();

    // Simulate a new authenticated session
    // becoming active while the request runs.
    currentObserver = secondObserver;

    const assertion = expect(
      request,
    ).rejects.toMatchObject({
      status: 401,
    });

    resolveResponse(
      jsonResponse({}, 401),
    );

    await assertion;

    expect(
      firstObserver,
    ).toHaveBeenCalledOnce();

    expect(
      secondObserver,
    ).not.toHaveBeenCalled();
  });

  it("preserves the original HTTP error if an observer fails", async () => {
    const {
      api,
      fetcher,
      notify,
    } = createObservedClient();

    notify.mockImplementation(
      () => {
        throw new Error(
          "Synthetic observer failure",
        );
      },
    );

    fetcher.mockResolvedValue(
      jsonResponse({}, 401),
    );

    await expect(
      api.getDevices(),
    ).rejects.toMatchObject({
      kind: "http",
      status: 401,
    });
  });

  it("does not report unauthorized responses after cancellation", async () => {
    const {
      api,
      fetcher,
      notify,
    } = createObservedClient();

    let resolveResponse!: (
      response: Response,
    ) => void;

    fetcher.mockReturnValue(
      new Promise<Response>(
        (resolve) => {
          resolveResponse = resolve;
        },
      ),
    );

    const controller =
      new AbortController();

    const request = api.getDevices({
      signal: controller.signal,
    });

    const assertion = expect(
      request,
    ).rejects.toMatchObject({
      kind: "cancelled",
    });

    controller.abort();

    resolveResponse(
      jsonResponse({}, 401),
    );

    await assertion;

    expect(
      notify,
    ).not.toHaveBeenCalled();
  });
});