import {
  useEffect,
  useId,
  useRef,
  useState,
  type FormEvent,
} from "react";

import {
  CircleAlert,
  Lock,
  RefreshCw,
  Server,
  ShieldCheck,
  User,
} from "lucide-react";

import {
  useAuth,
} from "./AuthContext";

import {
  describeLoginFailure,
} from "./loginErrors";

export function LoginPage() {
  const {
    state,
    login,
  } = useAuth();

  const headingID = useId();
  const usernameID = useId();
  const passwordID = useId();
  const errorID = useId();

  const usernameInput =
    useRef<HTMLInputElement>(null);

  const passwordInput =
    useRef<HTMLInputElement>(null);

  const submitting = useRef(false);

  const retryDeadline = useRef(0);

  const [error, setError] =
    useState<string | null>(null);

  const [
    retrySeconds,
    setRetrySeconds,
  ] = useState(0);

  const busy =
    state.status === "authenticating";

  const cooldownActive =
    retrySeconds > 0;

  const disabled =
    busy || cooldownActive;

  /**
   * Use a monotonic clock so changes to the
   * browser's wall clock do not extend or
   * shorten the local retry countdown.
   */
  useEffect(() => {
    if (!cooldownActive) {
      return;
    }

    const update = () => {
      const remaining = Math.max(
        0,
        Math.ceil(
          (
            retryDeadline.current -
            performance.now()
          ) / 1000,
        ),
      );

      setRetrySeconds(remaining);
    };

    const timer = window.setInterval(
      update,
      250,
    );

    return () => {
      window.clearInterval(timer);
    };
  }, [cooldownActive]);

  async function handleSubmit(
    event: FormEvent<HTMLFormElement>,
  ): Promise<void> {
    event.preventDefault();

    if (
      submitting.current ||
      state.status !== "anonymous" ||
      performance.now() <
        retryDeadline.current
    ) {
      return;
    }

    const username =
      usernameInput.current?.value ?? "";

    const password =
      passwordInput.current?.value ?? "";

    if (
      username.length === 0 ||
      password.length === 0
    ) {
      setError(
        "Enter your username and password.",
      );

      return;
    }

    submitting.current = true;

    setError(null);

    try {
      // Preserve the submitted credential
      // values exactly. The appliance owns
      // normalization and validation.
      await login({
        username,
        password,
      });
    } catch (error) {
      const failure =
        describeLoginFailure(error);

      setError(failure.message);

      if (
        failure.retryAfterSeconds > 0
      ) {
        retryDeadline.current =
          performance.now() +
          failure.retryAfterSeconds * 1000;

        setRetrySeconds(
          failure.retryAfterSeconds,
        );
      }
    } finally {
      // Preserve the username for retries,
      // but clear the password after every
      // completed authentication attempt.
      if (passwordInput.current) {
        passwordInput.current.value = "";
      }

      submitting.current = false;
    }
  }

  // The application authentication boundary
  // will render separate checking, unavailable
  // and authenticated states.
  if (
    state.status !== "anonymous" &&
    state.status !== "authenticating"
  ) {
    return null;
  }

  const inputClassName = [
    "block min-h-11 w-full rounded-lg",
    "border border-rack-border",
    "bg-rack-background",
    "py-3 pr-3 pl-10",
    "text-sm text-rack-primary",
    "placeholder:text-rack-muted",
    "disabled:cursor-not-allowed",
    "disabled:opacity-60",
  ].join(" ");

  return (
    <main className="flex min-h-screen items-center justify-center bg-rack-background px-4 py-12 text-rack-primary">
      <div className="w-full max-w-md">
        <header className="mb-8 flex items-center gap-3">
          <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-rack-accent/10 text-rack-accent">
            <Server
              size={28}
              aria-hidden="true"
            />
          </div>

          <div>
            <p className="text-2xl font-semibold tracking-tight">
              RackOS
            </p>

            <p className="text-sm text-rack-muted">
              Management Console
            </p>
          </div>
        </header>

        <section
          aria-labelledby={headingID}
          className="rounded-xl border border-rack-border bg-rack-surface p-6 shadow-xl sm:p-8"
        >
          <h1
            id={headingID}
            className="text-xl font-semibold tracking-tight"
          >
            Administrator sign in
          </h1>

          <p className="mt-2 text-sm leading-relaxed text-rack-muted">
            Sign in to manage your local appliance.
          </p>

          <form
            method="post"
            aria-labelledby={headingID}
            aria-busy={busy}
            onSubmit={(event) => {
              void handleSubmit(event);
            }}
            className="mt-7 space-y-5"
          >
            {error && (
              <div
                id={errorID}
                role="alert"
                className="flex items-start gap-3 rounded-lg border border-rack-danger/30 bg-rack-danger/10 p-3 text-sm text-rack-danger"
              >
                <CircleAlert
                  size={18}
                  aria-hidden="true"
                  className="mt-0.5 shrink-0"
                />

                <p className="leading-relaxed">
                  {error}
                </p>
              </div>
            )}

            <div>
              <label
                htmlFor={usernameID}
                className="mb-2 block text-sm font-medium"
              >
                Username
              </label>

              <div className="relative">
                <User
                  size={18}
                  aria-hidden="true"
                  className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-rack-muted"
                />

                <input
                  ref={usernameInput}
                  id={usernameID}
                  name="username"
                  type="text"
                  autoComplete="username"
                  autoCapitalize="none"
                  spellCheck={false}
                  required
                  disabled={busy}
                  aria-describedby={
                    error
                      ? errorID
                      : undefined
                  }
                  placeholder="Enter username"
                  className={inputClassName}
                />
              </div>
            </div>

            <div>
              <label
                htmlFor={passwordID}
                className="mb-2 block text-sm font-medium"
              >
                Password
              </label>

              <div className="relative">
                <Lock
                  size={18}
                  aria-hidden="true"
                  className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-rack-muted"
                />

                <input
                  ref={passwordInput}
                  id={passwordID}
                  name="password"
                  type="password"
                  autoComplete="current-password"
                  autoCapitalize="none"
                  spellCheck={false}
                  required
                  disabled={busy}
                  aria-describedby={
                    error
                      ? errorID
                      : undefined
                  }
                  placeholder="Enter password"
                  className={inputClassName}
                />
              </div>
            </div>

            <button
              type="submit"
              disabled={disabled}
              className="flex min-h-11 w-full items-center justify-center gap-2 rounded-lg bg-rack-accent px-4 py-3 text-sm font-semibold text-rack-background transition-colors hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50 motion-reduce:transition-none"
            >
              {busy && (
                <RefreshCw
                  size={17}
                  aria-hidden="true"
                  className="motion-safe:animate-spin"
                />
              )}

              {busy
                ? "Signing in..."
                : "Sign in"}
            </button>

            {cooldownActive && (
              <p className="text-center text-sm text-rack-warning">
                Try again in {retrySeconds}{" "}
                {retrySeconds === 1
                  ? "second"
                  : "seconds"}.
              </p>
            )}
          </form>

          <div className="mt-7 flex items-center justify-center gap-2 border-t border-rack-border pt-5 text-xs text-rack-muted">
            <ShieldCheck
              size={15}
              aria-hidden="true"
            />

            <span>
              Local administrator access
            </span>
          </div>
        </section>

        <footer className="mt-6 text-center text-xs text-rack-muted">
          RackOS · Pre-alpha development
        </footer>
      </div>
    </main>
  );
}