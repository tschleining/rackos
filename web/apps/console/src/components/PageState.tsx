import {
  CircleAlert,
  Inbox,
  RefreshCw,
} from "lucide-react";

import type { LucideIcon } from "lucide-react";

interface PageStateProps {
  title: string;
  description: string;
  icon?: LucideIcon;
  loading?: boolean;
  error?: boolean;
  onRetry?: () => void;
  retryDisabled?: boolean;
}

export function PageState({
  title,
  description,
  icon,
  loading = false,
  error = false,
  onRetry,
  retryDisabled = false,
}: PageStateProps) {
  const Icon = loading
    ? RefreshCw
    : error
      ? CircleAlert
      : icon ?? Inbox;

  return (
    <section
      role={
        error
          ? "alert"
          : loading
            ? "status"
            : "region"
      }
      aria-label={title}
      className="rounded-xl border border-rack-border bg-rack-surface px-6 py-14 text-center"
    >
      <Icon
        size={36}
        aria-hidden="true"
        className={[
          "mx-auto mb-4",
          error
            ? "text-rack-danger"
            : "text-rack-muted",
          loading
            ? "motion-safe:animate-spin"
            : "",
        ].join(" ")}
      />

      <h3 className="font-medium">
        {title}
      </h3>

      <p className="mx-auto mt-2 max-w-lg text-sm leading-relaxed text-rack-muted">
        {description}
      </p>

      {onRetry && (
        <button
          type="button"
          disabled={retryDisabled || loading}
          onClick={onRetry}
          className="mt-5 rounded-lg border border-rack-border px-4 py-2 text-sm font-medium text-rack-accent hover:border-rack-accent disabled:cursor-not-allowed disabled:opacity-50"
        >
          Try again
        </button>
      )}
    </section>
  );
}