import {
  lazy,
  Suspense,
} from "react";

import {
  Route,
  Routes,
} from "react-router";

import {
  AppLayout,
} from "./layouts/AppLayout";

import {
  DeviceLayout,
} from "./layouts/DeviceLayout";

import {
  PageState,
} from "./components/PageState";

import {
  OverviewPage,
} from "./pages/OverviewPage";

import {
  DevicesPage,
} from "./pages/DevicesPage";

import {
  DeviceOverviewPage,
} from "./pages/DeviceOverviewPage";

import {
  InventoryPage,
} from "./pages/InventoryPage";

import {
  SystemPage,
} from "./pages/SystemPage";

import {
  NotFoundPage,
} from "./pages/NotFoundPage";

const PerformancePage = lazy(
  () =>
    import("./pages/PerformancePage").then(
      (module) => ({
        default: module.PerformancePage,
      }),
    ),
);

export default function App() {
  return (
    <Routes>
      <Route element={<AppLayout />}>
        <Route
          index
          element={<OverviewPage />}
        />

        <Route
          path="devices"
          element={<DevicesPage />}
        />

        <Route
          path="devices/:deviceId"
          element={<DeviceLayout />}
        >
          <Route
            index
            element={<DeviceOverviewPage />}
          />

          <Route
            path="inventory"
            element={<InventoryPage />}
          />

          <Route
            path="performance"
            element={
              <Suspense
                fallback={
                  <PageState
                    title="Loading performance interface"
                    description="Preparing the performance monitoring components."
                    loading
                  />
                }
              >
                <PerformancePage />
              </Suspense>
            }
          />
        </Route>

        <Route
          path="system"
          element={<SystemPage />}
        />

        <Route
          path="*"
          element={<NotFoundPage />}
        />
      </Route>
    </Routes>
  );
}