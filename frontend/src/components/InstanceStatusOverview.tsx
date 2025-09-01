import React from "react";
import { CheckCircle, RefreshCcw, XCircle } from "lucide-react";
import { cn } from "@/lib/utils.js";

type Labels = {
  upToDate?: string;
  updatesAvailable?: string;
  failed?: string;
};

export type InstanceStatusOverviewProps = {
  upToDate: number;
  updatesAvailable: number;
  failed: number;
  labels?: Labels;
  className?: string;
};

function StatCard({
  icon,
  value,
  label,
  color,
}: {
  icon: React.ReactNode;
  value: number;
  label: string;
  color: "emerald" | "amber" | "red";
}) {
  const colorClasses = {
    emerald: {
      icon: "text-emerald-600",
      value: "text-emerald-700",
    },
    amber: {
      icon: "text-amber-600",
      value: "text-amber-700",
    },
    red: {
      icon: "text-red-600",
      value: "text-red-700",
    },
  } as const;

  const c = colorClasses[color];

  return (
    <div className="rounded-md border p-sm shadow-sm">
      <div className="flex items-center gap-sm">
        <span className={cn("shrink-0", c.icon)} aria-hidden>
          {icon}
        </span>
        <div className="flex items-baseline gap-xs">
          <span className={cn("text-3xl font-bold leading-none", c.value)}>
            {value}
          </span>
          <span className="text-sm text-muted-foreground">{label}</span>
        </div>
      </div>
    </div>
  );
}

export default function InstanceStatusOverview({
  upToDate,
  updatesAvailable,
  failed,
  labels,
  className,
}: InstanceStatusOverviewProps) {
  const l: Required<Labels> = {
    upToDate: labels?.upToDate ?? "Up to date",
    updatesAvailable: labels?.updatesAvailable ?? "Updates available",
    failed: labels?.failed ?? "Failed",
  } as Required<Labels>;

  return (
    <div
      className={cn(
        "grid grid-cols-1 gap-sm sm:grid-cols-3",
        className,
      )}
      role="region"
      aria-label="Instance status overview"
    >
      <StatCard
        icon={<CheckCircle className="h-6 w-6" />}
        value={upToDate}
        label={l.upToDate}
        color="emerald"
      />
      <StatCard
        icon={<RefreshCcw className="h-6 w-6" />}
        value={updatesAvailable}
        label={l.updatesAvailable}
        color="amber"
      />
      <StatCard
        icon={<XCircle className="h-6 w-6" />}
        value={failed}
        label={l.failed}
        color="red"
      />
    </div>
  );
}

