import {
  CircleAlert,
  Info,
  TriangleAlert,
} from "lucide-react";

import type { ReactNode } from "react";

import { describeAPIError } from "../lib/apiErrors";

type NoticeTone =
  | "info"
  | "warning"
  | "error";

interface StatusNoticeProps {
  tone?: NoticeTone;
  children: ReactNode;
}

const noticeClasses: Record<NoticeTone, string> = {
  info: "border-rack-border bg-rack-surface text-rack-muted",
  warning: "border-rack-warning/30 bg-rack-warning/5 text-rack-warning",
  error: "border-rack-danger/30 bg-rack-danger/5 text-rack-danger",
};

const noticeIcons = {
  info: Info,
  warning: TriangleAlert,
  error: CircleAlert,
};

export function StatusNotice({
  tone = "info",
  children,
}: StatusNoticeProps) {
  const Icon = noticeIcons[tone];

  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      className={`mb-6 flex items-start gap-3 rounded-lg border p-4 text-sm leading-relaxed ${noticeClasses[tone]}`}
    >
      <Icon
        size={18}
        aria-hidden="true"
        className="mt-0.5 shrink-0"
      />

      <div className="min-w-0">
        {children}
      </div>
    </div>
  );
}

interface QueryFailureNoticeProps {
  query: {
    isError: boolean;
    isPaused: boolean;
    error: unknown;
  };

  resource: string;
  hasData: boolean;
}

export function QueryFailureNotice({
  query,
  resource,
  hasData,
}: QueryFailureNoticeProps) {
  if (query.isPaused) {
    return (
      <StatusNotice tone="warning">
        Refresh of {resource} is paused.

        {hasData
          ? " Showing last known information."
          : " Information cannot be retrieved until the connection resumes."}
      </StatusNotice>
    );
  }

  if (!query.isError) {
    return null;
  }

  return (
    <StatusNotice
      tone={hasData ? "warning" : "error"}
    >
      <p className="font-medium">
        Unable to {hasData ? "refresh" : "retrieve"}{" "}
        {resource}.
      </p>

      <p className="mt-1">
        {describeAPIError(query.error)}
      </p>

      {hasData && (
        <p className="mt-1">
          Showing last known information.
          Displayed values may be outdated.
        </p>
      )}
    </StatusNotice>
  );
}