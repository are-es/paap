"use client";

import { useEffect, useRef, useState } from "react";

/**
 * Connection state of the live log stream.
 * - "connecting": the EventSource is opening or retrying
 * - "live": connected and receiving pushes
 * - "offline": repeated failures; the caller should fall back to polling
 */
export type LogStreamState = "connecting" | "live" | "offline";

interface UseLogStreamOptions<T> {
  /** Applied to every pushed row. Ignored rows never touch React state. */
  onRow: (row: T) => void;
  /**
   * Called when the stream reconnects after a drop. The server does not replay
   * missed rows (a full subscriber buffer drops them), so the caller must refetch
   * to close the gap.
   */
  onReconnect?: () => void;
  enabled?: boolean;
}

/**
 * Subscribes to GET /api/logs/stream (Server-Sent Events) and hands each pushed
 * row to onRow.
 *
 * This replaces interval polling: an idle server costs zero requests, and rows
 * appear as soon as the proxied request finishes rather than up to one poll
 * period later.
 *
 * EventSource reconnects on its own, but it retries forever and silently. The
 * failure counter here surfaces a persistently dead stream as "offline" so the
 * UI can re-enable polling instead of looking live while showing stale data.
 */
export function useLogStream<T>({
  onRow,
  onReconnect,
  enabled = true,
}: UseLogStreamOptions<T>): LogStreamState {
  const [state, setState] = useState<LogStreamState>("connecting");

  // Held in refs so a changing callback identity does not tear down and rebuild
  // the connection on every render.
  const onRowRef = useRef(onRow);
  const onReconnectRef = useRef(onReconnect);
  onRowRef.current = onRow;
  onReconnectRef.current = onReconnect;

  useEffect(() => {
    if (!enabled) {
      setState("offline");
      return;
    }

    let source: EventSource | null = null;
    let closed = false;
    let failures = 0;
    let hadConnected = false;

    const connect = () => {
      if (closed) return;

      source = new EventSource("/api/logs/stream");

      // The server sends a "ready" event immediately on subscribe, so the UI can
      // show live state on an idle server rather than waiting for traffic.
      source.addEventListener("ready", () => {
        failures = 0;
        setState("live");
        if (hadConnected) {
          // Rows emitted while disconnected are gone; resynchronize.
          onReconnectRef.current?.();
        }
        hadConnected = true;
      });

      source.addEventListener("log", (event) => {
        try {
          onRowRef.current(JSON.parse((event as MessageEvent).data) as T);
        } catch {
          // A malformed row must not kill the stream.
        }
      });

      source.onerror = () => {
        // EventSource fires error on every transient drop and retries itself.
        // Only give up after several consecutive failures.
        failures += 1;
        if (failures >= 4) {
          setState("offline");
          source?.close();
          source = null;
          return;
        }
        setState("connecting");
      };
    };

    connect();

    return () => {
      closed = true;
      source?.close();
    };
  }, [enabled]);

  return state;
}
