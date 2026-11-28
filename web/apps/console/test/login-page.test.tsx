// @vitest-environment jsdom

import {
  act,
  cleanup,
  fireEvent,
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
  AuthProvider,
  LoginPage,
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
  retryAfterSeconds: number | null = null,
): APIError {
  return new APIError(
    "http",
    "Private diagnostic information",
    status,
    "Private server error",
    retryAfterSeconds,
  );
}

function createClient() {
  return {
    getSession:
      vi.fn<AuthClient["getSession"]>()
        .mockRejectedValue(
          httpError(401),
        ),

    login:
      vi.fn<AuthClient["login"]>(),

    logout:
      vi.fn<AuthClient["logout"]>(),

    recordActivity:
      vi.fn<AuthClient["recordActivity"]>(),
  };
}

function TestApplication() {
  const { state } = useAuth();

  return (
    <>
      <output data-testid="auth-state">
        {state.status}
      </output>

      <LoginPage />
    </>
  );
}

async function renderLogin(
  client: AuthClient,
) {
  const result = render(
    <AuthProvider client={client}>
      <TestApplication />
    </AuthProvider>,
  );

  await screen.findByRole(
    "heading",
    {
      name: "Administrator sign in",
    },
  );

  return result;
}

function getUsername(): HTMLInputElement {
  return screen.getByLabelText(
    "Username",
  ) as HTMLInputElement;
}

function getPassword(): HTMLInputElement {
  return screen.getByLabelText(
    "Password",
  ) as HTMLInputElement;
}

function getSubmit(): HTMLButtonElement {
  return screen.getByRole(
    "button",
    {
      name: /sign(?:ing)? in/i,
    },
  ) as HTMLButtonElement;
}

function getForm(): HTMLElement {
  return screen.getByRole(
    "form",
    {
      name: "Administrator sign in",
    },
  );
}

function enterCredentials(
  username = "admin",
  password = "synthetic-test-password",
): void {
  fireEvent.change(
    getUsername(),
    {
      target: {
        value: username,
      },
    },
  );

  fireEvent.change(
    getPassword(),
    {
      target: {
        value: password,
      },
    },
  );
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("Administrator login page", () => {
  it("renders accessible credential inputs", async () => {
    const client = createClient();

    await renderLogin(client);

    const username = getUsername();
    const password = getPassword();

    expect(
      username.getAttribute("name"),
    ).toBe("username");

    expect(
      username.getAttribute(
        "autocomplete",
      ),
    ).toBe("username");

    expect(
      password.getAttribute("name"),
    ).toBe("password");

    expect(
      password.getAttribute(
        "autocomplete",
      ),
    ).toBe("current-password");

    expect(password.type).toBe(
      "password",
    );

    expect(username.required).toBe(true);
    expect(password.required).toBe(true);

    expect(
      getSubmit().disabled,
    ).toBe(false);

    expect(
      client.login,
    ).not.toHaveBeenCalled();
  });

  it("does not submit missing credentials", async () => {
    const client = createClient();

    await renderLogin(client);

    fireEvent.submit(getForm());

    expect(
      screen.getByRole("alert")
        .textContent,
    ).toContain(
      "Enter your username and password.",
    );

    expect(
      client.login,
    ).not.toHaveBeenCalled();
  });

  it("submits the original credential values", async () => {
    const client = createClient();

    client.login.mockResolvedValue(
      session,
    );

    await renderLogin(client);

    const username = "ADMIN";
    const password =
      " synthetic-test-password ";

    enterCredentials(
      username,
      password,
    );

    fireEvent.submit(getForm());

    await waitFor(() => {
      expect(
        screen.getByTestId("auth-state")
          .textContent,
      ).toBe("authenticated");
    });

    expect(
      client.login,
    ).toHaveBeenCalledWith(
      {
        username,
        password,
      },
      {
        signal: expect.any(AbortSignal),
      },
    );

    expect(
      screen.queryByLabelText(
        "Password",
      ),
    ).toBeNull();

    expect(
      client.login,
    ).toHaveBeenCalledOnce();
  });

  it("disables the form while signing in", async () => {
    const client = createClient();

    let resolveLogin!: (
      value: AdministratorSession,
    ) => void;

    client.login.mockReturnValue(
      new Promise<AdministratorSession>(
        (resolve) => {
          resolveLogin = resolve;
        },
      ),
    );

    await renderLogin(client);

    enterCredentials();

    fireEvent.submit(getForm());

    expect(
      getUsername().disabled,
    ).toBe(true);

    expect(
      getPassword().disabled,
    ).toBe(true);

    expect(
      getSubmit().disabled,
    ).toBe(true);

    expect(
      getSubmit().textContent,
    ).toContain("Signing in");

    expect(
      getForm().getAttribute("aria-busy"),
    ).toBe("true");

    // Simulate repeated submission events,
    // including events that bypass the
    // disabled submit button.
    fireEvent.submit(getForm());
    fireEvent.submit(getForm());

    expect(
      client.login,
    ).toHaveBeenCalledOnce();

    await act(async () => {
      resolveLogin(session);
    });

    expect(
      screen.getByTestId("auth-state")
        .textContent,
    ).toBe("authenticated");
  });

  it("displays a generic credential error and clears the password", async () => {
    const client = createClient();

    client.login.mockRejectedValue(
      httpError(401),
    );

    await renderLogin(client);

    enterCredentials();

    fireEvent.submit(getForm());

    const alert =
      await screen.findByRole("alert");

    expect(alert.textContent).toContain(
      "Incorrect username or password.",
    );

    expect(alert.textContent).not.toContain(
      "Private server error",
    );

    expect(
      getUsername().value,
    ).toBe("admin");

    expect(
      getPassword().value,
    ).toBe("");

    expect(
      getSubmit().disabled,
    ).toBe(false);

    expect(
      screen.getByTestId("auth-state")
        .textContent,
    ).toBe("anonymous");
  });

  it("allows another submission after a rejected login", async () => {
    const client = createClient();

    client.login
      .mockRejectedValueOnce(
        httpError(401),
      )
      .mockResolvedValueOnce(
        session,
      );

    await renderLogin(client);

    enterCredentials();

    fireEvent.submit(getForm());

    await screen.findByRole("alert");

    fireEvent.change(
      getPassword(),
      {
        target: {
          value:
            "synthetic-corrected-password",
        },
      },
    );

    fireEvent.submit(getForm());

    await waitFor(() => {
      expect(
        screen.getByTestId("auth-state")
          .textContent,
      ).toBe("authenticated");
    });

    expect(
      client.login,
    ).toHaveBeenCalledTimes(2);
  });

  it("displays session capacity errors", async () => {
    const client = createClient();

    client.login.mockRejectedValue(
      httpError(409),
    );

    await renderLogin(client);

    enterCredentials();

    fireEvent.submit(getForm());

    const alert =
      await screen.findByRole("alert");

    expect(alert.textContent).toContain(
      "The maximum number of administrator sessions has been reached.",
    );

    expect(
      getPassword().value,
    ).toBe("");
  });

  it("enforces the rate-limit countdown without retrying automatically", async () => {
    const client = createClient();

    client.login.mockRejectedValue(
      httpError(429, 1),
    );

    await renderLogin(client);

    enterCredentials();

    fireEvent.submit(getForm());

    const alert =
      await screen.findByRole("alert");

    expect(alert.textContent).toContain(
      "Too many sign-in attempts.",
    );

    expect(
      getSubmit().disabled,
    ).toBe(true);

    expect(
      screen.getByText(
        /Try again in 1 second/,
      ),
    ).toBeTruthy();

    enterCredentials();

    fireEvent.submit(getForm());

    expect(
      client.login,
    ).toHaveBeenCalledOnce();

    await waitFor(
      () => {
        expect(
          getSubmit().disabled,
        ).toBe(false);
      },
      {
        timeout: 3000,
      },
    );

    expect(
      screen.queryByText(
        /Try again in/,
      ),
    ).toBeNull();

    expect(
      client.login,
    ).toHaveBeenCalledOnce();
  });

  it("does not expose the login form when authentication becomes unavailable", async () => {
    const client = createClient();

    client.login.mockRejectedValue(
      httpError(503),
    );

    await renderLogin(client);

    enterCredentials();

    fireEvent.submit(getForm());

    await waitFor(() => {
      expect(
        screen.getByTestId("auth-state")
          .textContent,
      ).toBe("unavailable");
    });

    expect(
      screen.queryByLabelText(
        "Username",
      ),
    ).toBeNull();

    expect(
      screen.queryByLabelText(
        "Password",
      ),
    ).toBeNull();

    expect(
      client.login,
    ).toHaveBeenCalledOnce();
  });

  it("does not renew activity during login", async () => {
    const client = createClient();

    client.login.mockResolvedValue(
      session,
    );

    await renderLogin(client);

    enterCredentials();

    fireEvent.submit(getForm());

    await waitFor(() => {
      expect(
        screen.getByTestId("auth-state")
          .textContent,
      ).toBe("authenticated");
    });

    expect(
      client.recordActivity,
    ).not.toHaveBeenCalled();
  });
});