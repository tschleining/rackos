import { RefreshCw } from "lucide-react";

interface RefreshButtonProps {
  label?: string;
  isFetching: boolean;
  isPaused?: boolean;
  onRefresh: () => void;
}

export function RefreshButton({
  label = "Refresh",
  isFetching,
  isPaused = false,
  onRefresh,
}: RefreshButtonProps) {
  return (
    <button
      type="button"
      disabled={isFetching || isPaused}
      aria-busy={isFetching}
      onClick={onRefresh}
      className="inline-flex min-h-10 items-center gap-2 rounded-lg border border-rack-border bg-rack-surface px-4 py-2 text-sm font-medium text-rack-muted hover:border-rack-accent hover:text-rack-primary disabled:cursor-not-allowed disabled:opacity-50"
    >
      <RefreshCw
        size={16}
        aria-hidden="true"
        className={
          isFetching
            ? "motion-safe:animate-spin"
            : ""
        }
      />

      {isPaused
        ? "Refresh paused"
        : isFetching
          ? "Refreshing..."
          : label}
    </button>
  );
}