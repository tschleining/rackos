// @vitest-environment jsdom

import {
  act,
  cleanup,
  render,
  screen,
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
  ActivityTracker,
  AuthProvider,
  useAuth,
  type AuthClient,
} from "../src/features/auth";

import {
  isQualifyingActivity,
} from "../src/features/auth/ActivityTracker";

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

function createClient() {
  return {
    getSession:
      vi.fn<AuthClient["getSession"]>()
        .mockResolvedValue(session),

    login:
      vi.fn<AuthClient["login"]>(),

    logout:
      vi.fn<AuthClient["logout"]>(),

    recordActivity:
      vi.fn<AuthClient["recordActivity"]>()
        .mockResolvedValue(session),
  };
}

function TestApplication() {
  const { state } = useAuth();

  return (
    <>
      <output data-testid="auth-state">
        {state.status}
      </output>

      <ActivityTracker />
    </>
  );
}

async function renderApplication(
  client: AuthClient,
  expectedState = "authenticated",
) {
  const result = render(
    <AuthProvider client={client}>
      <TestApplication />
    </AuthProvider>,
  );

  await waitFor(() => {
    expect(
      screen.getByTestId("auth-state")
        .textContent,
    ).toBe(expectedState);
  });

  return result;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("Qualifying administrator activity", () => {
  it.each([
    "pointerdown",
    "click",
    "keydown",
  ])(
    "accepts trusted visible %s events",
    (type) => {
      expect(
        isQualifyingActivity(
          {
            type,
            isTrusted: true,
          },
          "visible",
        ),
      ).toBe(true);
    },
  );

  it.each([
    "pointerdown",
    "click",
    "keydown",
  ])(
    "rejects synthetic %s events",
    (type) => {
      expect(
        isQualifyingActivity(
          {
            type,
            isTrusted: false,
          },
          "visible",
        ),
      ).toBe(false);
    },
  );

  it.each([
    "pointerdown",
    "click",
    "keydown",
  ])(
    "rejects hidden-tab %s events",
    (type) => {
      expect(
        isQualifyingActivity(
          {
            type,
            isTrusted: true,
          },
          "hidden",
        ),
      ).toBe(false);
    },
  );

  it("rejects repeating keydown events", () => {
    expect(
      isQualifyingActivity(
        {
          type: "keydown",
          isTrusted: true,
          repeat: true,
        },
        "visible",
      ),
    ).toBe(false);
  });

  it.each([
    "mousemove",
    "pointermove",
    "scroll",
    "focus",
    "blur",
    "visibilitychange",
    "load",
    "resize",
    "pageshow",
  ])(
    "does not qualify %s as activity",
    (type) => {
      expect(
        isQualifyingActivity(
          {
            type,
            isTrusted: true,
          },
          "visible",
        ),
      ).toBe(false);
    },
  );
});

describe("Administrator activity tracker", () => {
  it("does not renew activity when initially mounted", async () => {
    const client = createClient();

    await renderApplication(client);

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();
  });

  it("ignores programmatically dispatched browser events", async () => {
    const client = createClient();

    vi.spyOn(
      document,
      "visibilityState",
      "get",
    ).mockReturnValue("visible");

    await renderApplication(client);

    await act(async () => {
      window.dispatchEvent(
        new Event("pointerdown"),
      );

      window.dispatchEvent(
        new MouseEvent("click"),
      );

      window.dispatchEvent(
        new KeyboardEvent("keydown"),
      );
    });

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();
  });

  it("does not attach activity listeners for anonymous sessions", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      new APIError(
        "http",
        "Authentication required",
        401,
      ),
    );

    const addListener = vi.spyOn(
      window,
      "addEventListener",
    );

    await renderApplication(
      client,
      "anonymous",
    );

    for (const type of [
      "pointerdown",
      "click",
      "keydown",
    ]) {
      expect(
        addListener.mock.calls.some(
          (call) => call[0] === type,
        ),
      ).toBe(false);
    }

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();
  });

  it("removes its listeners when unmounted", async () => {
    const client = createClient();

    const addListener = vi.spyOn(
      window,
      "addEventListener",
    );

    const removeListener = vi.spyOn(
      window,
      "removeEventListener",
    );

    const { unmount } =
      await renderApplication(client);

    const types = [
      "pointerdown",
      "click",
      "keydown",
    ];

    const activityListeners =
      addListener.mock.calls.filter(
        (call) =>
          types.includes(call[0]),
      );

    expect(
      activityListeners,
    ).toHaveLength(3);

    unmount();

    for (const [
      type,
      listener,
    ] of activityListeners) {
      expect(
        removeListener,
      ).toHaveBeenCalledWith(
        type,
        listener,
        true,
      );
    }
  });
});