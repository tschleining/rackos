// @vitest-environment jsdom

import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";

import {
  afterEach,
  beforeEach,
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
  AdministratorControls,
  AuthProvider,
  SessionLifecycle,
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

function httpError(
  status: number,
): APIError {
  return new APIError(
    "http",
    "Synthetic authentication failure",
    status,
  );
}

function createClient() {
  return {
    getSession:
      vi.fn<AuthClient["getSession"]>()
        .mockResolvedValue(session),

    login:
      vi.fn<AuthClient["login"]>(),

    logout:
      vi.fn<AuthClient["logout"]>()
        .mockResolvedValue(undefined),

    recordActivity:
      vi.fn<AuthClient["recordActivity"]>(),
  };
}

function TestApplication() {
  const {
    state,
    recordActivity,
  } = useAuth();

  return (
    <>
      <output data-testid="auth-state">
        {state.status}
      </output>

      <SessionLifecycle />

      <AdministratorControls />

      {state.status === "authenticated" && (
        <button
          type="button"
          onClick={() => {
            void recordActivity();
          }}
        >
          Record test activity
        </button>
      )}
    </>
  );
}

async function renderApplication(
  client: AuthClient,
) {
  const result = render(
    <AuthProvider client={client}>
      <TestApplication />
    </AuthProvider>,
  );

  await act(async () => {
    // Allow session restoration to complete.
  });

  return result;
}

function currentState(): string | null {
  return screen.getByTestId(
    "auth-state",
  ).textContent;
}

async function advanceTime(
  milliseconds: number,
): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(
      milliseconds,
    );
  });
}

beforeEach(() => {
  vi.useFakeTimers({
    toFake: [
      "setTimeout",
      "clearTimeout",
      "Date",
      "performance",
    ],
  });

  vi.setSystemTime(
    new Date(
      "2026-11-27T12:29:55Z",
    ),
  );

  vi.spyOn(
    document,
    "visibilityState",
    "get",
  ).mockReturnValue("visible");
});

afterEach(() => {
  cleanup();

  vi.useRealTimers();

  vi.restoreAllMocks();
});

describe("Administrator session lifecycle", () => {
  it("does not verify before the inactivity deadline", async () => {
    const client = createClient();

    await renderApplication(client);

    expect(currentState()).toBe(
      "authenticated",
    );

    await advanceTime(4_999);

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();
  });

  it("verifies the session at the inactivity deadline", async () => {
    const client = createClient();

    await renderApplication(client);

    client.getSession.mockRejectedValueOnce(
      httpError(401),
    );

    await advanceTime(5_000);

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(currentState()).toBe(
      "anonymous",
    );

    expect(
      client.logout,
    ).not.toHaveBeenCalled();

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();
  });

  it("verifies the session at its absolute expiration", async () => {
    vi.setSystemTime(
      new Date(
        "2026-11-27T23:59:55Z",
      ),
    );

    const client = createClient();

    const cappedSession = {
      ...session,

      last_activity_at:
        "2026-11-27T23:50:00Z",

      idle_expires_at:
        session.expires_at,
    };

    client.getSession.mockResolvedValueOnce(
      cappedSession,
    );

    await renderApplication(client);

    client.getSession.mockRejectedValueOnce(
      httpError(401),
    );

    await advanceTime(5_000);

    expect(currentState()).toBe(
      "anonymous",
    );

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);
  });

  it("fails closed if deadline verification is unavailable", async () => {
    const client = createClient();

    await renderApplication(client);

    client.getSession.mockRejectedValueOnce(
      httpError(503),
    );

    await advanceTime(5_000);

    expect(currentState()).toBe(
      "unavailable",
    );

    expect(
      screen.queryByRole("button", {
        name: "Sign out",
      }),
    ).toBeNull();
  });

  it("accepts appliance verification despite a conflicting local clock", async () => {
    const client = createClient();

    await renderApplication(client);

    // Simulate an appliance whose clock
    // is behind the browser's clock.
    await advanceTime(5_000);

    expect(currentState()).toBe(
      "authenticated",
    );

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    // Prevent immediate repeated
    // verification of the same deadline.
    await advanceTime(29_999);

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    client.getSession.mockRejectedValueOnce(
      httpError(401),
    );

    await advanceTime(1);

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(3);

    expect(currentState()).toBe(
      "anonymous",
    );
  });

  it("does not extend deadlines after a backward wall-clock change", async () => {
    const client = createClient();

    await renderApplication(client);

    vi.setSystemTime(
      new Date(
        "2026-11-27T11:00:00Z",
      ),
    );

    client.getSession.mockRejectedValueOnce(
      httpError(401),
    );

    await advanceTime(5_000);

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(currentState()).toBe(
      "anonymous",
    );
  });

  it("revalidates authentication when the browser regains focus", async () => {
    const client = createClient();

    await renderApplication(client);

    client.getSession.mockRejectedValueOnce(
      httpError(401),
    );

    await act(async () => {
      window.dispatchEvent(
        new Event("focus"),
      );
    });

    expect(currentState()).toBe(
      "anonymous",
    );

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();
  });

  it("coalesces closely spaced browser resume events", async () => {
    const client = createClient();

    await renderApplication(client);

    await act(async () => {
      window.dispatchEvent(
        new Event("focus"),
      );
    });

    await act(async () => {
      document.dispatchEvent(
        new Event(
          "visibilitychange",
        ),
      );

      window.dispatchEvent(
        new Event("focus"),
      );
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(currentState()).toBe(
      "authenticated",
    );
  });

  it("does not verify merely because the tab becomes hidden", async () => {
    const client = createClient();

    await renderApplication(client);

    vi.spyOn(
      document,
      "visibilityState",
      "get",
    ).mockReturnValue("hidden");

    await act(async () => {
      document.dispatchEvent(
        new Event(
          "visibilitychange",
        ),
      );

      window.dispatchEvent(
        new Event("focus"),
      );
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();
  });

  it("verifies when a hidden tab becomes visible", async () => {
    const client = createClient();

    await renderApplication(client);

    const visibility = vi.spyOn(
      document,
      "visibilityState",
      "get",
    );

    visibility.mockReturnValue(
      "hidden",
    );

    await act(async () => {
      document.dispatchEvent(
        new Event(
          "visibilitychange",
        ),
      );
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    visibility.mockReturnValue(
      "visible",
    );

    await act(async () => {
      document.dispatchEvent(
        new Event(
          "visibilitychange",
        ),
      );
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);
  });

  it("verifies pages restored from the back-forward cache", async () => {
    const client = createClient();

    await renderApplication(client);

    client.getSession.mockRejectedValueOnce(
      httpError(401),
    );

    const event = Object.assign(
      new Event("pageshow"),
      {
        persisted: true,
      },
    );

    await act(async () => {
      window.dispatchEvent(event);
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(currentState()).toBe(
      "anonymous",
    );
  });

  it("does not revalidate anonymous sessions on browser focus", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(401),
    );

    await renderApplication(client);

    await act(async () => {
      window.dispatchEvent(
        new Event("focus"),
      );

      document.dispatchEvent(
        new Event(
          "visibilitychange",
        ),
      );
    });

    await advanceTime(60_000);

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    expect(currentState()).toBe(
      "anonymous",
    );
  });

  it("removes timers and browser listeners when unmounted", async () => {
    const client = createClient();

    const { unmount } =
      await renderApplication(client);

    unmount();

    window.dispatchEvent(
      new Event("focus"),
    );

    document.dispatchEvent(
      new Event(
        "visibilitychange",
      ),
    );

    await advanceTime(60_000);

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();
  });
});

describe("Administrator controls", () => {
  it("renders nothing outside the authentication provider", () => {
    const { container } = render(
      <AdministratorControls />,
    );

    expect(
      container.innerHTML,
    ).toBe("");
  });

  it("renders administrator controls after authentication", async () => {
    const client = createClient();

    await renderApplication(client);

    expect(
      screen.getByText(
        "Administrator",
      ),
    ).toBeTruthy();

    expect(
      screen.getByRole("button", {
        name: "Sign out",
      }),
    ).toBeTruthy();
  });

  it("revokes the current session when signing out", async () => {
    const client = createClient();

    await renderApplication(client);

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", {
          name: "Sign out",
        }),
      );
    });

    expect(
      client.logout,
    ).toHaveBeenCalledOnce();

    expect(currentState()).toBe(
      "anonymous",
    );

    expect(
      screen.queryByRole("button", {
        name: "Sign out",
      }),
    ).toBeNull();
  });

  it("does not report successful revocation after a logout failure", async () => {
    const client = createClient();

    client.logout.mockRejectedValue(
      httpError(503),
    );

    await renderApplication(client);

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", {
          name: "Sign out",
        }),
      );
    });

    expect(currentState()).toBe(
      "unavailable",
    );

    expect(
      client.logout,
    ).toHaveBeenCalledOnce();
  });

  it("stops session deadline monitoring after logout", async () => {
    const client = createClient();

    await renderApplication(client);

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", {
          name: "Sign out",
        }),
      );
    });

    await advanceTime(60_000);

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();

    expect(currentState()).toBe(
      "anonymous",
    );
  });
});
describe("Session deadline regression tests", () => {
  it("does not postpone expiration after recent browser focus verification", async () => {
    const client = createClient();

    await renderApplication(client);

    await act(async () => {
      window.dispatchEvent(
        new Event("focus"),
      );
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(currentState()).toBe(
      "authenticated",
    );

    client.getSession.mockRejectedValueOnce(
      httpError(401),
    );

    await advanceTime(4_999);

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    await advanceTime(1);

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(3);

    expect(currentState()).toBe(
      "anonymous",
    );
  });

  it("reschedules inactivity expiration after successful activity renewal", async () => {
    const client = createClient();

    const renewedSession = {
      ...session,

      last_activity_at:
        "2026-11-27T12:29:55Z",

      idle_expires_at:
        "2026-11-27T12:59:55Z",
    };

    client.recordActivity.mockResolvedValue(
      renewedSession,
    );

    await renderApplication(client);

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

    // Pass the original inactivity deadline.
    await advanceTime(5_000);

    expect(currentState()).toBe(
      "authenticated",
    );

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    // Stop just before the new deadline.
    await advanceTime(
      30 * 60_000 - 5_001,
    );

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    client.getSession.mockRejectedValueOnce(
      httpError(401),
    );

    await advanceTime(1);

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(currentState()).toBe(
      "anonymous",
    );

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();
  });

  it("does not postpone absolute expiration after activity renewal", async () => {
    vi.setSystemTime(
      new Date(
        "2026-11-27T23:59:55Z",
      ),
    );

    const client = createClient();

    const cappedSession = {
      ...session,

      last_activity_at:
        "2026-11-27T23:50:00Z",

      idle_expires_at:
        session.expires_at,
    };

    client.getSession.mockResolvedValueOnce(
      cappedSession,
    );

    client.recordActivity.mockResolvedValue({
      ...cappedSession,

      last_activity_at:
        "2026-11-27T23:59:55Z",
    });

    await renderApplication(client);

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", {
          name: "Record test activity",
        }),
      );
    });

    client.getSession.mockRejectedValueOnce(
      httpError(401),
    );

    await advanceTime(5_000);

    expect(currentState()).toBe(
      "anonymous",
    );

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(
      client.recordActivity,
    ).toHaveBeenCalledOnce();
  });
});