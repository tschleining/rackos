import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import {
  QueryClientProvider,
} from "@tanstack/react-query";

import { BrowserRouter } from "react-router";

import { queryClient } from "./app/services";
import { ErrorBoundary } from "./components/ErrorBoundary";

import App from "./App";

import "./styles/index.css";

const container = document.getElementById("root");

if (!container) {
  throw new Error(
    "RackOS application root was not found",
  );
}

createRoot(container).render(
  <StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <App />
        </BrowserRouter>
      </QueryClientProvider>
    </ErrorBoundary>
  </StrictMode>,
);