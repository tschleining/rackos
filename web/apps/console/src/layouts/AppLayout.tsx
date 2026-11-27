
import {
  LayoutDashboard,
  Server,
  Settings,
} from "lucide-react";

import {
  Link,
  NavLink,
  Outlet,
  useLocation,
} from "react-router";

import {
  ApplianceStatus,
} from "../components/ApplianceStatus";

import {
  useApplianceVersion,
} from "../hooks/useAppliance";

import {
  ErrorBoundary,
} from "../components/ErrorBoundary";


const navigation = [
  {
    label: "Overview",
    path: "/",
    icon: LayoutDashboard,
    end: true,
  },
  {
    label: "Devices",
    path: "/devices",
    icon: Server,
    end: false,
  },
  {
    label: "System",
    path: "/system",
    icon: Settings,
    end: true,
  },
];

export function AppLayout() {
  const version = useApplianceVersion();
  const location = useLocation();

  // Existing return statement follows.

  return (
    <div className="min-h-screen bg-rack-background text-rack-primary lg:grid lg:grid-cols-[16rem_minmax(0,1fr)]">

      <a
        href="#main-content"
        className="sr-only z-50 rounded-md bg-rack-surface p-3 focus:not-sr-only focus:fixed focus:top-4 focus:left-4"
      >
        Skip to main content
      </a>

      <aside className="flex flex-col border-b border-rack-border bg-rack-surface lg:sticky lg:top-0 lg:h-screen lg:border-r lg:border-b-0">

        <div className="px-5 py-6">
          <Link
            to="/"
            className="inline-flex items-center gap-3 rounded-md"
          >
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-rack-accent/10 text-rack-accent">
              <Server
                size={24}
                aria-hidden="true"
              />
            </div>

            <div>
              <span className="block text-xl font-semibold tracking-tight">
                RackOS
              </span>

              <span className="block text-xs text-rack-muted">
                Management Console
              </span>
            </div>
          </Link>
        </div>

        <nav
          aria-label="Main navigation"
          className="px-3 pb-4 lg:mt-4"
        >
          <p className="mb-3 hidden px-3 text-xs font-semibold tracking-widest text-rack-muted uppercase lg:block">
            Workspace
          </p>

          <div className="flex flex-wrap gap-1 lg:flex-col">
            {navigation.map((item) => {
              const Icon = item.icon;

              return (
                <NavLink
                  key={item.path}
                  to={item.path}
                  end={item.end}
                  className={({ isActive }) =>
                    [
                      "flex min-h-11 items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium",
                      "transition-colors motion-reduce:transition-none",
                      isActive
                        ? "bg-rack-accent/10 text-rack-accent"
                        : "text-rack-muted hover:bg-rack-background hover:text-rack-primary",
                    ].join(" ")
                  }
                >
                  <Icon
                    size={19}
                    aria-hidden="true"
                  />

                  <span>{item.label}</span>
                </NavLink>
              );
            })}
          </div>
        </nav>

        <footer className="mt-auto hidden border-t border-rack-border px-5 py-5 lg:block">
          <p className="text-xs font-medium text-rack-muted">
            {version.data
              ? `RackOS ${version.data.version}`
              : "RackOS"}
          </p>

          <p className="mt-1 text-xs text-rack-muted">
            Pre-alpha development
          </p>
        </footer>
      </aside>

      <div className="min-w-0">
        <header className="flex min-h-16 flex-wrap items-center justify-between gap-3 border-b border-rack-border bg-rack-surface px-5 py-3 sm:px-8">
          <p className="text-sm font-medium text-rack-muted">
            Local appliance
          </p>

          <ApplianceStatus />
        </header>

        <main
          id="main-content"
          tabIndex={-1}
          className="mx-auto w-full max-w-[1600px] px-4 py-6 sm:px-8 sm:py-8"
        >
          <ErrorBoundary resetKey={location.pathname}>
            <Outlet />
        </ErrorBoundary>
        </main>
      </div>
    </div>
  );
}
