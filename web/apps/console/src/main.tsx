import {
  StrictMode,
} from "react";

import {
  createRoot,
} from "react-dom/client";

import {
  QueryClientProvider,
} from "@tanstack/react-query";

import {
  BrowserRouter,
} from "react-router";

import {
  api,
  authenticationEnabled,
  queryClient,
} from "./app/services";

import {
  ErrorBoundary,
} from "./components/ErrorBoundary";

import {
  ActivityTracker,
  AuthBoundary,
  AuthProvider,
  SessionLifecycle,
} from "./features/auth";

import App from "./App";

import "./styles/index.css";

const container =
  document.getElementById("root");

if (!container) {
  throw new Error(
    "RackOS application root was not found",
  );
}

createRoot(container).render(
  <StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        {authenticationEnabled ? (
          <AuthProvider client={api}>
            <SessionLifecycle />

            <ActivityTracker />

            <AuthBoundary>
              <App />
            </AuthBoundary>
          </AuthProvider>
        ) : (
          <QueryClientProvider
            client={queryClient}
          >
            <App />
          </QueryClientProvider>
        )}
      </BrowserRouter>
    </ErrorBoundary>
  </StrictMode>,
);