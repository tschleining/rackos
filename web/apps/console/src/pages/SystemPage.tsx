
import {
  RefreshCw,
  Shield,
} from "lucide-react";

import {
  PageHeader,
} from "../components/PageHeader";

import {
  useApplianceVersion,
} from "../hooks/useAppliance";

export function SystemPage() {
  const version = useApplianceVersion();

  return (
    <>
      <PageHeader
        title="System"
        description="Appliance software and local management configuration."
      />

      <div className="grid gap-6 xl:grid-cols-2">
        <section className="rounded-xl border border-rack-border bg-rack-surface p-6">
          <div className="mb-6 flex items-center justify-between gap-3">
            <h2 className="text-lg font-medium">
              Appliance information
            </h2>

            <button
              type="button"
              aria-label="Refresh appliance information"
              disabled={
                version.isFetching ||
                version.isPaused
              }
              onClick={() => {
                void version.refetch();
              }}
              className="rounded-md border border-rack-border p-2 text-rack-muted hover:text-rack-primary disabled:cursor-not-allowed disabled:opacity-50"
            >
              <RefreshCw
                size={16}
                aria-hidden="true"
                className={
                  version.isFetching
                    ? "motion-safe:animate-spin"
                    : ""
                }
              />
            </button>
          </div>

          {version.isPaused ? (
            <p className="text-sm text-rack-warning">
              Connection paused.
            </p>
          ) : version.isError ? (
            <p
              role="alert"
              className="text-sm text-rack-danger"
            >
              Unable to retrieve appliance information.
            </p>
          ) : version.isPending ? (
            <p className="text-sm text-rack-muted">
              Loading appliance information...
            </p>
          ) : (
            <dl className="divide-y divide-rack-border text-sm">
              <div className="flex justify-between gap-4 py-3">
                <dt className="text-rack-muted">
                  Product
                </dt>

                <dd className="break-all text-right font-medium">
                  {version.data.name}
                </dd>
              </div>

              <div className="flex justify-between gap-4 py-3">
                <dt className="text-rack-muted">
                  Appliance version
                </dt>

                <dd className="break-all text-right font-mono">
                  {version.data.version}
                </dd>
              </div>
            </dl>
          )}
        </section>

        <section className="rounded-xl border border-rack-border bg-rack-surface p-6">
          <div className="mb-5 flex items-center gap-3">
            <Shield
              size={20}
              className="text-rack-accent"
              aria-hidden="true"
            />

            <h2 className="text-lg font-medium">
              Management access
            </h2>
          </div>

          <dl className="divide-y divide-rack-border text-sm">
            <div className="flex justify-between gap-4 py-3">
              <dt className="text-rack-muted">
                Access mode
              </dt>

              <dd>Local only</dd>
            </div>

            <div className="flex justify-between gap-4 py-3">
              <dt className="text-rack-muted">
                Management API
              </dt>

              <dd>Read-only</dd>
            </div>

            <div className="flex justify-between gap-4 py-3">
              <dt className="text-rack-muted">
                Remote management
              </dt>

              <dd>Unsupported</dd>
            </div>
          </dl>

          <p className="mt-5 text-sm leading-relaxed text-rack-warning">
            Administrator authentication and management
            HTTPS are not implemented. Do not expose
            this console to untrusted networks.
          </p>
        </section>
      </div>
    </>
  );
}
