import {
  useEffect,
  useRef,
} from "react";

import {
  useAuth,
} from "./AuthContext";

const MAX_DEADLINE_CHECK_MS = 60_000;
const DEADLINE_RECHECK_MS = 30_000;
const RESUME_DEBOUNCE_MS = 1_000;

interface DeadlineVerification {
  key: string;
  attemptedAt: number;
}

export function SessionLifecycle() {
  const {
    state,
    sessionGeneration,
    captureManagementUnauthorized,
  } = useAuth();

  const lastResumeVerification =
    useRef(Number.NEGATIVE_INFINITY);

  const lastDeadlineVerification =
    useRef<DeadlineVerification | null>(
      null,
    );

  useEffect(() => {
    if (
      state.status !== "authenticated"
    ) {
      return;
    }

    const revalidate =
      captureManagementUnauthorized();

    if (revalidate === undefined) {
      return;
    }

    const session = state.session;

    const deadline = Math.min(
      Date.parse(
        session.idle_expires_at,
      ),
      Date.parse(
        session.expires_at,
      ),
    );

    if (!Number.isFinite(deadline)) {
      revalidate();
      return;
    }

    const deadlineKey = JSON.stringify([
      session.administrator_id,
      session.created_at,
      session.expires_at,
      session.idle_expires_at,
    ]);

    const monotonicDeadline =
      performance.now() +
      Math.max(
        0,
        deadline - Date.now(),
      );

    let timer: number | undefined;

    let disposed = false;

    function remainingTime(): number {
      return Math.min(
        deadline - Date.now(),

        monotonicDeadline -
          performance.now(),
      );
    }

    function remainingCooldown(): number {
      const previous =
        lastDeadlineVerification.current;

      if (
        previous === null ||
        previous.key !== deadlineKey
      ) {
        return 0;
      }

      return Math.max(
        0,

        DEADLINE_RECHECK_MS -
          (
            performance.now() -
            previous.attemptedAt
          ),
      );
    }

    function rememberDeadlineAttempt(): void {
      lastDeadlineVerification.current = {
        key: deadlineKey,
        attemptedAt: performance.now(),
      };
    }

    function verifyDeadline(): void {
      if (
        disposed ||
        remainingTime() > 0 ||
        remainingCooldown() > 0
      ) {
        return;
      }

      rememberDeadlineAttempt();

      revalidate?.();
    }

    function schedule(): void {
      if (disposed) {
        return;
      }

      if (timer !== undefined) {
        window.clearTimeout(timer);
      }

      const remaining =
        remainingTime();

      // A future expiration deadline must
      // never be postponed by a cooldown
      // from an earlier verification.
      const delay =
        remaining > 0
          ? remaining
          : remainingCooldown();

      timer = window.setTimeout(
        checkDeadline,

        Math.min(
          MAX_DEADLINE_CHECK_MS,
          Math.max(0, delay),
        ),
      );
    }

    function checkDeadline(): void {
      if (disposed) {
        return;
      }

      verifyDeadline();

      schedule();
    }

    function handleResume(): void {
      if (
        disposed ||
        document.visibilityState !==
          "visible"
      ) {
        return;
      }

      const now = performance.now();

      if (
        now -
          lastResumeVerification.current <
        RESUME_DEBOUNCE_MS
      ) {
        return;
      }

      lastResumeVerification.current = now;

      // If resume verification occurs after
      // expiration, count it as a deadline
      // verification too.
      //
      // This avoids two immediate inspections
      // for the same expired metadata.
      if (remainingTime() <= 0) {
        rememberDeadlineAttempt();
      }

      revalidate?.();
    }

    function handleVisibilityChange(): void {
      if (
        document.visibilityState ===
        "visible"
      ) {
        handleResume();
      }
    }

    function handlePageShow(
      event: PageTransitionEvent,
    ): void {
      if (event.persisted) {
        handleResume();
      }
    }

    window.addEventListener(
      "focus",
      handleResume,
    );

    window.addEventListener(
      "pageshow",
      handlePageShow,
    );

    document.addEventListener(
      "visibilitychange",
      handleVisibilityChange,
    );

    schedule();

    return () => {
      disposed = true;

      if (timer !== undefined) {
        window.clearTimeout(timer);
      }

      window.removeEventListener(
        "focus",
        handleResume,
      );

      window.removeEventListener(
        "pageshow",
        handlePageShow,
      );

      document.removeEventListener(
        "visibilitychange",
        handleVisibilityChange,
      );
    };
  }, [
    state,
    sessionGeneration,
    captureManagementUnauthorized,
  ]);

  return null;
}