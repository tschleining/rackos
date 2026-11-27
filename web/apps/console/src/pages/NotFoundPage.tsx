
import { ArrowLeft } from "lucide-react";

import { Link } from "react-router";

export function NotFoundPage() {
  return (
    <section className="flex min-h-[60vh] flex-col items-center justify-center px-4 text-center">
      <p className="text-6xl font-semibold tracking-tight text-rack-accent">
        404
      </p>

      <h1 className="mt-5 text-2xl font-semibold">
        Page not found
      </h1>

      <p className="mt-3 max-w-md text-sm text-rack-muted">
        The requested RackOS page does not exist.
      </p>

      <Link
        to="/"
        className="mt-8 inline-flex items-center gap-2 rounded-lg border border-rack-border bg-rack-surface px-5 py-3 text-sm font-medium hover:border-rack-accent"
      >
        <ArrowLeft
          size={18}
          aria-hidden="true"
        />

        Return to overview
      </Link>
    </section>
  );
}
