// @vitest-environment jsdom

import {
  useEffect,
} from "react";

import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";

import {
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";

import {
  MemoryRouter,
  useLocation,
} from "react-router";

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
  AuthBoundary,
  AuthProvider,
  useAuth,
  type AuthClient,
} from "../src/features/auth";

import {
  bindManagementUnauthorized,
  captureManagementUnauthorized,
} from "../src/app/services";

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

const protectedQueryKey = [
  "protected-test-data",
] as const;

function httpError(
  status: number,
): APIError {
  return new APIError(
    "http",
    "Synthetic authentication failure",
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
      vi.fn<AuthClient["getSession"]>(),

    login:
      vi.fn<AuthClient["login"]>(),

    logout:
      vi.fn<AuthClient["logout"]>(),

    recordActivity:
      vi.fn<AuthClient["recordActivity"]>(),
  };
}

function renderApplication(
  client: AuthClient,
  initialPath = "/devices",
) {
  const fetchProtected =
    vi.fn(async () => "Protected device data");

  const mounted = vi.fn();
  const unmounted = vi.fn();

  const queryClients: QueryClient[] = [];

  function ProtectedContent() {
    const location = useLocation();
    const queryClient = useQueryClient();

    const {
      logout,
      recordActivity,
    } = useAuth();

    const query = useQuery({
      queryKey: protectedQueryKey,
      queryFn: fetchProtected,
    });

    useEffect(() => {
      mounted();

      queryClients.push(queryClient);

      return () => {
        unmounted();
      };
    }, [queryClient]);

    return (
      <main>
        <h1>Protected application</h1>

        <p data-testid="current-path">
          {location.pathname}
        </p>

        <p>
          {query.data ?? "Loading data"}
        </p>
        <button
          type="button"
          onClick={() => {
            void recordActivity();
          }}
        >
          Record test activity
        </button>
        <button
          type="button"
          onClick={() => {
            void logout().catch(() => {
              // The authentication boundary
              // owns unavailable-state handling.
            });
          }}
        >
          Sign out
        </button>
      </main>
    );
  }

  const result = render(
    <MemoryRouter
      initialEntries={[initialPath]}
    >
      <AuthProvider client={client}>
        <AuthBoundary>
          <ProtectedContent />
        </AuthBoundary>
      </AuthProvider>
    </MemoryRouter>,
  );

  return {
    ...result,
    fetchProtected,
    mounted,
    unmounted,
    queryClients,
  };
}

function submitLogin(): void {
  fireEvent.change(
    screen.getByLabelText("Username"),
    {
      target: {
        value: "admin",
      },
    },
  );

  fireEvent.change(
    screen.getByLabelText("Password"),
    {
      target: {
        value: "synthetic-test-password",
      },
    },
  );

  fireEvent.submit(
    screen.getByRole("form", {
      name: "Administrator sign in",
    }),
  );
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("Protected application boundary", () => {
  it("does not mount protected components during session restoration", async () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.getSession.mockReturnValue(
      pending.promise,
    );

    const {
      mounted,
      fetchProtected,
    } = renderApplication(client);

    expect(
      screen.getByRole("heading", {
        name: "Checking session",
      }),
    ).toBeTruthy();

    expect(
      screen.queryByRole("heading", {
        name: "Protected application",
      }),
    ).toBeNull();

    expect(
      mounted,
    ).not.toHaveBeenCalled();

    expect(
      fetchProtected,
    ).not.toHaveBeenCalled();

    await act(async () => {
      pending.resolve(session);
    });

    await screen.findByText(
      "Protected device data",
    );

    expect(mounted).toHaveBeenCalledOnce();

    expect(
      fetchProtected,
    ).toHaveBeenCalledOnce();
  });

  it("renders login without fetching protected management data", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(401),
    );

    const {
      mounted,
      fetchProtected,
    } = renderApplication(client);

    await screen.findByRole("heading", {
      name: "Administrator sign in",
    });

    expect(
      mounted,
    ).not.toHaveBeenCalled();

    expect(
      fetchProtected,
    ).not.toHaveBeenCalled();
  });

  it("preserves a protected deep link after login", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(401),
    );

    client.login.mockResolvedValue(
      session,
    );

    const path =
      "/devices/12345678-1234-4000-8000-123456789abc/performance";

    renderApplication(client, path);

    await screen.findByRole("heading", {
      name: "Administrator sign in",
    });

    submitLogin();

    await screen.findByRole("heading", {
      name: "Protected application",
    });

    expect(
      screen.getByTestId("current-path")
        .textContent,
    ).toBe(path);

    expect(
      screen.queryByLabelText("Password"),
    ).toBeNull();

    expect(
      client.login,
    ).toHaveBeenCalledOnce();
  });

  it("fails closed when authentication is unavailable", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(503),
    );

    const {
      mounted,
      fetchProtected,
    } = renderApplication(client);

    await screen.findByRole("heading", {
      name: "Authentication unavailable",
    });

    expect(
      screen.queryByLabelText("Password"),
    ).toBeNull();

    expect(
      mounted,
    ).not.toHaveBeenCalled();

    expect(
      fetchProtected,
    ).not.toHaveBeenCalled();
  });

  it("allows session verification to be retried", async () => {
    const client = createClient();

    client.getSession
      .mockRejectedValueOnce(
        httpError(503),
      )
      .mockResolvedValueOnce(
        session,
      );

    const {
      fetchProtected,
    } = renderApplication(client);

    await screen.findByRole("heading", {
      name: "Authentication unavailable",
    });

    fireEvent.click(
      screen.getByRole("button", {
        name: "Check session again",
      }),
    );

    await screen.findByText(
      "Protected device data",
    );

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(
      fetchProtected,
    ).toHaveBeenCalledOnce();
  });

  it("unmounts protected content when authentication becomes invalid", async () => {
    const client = createClient();

    const pending =
      deferred<AdministratorSession>();

    client.getSession
      .mockResolvedValueOnce(session)
      .mockReturnValueOnce(
        pending.promise,
      );

    const {
      unmounted,
    } = renderApplication(client);

    await screen.findByText(
      "Protected device data",
    );

    const notify =
      captureManagementUnauthorized();

    expect(notify).toBeDefined();

    act(() => {
      notify?.();
    });

    expect(
      screen.getByRole("heading", {
        name: "Checking session",
      }),
    ).toBeTruthy();

    expect(
      screen.queryByText(
        "Protected device data",
      ),
    ).toBeNull();

    expect(
      unmounted,
    ).toHaveBeenCalledOnce();

    await act(async () => {
      pending.reject(httpError(401));
    });

    expect(
      screen.getByRole("heading", {
        name: "Administrator sign in",
      }),
    ).toBeTruthy();
  });

  it("removes protected content immediately during logout", async () => {
    const client = createClient();

    const pending = deferred<void>();

    client.getSession.mockResolvedValue(
      session,
    );

    client.logout.mockReturnValue(
      pending.promise,
    );

    renderApplication(client);

    await screen.findByText(
      "Protected device data",
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "Sign out",
      }),
    );

    expect(
      screen.getByRole("heading", {
        name: "Signing out",
      }),
    ).toBeTruthy();

    expect(
      screen.queryByText(
        "Protected device data",
      ),
    ).toBeNull();

    await act(async () => {
      pending.resolve(undefined);
    });

    expect(
      screen.getByRole("heading", {
        name: "Administrator sign in",
      }),
    ).toBeTruthy();
  });

  it("does not restore protected content after uncertain logout", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    client.logout.mockRejectedValue(
      httpError(503),
    );

    renderApplication(client);

    await screen.findByText(
      "Protected device data",
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "Sign out",
      }),
    );

    await screen.findByRole("heading", {
      name: "Authentication unavailable",
    });

    expect(
      screen.queryByText(
        "Protected device data",
      ),
    ).toBeNull();
  });
});

describe("Authenticated application generations", () => {
  it("creates a fresh query cache after another login", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    client.logout.mockResolvedValue(
      undefined,
    );

    client.login.mockResolvedValue(
      session,
    );

    const {
      queryClients,
      fetchProtected,
    } = renderApplication(client);

    await screen.findByText(
      "Protected device data",
    );

    const originalClient =
      queryClients[0];

    expect(originalClient).toBeDefined();

    act(() => {
      originalClient?.setQueryData(
        protectedQueryKey,
        "Previous session data",
      );
    });

    await screen.findByText(
      "Previous session data",
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "Sign out",
      }),
    );

    await screen.findByRole("heading", {
      name: "Administrator sign in",
    });

    submitLogin();

    await screen.findByText(
      "Protected device data",
    );

    expect(queryClients).toHaveLength(2);

    expect(
      queryClients[1],
    ).not.toBe(originalClient);

    expect(
      queryClients[1]?.getQueryData(
        protectedQueryKey,
      ),
    ).toBe("Protected device data");

    expect(
      screen.queryByText(
        "Previous session data",
      ),
    ).toBeNull();

    expect(
      fetchProtected,
    ).toHaveBeenCalledTimes(2);
  });

  it("replaces the query cache after rapid session revalidation", async () => {
    const client = createClient();

    // Both restorations return identical
    // metadata. Session identity alone
    // cannot determine cache ownership.
    client.getSession.mockResolvedValue(
      session,
    );

    const {
      queryClients,
      mounted,
      fetchProtected,
    } = renderApplication(client);

    await screen.findByText(
      "Protected device data",
    );

    const originalClient =
      queryClients[0];

    const notify =
      captureManagementUnauthorized();

    await act(async () => {
      notify?.();
    });

    await waitFor(() => {
      expect(
        fetchProtected,
      ).toHaveBeenCalledTimes(2);
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(
      mounted,
    ).toHaveBeenCalledTimes(2);

    expect(queryClients).toHaveLength(2);

    expect(
      queryClients[1],
    ).not.toBe(originalClient);
  });
});

describe("Management authentication observer binding", () => {
  it("removes the observer when the authentication boundary unmounts", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    const {
      unmount,
    } = renderApplication(client);

    await screen.findByText(
      "Protected device data",
    );

    expect(
      captureManagementUnauthorized(),
    ).toBeDefined();

    unmount();

    expect(
      captureManagementUnauthorized(),
    ).toBeUndefined();
  });

  it("does not remove a replacement binding during obsolete cleanup", () => {
    const firstNotification = vi.fn();
    const secondNotification = vi.fn();

    const releaseFirst =
      bindManagementUnauthorized(
        () => firstNotification,
      );

    const releaseSecond =
      bindManagementUnauthorized(
        () => secondNotification,
      );

    try {
      releaseFirst();

      expect(
        captureManagementUnauthorized(),
      ).toBe(secondNotification);
    } finally {
      releaseSecond();
      releaseFirst();
    }

    expect(
      captureManagementUnauthorized(),
    ).toBeUndefined();
  });
});
describe("Activity renewal cache preservation", () => {
  it("preserves the protected application and cache after activity renewal", async () => {
    const client = createClient();

    client.getSession.mockResolvedValue(
      session,
    );

    client.recordActivity.mockResolvedValue({
      ...session,

      last_activity_at:
        "2026-11-27T12:10:00Z",

      idle_expires_at:
        "2026-11-27T12:40:00Z",
    });

    const {
      queryClients,
      fetchProtected,
      mounted,
      unmounted,
    } = renderApplication(client);

    await screen.findByText(
      "Protected device data",
    );

    const originalClient =
      queryClients[0];

    expect(originalClient).toBeDefined();

    act(() => {
      originalClient?.setQueryData(
        protectedQueryKey,
        "Active session cached data",
      );
    });

    await screen.findByText(
      "Active session cached data",
    );

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", {
          name: "Record test activity",
        }),
      );
    });

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    expect(mounted).toHaveBeenCalledOnce();

    expect(
      unmounted,
    ).not.toHaveBeenCalled();

    expect(queryClients).toHaveLength(1);

    expect(queryClients[0]).toBe(
      originalClient,
    );

    expect(
      originalClient?.getQueryData(
        protectedQueryKey,
      ),
    ).toBe(
      "Active session cached data",
    );

    expect(
      fetchProtected,
    ).toHaveBeenCalledOnce();

    expect(
      screen.getByText(
        "Active session cached data",
      ),
    ).toBeTruthy();
  });
});