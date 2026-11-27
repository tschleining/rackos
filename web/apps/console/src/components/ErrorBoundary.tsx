import { Component } from "react";

import type { ReactNode } from "react";

import { PageState } from "./PageState";

interface ErrorBoundaryProps {
  children: ReactNode;
  resetKey?: string;
}

interface ErrorBoundaryState {
  hasError: boolean;
}

export class ErrorBoundary extends Component<
  ErrorBoundaryProps,
  ErrorBoundaryState
> {
  state: ErrorBoundaryState = {
    hasError: false,
  };

  static getDerivedStateFromError(): ErrorBoundaryState {
    return {
      hasError: true,
    };
  }

  componentDidUpdate(
    previousProps: ErrorBoundaryProps,
  ) {
    if (
      this.state.hasError &&
      previousProps.resetKey !== this.props.resetKey
    ) {
      this.setState({
        hasError: false,
      });
    }
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="mx-auto w-full max-w-3xl p-6">
          <PageState
            title="Something went wrong"
            description="An unexpected console error occurred. Try restoring the interface or reload the console."
            error
            onRetry={() => {
              this.setState({
                hasError: false,
              });
            }}
          />

          <div className="mt-4 text-center">
            <button
              type="button"
              onClick={() => {
                window.location.reload();
              }}
              className="rounded-lg px-4 py-2 text-sm font-medium text-rack-accent hover:underline"
            >
              Reload console
            </button>
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}