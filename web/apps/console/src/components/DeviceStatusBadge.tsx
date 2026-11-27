
import type {
  DeviceStatus,
} from "@rackos/api-client";

interface DeviceStatusBadgeProps {
  status: DeviceStatus;
}

export function DeviceStatusBadge({
  status,
}: DeviceStatusBadgeProps) {
  const online = status === "online";

  return (
    <span
      className={[
        "inline-flex w-fit items-center gap-2",
        "rounded-full px-2.5 py-1",
        "text-xs font-medium",
        online
          ? "bg-rack-success/10 text-rack-success"
          : "bg-rack-danger/10 text-rack-danger",
      ].join(" ")}
    >
      <span
        aria-hidden="true"
        className={[
          "h-1.5 w-1.5 rounded-full",
          online
            ? "bg-rack-success"
            : "bg-rack-danger",
        ].join(" ")}
      />

      {online ? "Online" : "Offline"}
    </span>
  );
}
