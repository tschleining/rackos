import {
  CircleAlert,
  Cpu,
  RefreshCw,
} from "lucide-react";

import {
  APIError,
} from "@rackos/api-client";

import {
  InventoryDetails,
} from "../features/inventory/InventoryDetails";

import {
  useDeviceDetail,
} from "../features/devices/deviceDetail";

import {
  useInventory,
} from "../hooks/useInventory";

export function InventoryPage() {
  const { device, isStale } = useDeviceDetail();

  const inventory = useInventory(device.id);

  const snapshot = inventory.data;

  const deviceRemoved =
    inventory.isError &&
    inventory.error instanceof APIError &&
    inventory.error.kind === "http" &&
    inventory.error.status === 404 &&
    inventory.error.serverMessage ===
      "device not found";

  const refreshFailed =
    inventory.isError || inventory.isPaused;

  const showSnapshot =
    snapshot !== undefined &&
    snapshot !== null &&
    !deviceRemoved;

  return (
    <>
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 className="text-xl font-semibold">
            Hardware inventory
          </h2>

          <p className="mt-2 text-sm leading-relaxed text-rack-muted">
            Processor, memory and system specifications
            from the latest stored hardware snapshot.
          </p>
        </div>

        <button
          type="button"
          disabled={
            inventory.isFetching ||
            inventory.isPaused
          }
          onClick={() => {
            void inventory.refetch();
          }}
          className="inline-flex min-h-10 items-center gap-2 rounded-lg border border-rack-border bg-rack-surface px-4 py-2 text-sm text-rack-muted hover:border-rack-accent hover:text-rack-primary disabled:cursor-not-allowed disabled:opacity-50"
        >
          <RefreshCw
            size={16}
            aria-hidden="true"
            className={
              inventory.isFetching
                ? "motion-safe:animate-spin"
                : ""
            }
          />

          {inventory.isFetching
            ? "Refreshing..."
            : "Refresh inventory"}
        </button>
      </div>

      {!isStale && device.status === "offline" && (
        <div
          role="status"
          className="mb-6 rounded-lg border border-rack-warning/30 bg-rack-warning/5 p-4 text-sm leading-relaxed text-rack-warning"
        >
          This agent is offline. Previously collected
          inventory remains accessible, but new
          hardware information requires a successful
          inventory report.
        </div>
      )}

      {refreshFailed && showSnapshot && (
        <div
          role="alert"
          className="mb-6 rounded-lg border border-rack-warning/30 bg-rack-warning/5 p-4 text-sm text-rack-warning"
        >
          {inventory.isPaused
            ? "Inventory refresh is paused. Showing the last successfully retrieved snapshot."
            : "Unable to refresh inventory. Showing the last successfully retrieved snapshot."}
        </div>
      )}

      {showSnapshot ? (
        <InventoryDetails inventory={snapshot} />
      ) : deviceRemoved ? (
        <section className="rounded-xl border border-rack-border bg-rack-surface px-6 py-14 text-center">
          <CircleAlert
            size={36}
            aria-hidden="true"
            className="mx-auto mb-4 text-rack-warning"
          />

          <h3 className="font-medium">
            Device no longer registered
          </h3>

          <p className="mt-2 text-sm text-rack-muted">
            Inventory is unavailable for this device.
          </p>
        </section>
      ) : inventory.isPaused ? (
        <section
          role="status"
          className="rounded-xl border border-rack-border bg-rack-surface px-6 py-14 text-center"
        >
          <CircleAlert
            size={36}
            aria-hidden="true"
            className="mx-auto mb-4 text-rack-warning"
          />

          <h3 className="font-medium">
            Inventory refresh paused
          </h3>

          <p className="mt-2 text-sm text-rack-muted">
            Inventory retrieval will resume when
            the connection becomes available.
          </p>
        </section>
      ) : inventory.isError ? (
        <section
          role="alert"
          className="rounded-xl border border-rack-border bg-rack-surface px-6 py-14 text-center"
        >
          <CircleAlert
            size={36}
            aria-hidden="true"
            className="mx-auto mb-4 text-rack-danger"
          />

          <h3 className="font-medium">
            Inventory unavailable
          </h3>

          <p className="mt-2 text-sm text-rack-muted">
            Unable to retrieve hardware inventory
            from the appliance.
          </p>

          <button
            type="button"
            disabled={inventory.isFetching}
            onClick={() => {
              void inventory.refetch();
            }}
            className="mt-5 rounded-lg border border-rack-border px-4 py-2 text-sm font-medium text-rack-accent hover:border-rack-accent disabled:opacity-50"
          >
            Try again
          </button>
        </section>
      ) : inventory.isPending ? (
        <section
          role="status"
          className="rounded-xl border border-rack-border bg-rack-surface px-6 py-14 text-center"
        >
          <RefreshCw
            size={36}
            aria-hidden="true"
            className="mx-auto mb-4 text-rack-muted motion-safe:animate-spin"
          />

          <h3 className="font-medium">
            Loading hardware inventory...
          </h3>
        </section>
      ) : (
        <section className="rounded-xl border border-rack-border bg-rack-surface px-6 py-14 text-center">
          <Cpu
            size={36}
            aria-hidden="true"
            className="mx-auto mb-4 text-rack-muted"
          />

          <h3 className="font-medium">
            No inventory snapshot
          </h3>

          <p className="mx-auto mt-2 max-w-md text-sm leading-relaxed text-rack-muted">
            The appliance has not received hardware
            inventory from this agent yet. Inventory
            will appear after a successful report.
          </p>
        </section>
      )}

      <footer className="mt-6 flex flex-wrap items-center justify-between gap-3 text-xs text-rack-muted">
        <p>
          Inventory refreshes every five minutes
          while this page is active.
        </p>

        {inventory.dataUpdatedAt > 0 && (
          <p>
            Last successful retrieval:{" "}

            <time
              dateTime={new Date(
                inventory.dataUpdatedAt,
              ).toISOString()}
            >
              {new Date(
                inventory.dataUpdatedAt,
              ).toLocaleString()}
            </time>
          </p>
        )}
      </footer>
    </>
  );
}