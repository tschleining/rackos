
import {
  useApplianceHealth,
  useApplianceReadiness,
} from "../hooks/useAppliance";

type StatusTone =
  | "success"
  | "warning"
  | "danger"
  | "neutral";

interface StatusPresentation {
  label: string;
  tone: StatusTone;
}

const toneClasses: Record<StatusTone, string> = {
  success: "bg-rack-success",
  warning: "bg-rack-warning",
  danger: "bg-rack-danger",
  neutral: "bg-rack-muted",
};

export function ApplianceStatus() {
  const health = useApplianceHealth();
  const readiness = useApplianceReadiness();

  let status: StatusPresentation;

  if (health.isPaused || readiness.isPaused) {
    status = {
      label: "Connection paused",
      tone: "warning",
    };
  } else if (health.isError) {
    status = {
      label: "Appliance unavailable",
      tone: "danger",
    };
  } else if (readiness.isError) {
    status = {
      label: "Readiness unknown",
      tone: "warning",
    };
  } else if (
    health.isPending ||
    readiness.isPending
  ) {
    status = {
      label: "Connecting",
      tone: "neutral",
    };
  } else if (
    readiness.data?.status === "not_ready"
  ) {
    status = {
      label: "Appliance not ready",
      tone: "warning",
    };
  } else if (
    health.data?.status === "healthy" &&
    readiness.data?.status === "ready"
  ) {
    status = {
      label: "Appliance ready",
      tone: "success",
    };
  } else {
    status = {
      label: "Status unknown",
      tone: "neutral",
    };
  }

  return (
    <div
      role="status"
      aria-live="polite"
      className="flex items-center gap-2"
    >
      <span
        aria-hidden="true"
        className={`h-2.5 w-2.5 shrink-0 rounded-full ${toneClasses[status.tone]}`}
      />

      <span className="text-sm text-rack-muted">
        {status.label}
      </span>
    </div>
  );
}
