const CHANNEL_NAME = "rackos-auth-v1";

const SESSION_CHANGED = "session-changed";

export interface SessionChannel {
  notify(): void;
  close(): void;
}

/**
 * Exchange session invalidation hints between
 * RackOS console tabs.
 *
 * Messages never establish authentication
 * state and never contain credentials or
 * session metadata.
 */
export function openSessionChannel(
  onSessionChanged: () => void,
): SessionChannel | null {
  if (
    typeof window === "undefined" ||
    typeof window.BroadcastChannel !== "function"
  ) {
    return null;
  }

  let channel: BroadcastChannel;

  try {
    channel = new window.BroadcastChannel(
      CHANNEL_NAME,
    );
  } catch {
    // Browser policy may prevent access to
    // BroadcastChannel.
    return null;
  }

  let closed = false;

  function handleMessage(
    event: MessageEvent<unknown>,
  ): void {
    if (
      !closed &&
      event.data === SESSION_CHANGED
    ) {
      onSessionChanged();
    }
  }

  channel.addEventListener(
    "message",
    handleMessage,
  );

  return {
    notify() {
      if (closed) {
        return;
      }

      try {
        channel.postMessage(SESSION_CHANGED);
      } catch {
        // Synchronization failures must not
        // change the authentication result.
      }
    },

    close() {
      if (closed) {
        return;
      }

      closed = true;

      channel.removeEventListener(
        "message",
        handleMessage,
      );

      channel.close();
    },
  };
}