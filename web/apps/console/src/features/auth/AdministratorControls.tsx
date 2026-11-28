import {
  useContext,
} from "react";

import {
  LogOut,
  ShieldCheck,
} from "lucide-react";

import {
  AuthContext,
} from "./AuthContext";

export function AdministratorControls() {
  const auth = useContext(AuthContext);

  // Preserve compatibility with the existing
  // unauthenticated development console.
  if (
    auth === null ||
    auth.state.status !== "authenticated"
  ) {
    return null;
  }

  function handleLogout(): void {
    if (auth === null) {
      return;
    }

    void auth.logout().catch(() => {
      // AuthProvider and AuthBoundary handle
      // uncertain logout outcomes.
    });
  }

  return (
    <div className="flex items-center gap-3">
      <div className="hidden items-center gap-2 text-sm text-rack-muted sm:flex">
        <ShieldCheck
          size={17}
          aria-hidden="true"
          className="text-rack-success"
        />

        <span>Administrator</span>
      </div>

      <button
        type="button"
        onClick={handleLogout}
        className="inline-flex min-h-10 items-center justify-center gap-2 rounded-lg border border-rack-border px-3 py-2 text-sm font-medium text-rack-muted transition-colors hover:border-rack-danger hover:text-rack-danger motion-reduce:transition-none"
      >
        <LogOut
          size={16}
          aria-hidden="true"
        />

        <span>Sign out</span>
      </button>
    </div>
  );
}