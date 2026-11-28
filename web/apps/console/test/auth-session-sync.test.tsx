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
  AuthProvider,
  useAuth,
  type AuthClient,
} from "../src/features/auth";

import {
  openSessionChannel,
} from "../src/features/auth/sessionChannel";

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

const credentials = {
  username: "admin",
  password: "synthetic-test-password",
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

/**
 * Deterministic in-memory implementation
 * of the browser messaging transport.
 */
class TestBroadcastChannel extends EventTarget {
  static channels =
    new Set<TestBroadcastChannel>();

  static messages: unknown[] = [];

  private closed = false;

  constructor(
    readonly name: string,
  ) {
    super();

    TestBroadcastChannel.channels.add(
      this,
    );
  }

  postMessage(message: unknown): void {
    if (this.closed) {
      throw new Error("Channel closed");
    }

    TestBroadcastChannel.messages.push(
      message,
    );

    for (
      const peer of TestBroadcastChannel.channels
    ) {
      if (
        peer === this ||
        peer.name !== this.name ||
        peer.closed
      ) {
        continue;
      }

      queueMicrotask(() => {
        if (!peer.closed) {
          peer.dispatchEvent(
            new MessageEvent("message", {
              data: message,
            }),
          );
        }
      });
    }
  }

  close(): void {
    this.closed = true;

    TestBroadcastChannel.channels.delete(
      this,
    );
  }
}

function deferred<T>() {
  let resolve!: (
    value: T | PromiseLike<T>,
  ) => void;

  const promise = new Promise<T>(
    (resolvePromise) => {
      resolve = resolvePromise;
    },
  );

  return {
    promise,
    resolve,
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
        .mockResolvedValue(session),
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

beforeEach(() => {
  TestBroadcastChannel.messages = [];

  vi.stubGlobal(
    "BroadcastChannel",
    TestBroadcastChannel,
  );
});

afterEach(() => {
  cleanup();

  for (
    const channel of TestBroadcastChannel.channels
  ) {
    channel.close();
  }

  TestBroadcastChannel.messages = [];

  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Session synchronization transport", () => {
  it("sends only an invalidation hint", async () => {
    const firstReceiver = vi.fn();
    const secondReceiver = vi.fn();

    const first = openSessionChannel(
      firstReceiver,
    );

    const second = openSessionChannel(
      secondReceiver,
    );

    expect(first).not.toBeNull();
    expect(second).not.toBeNull();

    first?.notify();

    await Promise.resolve();

    expect(
      TestBroadcastChannel.messages,
    ).toEqual([
      "session-changed",
    ]);

    expect(
      firstReceiver,
    ).not.toHaveBeenCalled();

    expect(
      secondReceiver,
    ).toHaveBeenCalledOnce();
  });

  it("ignores unrecognized messages", async () => {
    const receiver = vi.fn();

    openSessionChannel(receiver);

    const peer = new TestBroadcastChannel(
      "rackos-auth-v1",
    );

    peer.postMessage("unknown-message");

    peer.postMessage({
      status: "authenticated",
      session,
    });

    peer.postMessage(null);

    await Promise.resolve();

    expect(
      receiver,
    ).not.toHaveBeenCalled();
  });

  it("does not receive messages after closure", async () => {
    const receiver = vi.fn();

    const channel =
      openSessionChannel(receiver);

    const peer = new TestBroadcastChannel(
      "rackos-auth-v1",
    );

    channel?.close();
    channel?.close();

    peer.postMessage("session-changed");

    await Promise.resolve();

    expect(
      receiver,
    ).not.toHaveBeenCalled();
  });

  it("handles unsupported browser messaging", () => {
    vi.stubGlobal(
      "BroadcastChannel",
      undefined,
    );

    expect(
      openSessionChannel(vi.fn()),
    ).toBeNull();
  });

  it("handles browser messaging restrictions", () => {
    vi.stubGlobal(
      "BroadcastChannel",
      class {
        constructor() {
          throw new DOMException(
            "Messaging blocked",
            "SecurityError",
          );
        }
      },
    );

    expect(
      openSessionChannel(vi.fn()),
    ).toBeNull();
  });
});

describe("Cross-tab administrator sessions", () => {
  it("synchronizes login and logout without notification loops", async () => {
    let authenticated = false;

    const firstClient = createClient();
    const secondClient = createClient();

    async function getSession() {
      if (!authenticated) {
        throw httpError(401);
      }

      return session;
    }

    firstClient.getSession
      .mockImplementation(getSession);

    secondClient.getSession
      .mockImplementation(getSession);

    firstClient.login.mockImplementation(
      async () => {
        authenticated = true;

        return session;
      },
    );

    firstClient.logout.mockImplementation(
      async () => {
        authenticated = false;
      },
    );

    const first = renderAuth(firstClient);
    const second = renderAuth(secondClient);

    await waitFor(() => {
      expect(
        first.result.current.state.status,
      ).toBe("anonymous");

      expect(
        second.result.current.state.status,
      ).toBe("anonymous");
    });

    await act(async () => {
      await first.result.current.login(
        credentials,
      );
    });

    await waitFor(() => {
      expect(
        second.result.current.state.status,
      ).toBe("authenticated");
    });

    expect(
      first.result.current.state.status,
    ).toBe("authenticated");

    expect(
      secondClient.login,
    ).not.toHaveBeenCalled();

    await act(async () => {
      await first.result.current.logout();
    });

    await waitFor(() => {
      expect(
        second.result.current.state.status,
      ).toBe("anonymous");
    });

    expect(
      first.result.current.state.status,
    ).toBe("anonymous");

    expect(
      secondClient.logout,
    ).not.toHaveBeenCalled();

    expect(
      secondClient.getSession,
    ).toHaveBeenCalledTimes(3);

    expect(
      TestBroadcastChannel.messages,
    ).toEqual([
      "session-changed",
      "session-changed",
    ]);
  });

  it("does not broadcast definitively rejected login attempts", async () => {
    const client = createClient();

    client.getSession.mockRejectedValue(
      httpError(401),
    );

    client.login.mockRejectedValue(
      httpError(401),
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
        status: 401,
      });
    });

    expect(
      TestBroadcastChannel.messages,
    ).toHaveLength(0);
  });

  it("broadcasts uncertain logout outcomes", async () => {
    const client = createClient();

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

    expect(
      result.current.state.status,
    ).toBe("unavailable");

    expect(
      TestBroadcastChannel.messages,
    ).toEqual([
      "session-changed",
    ]);
  });

  it("never broadcasts ordinary session inspection or activity renewal", async () => {
    const client = createClient();

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    await act(async () => {
      await result.current.recordActivity();

      await result.current.restoreSession();
    });

    expect(
      TestBroadcastChannel.messages,
    ).toHaveLength(0);
  });

  it("does not expose an obsolete restoration result after a peer notification", async () => {
    const client = createClient();

    const firstInspection =
      deferred<AdministratorSession>();

    const secondInspection =
      deferred<AdministratorSession>();

    client.getSession
      .mockReturnValueOnce(
        firstInspection.promise,
      )
      .mockReturnValueOnce(
        secondInspection.promise,
      );

    const { result } = renderAuth(client);

    expect(
      result.current.state.status,
    ).toBe("checking");

    const peer = new TestBroadcastChannel(
      "rackos-auth-v1",
    );

    await act(async () => {
      peer.postMessage("session-changed");
      peer.postMessage("session-changed");
      peer.postMessage("session-changed");
    });

    // Notifications must not start
    // overlapping session inspections.
    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    await act(async () => {
      firstInspection.resolve(session);
    });

    // The first response is obsolete.
    // Management access stays locked.
    expect(
      result.current.state.status,
    ).toBe("checking");

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    await act(async () => {
      secondInspection.resolve(session);
    });

    expect(
      result.current.state.status,
    ).toBe("authenticated");

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);
  });

  it("verifies again after login when a peer changes the session during authentication", async () => {
    const client = createClient();

    const pendingLogin =
      deferred<AdministratorSession>();

    client.getSession.mockRejectedValue(
      httpError(401),
    );

    client.login.mockReturnValue(
      pendingLogin.promise,
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

    const peer = new TestBroadcastChannel(
      "rackos-auth-v1",
    );

    await act(async () => {
      peer.postMessage("session-changed");
    });

    expect(
      result.current.state.status,
    ).toBe("authenticating");

    expect(
      client.getSession,
    ).toHaveBeenCalledOnce();

    await act(async () => {
      pendingLogin.resolve(session);

      await operation;
    });

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    expect(
      result.current.state.status,
    ).toBe("anonymous");
  });

  it("continues authenticating when BroadcastChannel is unsupported", async () => {
    vi.stubGlobal(
      "BroadcastChannel",
      undefined,
    );

    const client = createClient();

    const { result } = renderAuth(client);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    await act(async () => {
      await result.current.logout();
    });

    expect(
      result.current.state.status,
    ).toBe("anonymous");
  });

  it("cleans up the obsolete channel during Strict Mode replay", async () => {
    const client = createClient();

    const {
      result,
      unmount,
    } = renderAuth(client, true);

    await waitFor(() => {
      expect(
        result.current.state.status,
      ).toBe("authenticated");
    });

    expect(
      TestBroadcastChannel.channels.size,
    ).toBe(1);

    expect(
      client.getSession,
    ).toHaveBeenCalledTimes(2);

    unmount();

    expect(
      TestBroadcastChannel.channels.size,
    ).toBe(0);
  });
});