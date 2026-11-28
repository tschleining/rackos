import {
  useEffect,
} from "react";

import {
  useAuth,
} from "./AuthContext";

interface ActivityEvent {
  type: string;
  isTrusted: boolean;
  repeat?: boolean;
}

/**
 * Identify browser-generated interactions
 * that qualify for administrator activity.
 *
 * This is a frontend activity policy,
 * not proof of human identity.
 */
export function isQualifyingActivity(
  event: ActivityEvent,
  visibility: DocumentVisibilityState,
): boolean {
  if (
    !event.isTrusted ||
    visibility !== "visible"
  ) {
    return false;
  }

  switch (event.type) {
    case "pointerdown":
    case "click":
      return true;

    case "keydown":
      return event.repeat !== true;

    default:
      return false;
  }
}

/**
 * Observe administrator interaction without
 * collecting input values or retaining events.
 *
 * Throttling and request coordination are
 * owned by AuthProvider.
 */
export function ActivityTracker() {
  const {
    state,
    recordActivity,
  } = useAuth();

  const authenticated =
    state.status === "authenticated";

  useEffect(() => {
    if (!authenticated) {
      return;
    }

    function handleActivity(
      event: Event,
    ): void {
      if (
        !isQualifyingActivity(
          event,
          document.visibilityState,
        )
      ) {
        return;
      }

      void recordActivity();
    }

    const options: AddEventListenerOptions = {
      capture: true,
      passive: true,
    };

    window.addEventListener(
      "pointerdown",
      handleActivity,
      options,
    );

    window.addEventListener(
      "click",
      handleActivity,
      options,
    );

    window.addEventListener(
      "keydown",
      handleActivity,
      options,
    );

    return () => {
      window.removeEventListener(
        "pointerdown",
        handleActivity,
        true,
      );

      window.removeEventListener(
        "click",
        handleActivity,
        true,
      );

      window.removeEventListener(
        "keydown",
        handleActivity,
        true,
      );
    };
  }, [
    authenticated,
    recordActivity,
  ]);

  return null;
}