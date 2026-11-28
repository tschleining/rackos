import {
  useLayoutEffect,
  type PropsWithChildren,
} from "react";

import {
  CircleAlert,
  RefreshCw,
  Server,
  ShieldCheck,
} from "lucide-react";

import {
  bindManagementUnauthorized,
} from "../../app/services";

import {
  useAuth,
} from "./AuthContext";

import {
  LoginPage,
} from "./LoginPage";

import {
  ProtectedQueryProvider,
} from "./ProtectedQueryProvider";

interface AuthenticationStatusProps {
  title: string;
  description: string;
  loading?: boolean;
  onRetry?: () => void;
}

function AuthenticationStatus({
  title,
  description,
  loading = false,
  onRetry,
}: AuthenticationStatusProps) {
  const Icon = loading
    ? RefreshCw
    : CircleAlert;

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
          aria-labelledby="authentication-status-heading"
          className="rounded-xl border border-rack-border bg-rack-surface p-8 text-center shadow-xl"
        >
          <div
            role={
              loading
                ? "status"
                : "alert"
            }
          >
            <Icon
              size={36}
              aria-hidden="true"
              className={[
                "mx-auto mb-5",
                loading
                  ? "text-rack-accent motion-safe:animate-spin"
                  : "text-rack-warning",
              ].join(" ")}
            />

            <h1
              id="authentication-status-heading"
              className="text-xl font-semibold"
            >
              {title}
            </h1>

            <p className="mt-3 text-sm leading-relaxed text-rack-muted">
              {description}
            </p>
          </div>

          {onRetry && (
            <button
              type="button"
              onClick={onRetry}
              className="mt-6 flex min-h-11 w-full items-center justify-center gap-2 rounded-lg border border-rack-border px-4 py-3 text-sm font-medium text-rack-accent hover:border-rack-accent"
            >
              <RefreshCw
                size={16}
                aria-hidden="true"
              />

              Check session again
            </button>
          )}

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
      </div>
    </main>
  );
}

/**
 * Prevent protected application components
 * from mounting until authentication has
 * been established.
 */
export function AuthBoundary({
  children,
}: PropsWithChildren) {
  const {
    state,
    sessionGeneration,
    restoreSession,
    captureManagementUnauthorized,
  } = useAuth();

  // Register the observer before protected
  // management components begin querying.
  useLayoutEffect(
    () =>
      bindManagementUnauthorized(
        captureManagementUnauthorized,
      ),
    [captureManagementUnauthorized],
  );

  switch (state.status) {
    case "checking":
      return (
        <AuthenticationStatus
          title="Checking session"
          description="Verifying your administrator session with the appliance."
          loading
        />
      );

    case "anonymous":
    case "authenticating":
      return <LoginPage />;

    case "signing-out":
      return (
        <AuthenticationStatus
          title="Signing out"
          description="Revoking your current administrator session."
          loading
        />
      );

    case "unavailable":
      return (
        <AuthenticationStatus
          title="Authentication unavailable"
          description="RackOS could not verify your administrator session. Management access remains locked until your session can be verified."
          onRetry={() => {
            void restoreSession();
          }}
        />
      );

    case "authenticated":
    return (
        <ProtectedQueryProvider
        key={sessionGeneration}
        >
        {children}
        </ProtectedQueryProvider>
    );
  }
}