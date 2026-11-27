
import type { LucideIcon } from "lucide-react";

type StatTone =
  | "neutral"
  | "success"
  | "danger";

interface DeviceStatCardProps {
  label: string;
  value: number | undefined;
  description: string;
  icon: LucideIcon;
  tone?: StatTone;
}

const toneClasses: Record<StatTone, string> = {
  neutral: "text-rack-primary",
  success: "text-rack-success",
  danger: "text-rack-danger",
};

export function DeviceStatCard({
  label,
  value,
  description,
  icon: Icon,
  tone = "neutral",
}: DeviceStatCardProps) {
  return (
    <section className="min-w-0 rounded-xl border border-rack-border bg-rack-surface p-5 sm:p-6">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-medium text-rack-muted">
          {label}
        </h2>

        <Icon
          size={20}
          aria-hidden="true"
          className={toneClasses[tone]}
        />
      </div>

      <p
        className={[
          "mt-4 text-3xl font-semibold tracking-tight tabular-nums",
          value === undefined
            ? "text-rack-muted"
            : toneClasses[tone],
        ].join(" ")}
      >
        {value === undefined
          ? "—"
          : value.toLocaleString()}
      </p>

      <p className="mt-2 text-xs leading-relaxed text-rack-muted">
        {description}
      </p>
    </section>
  );
}
