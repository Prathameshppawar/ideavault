"use client";

import { CircleCheck, CircleX } from "lucide-react";
import type { ConnectorEvent } from "@/lib/types";
import { fullDate, timeAgo } from "@/lib/format";
import { connectorName } from "@/features/connectors/connector-meta";
import { formatMs } from "./usage-data";

/** Compact log of connector events (connect attempts and tool calls). */
export function ConnectorEventList({ events, empty }: { events: ConnectorEvent[]; empty?: React.ReactNode }) {
  if (events.length === 0) return <>{empty ?? null}</>;
  return (
    <ol className="divide-y divide-border rounded-[var(--radius-lg)] border border-border bg-surface" aria-label="Recent connector events">
      {events.map((e) => (
        <li key={e.id} className="flex items-start gap-3 px-4 py-2.5 text-[13px]">
          {e.success ? <CircleCheck className="mt-0.5 h-3.5 w-3.5 shrink-0 text-success" aria-label="Succeeded" /> : <CircleX className="mt-0.5 h-3.5 w-3.5 shrink-0 text-danger" aria-label="Failed" />}
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-baseline gap-x-2">
              <span className="text-fg">{connectorName(e.connector_key)}</span>
              <span className="font-mono text-[11.5px] text-muted">{e.tool || e.operation}</span>
              <span className="text-[11.5px] tabular-nums text-faint">{e.latency_ms < 1 ? "<1 ms" : formatMs(e.latency_ms)}</span>
            </div>
            {e.error && <p className="mt-0.5 break-words text-[12px] text-danger">{e.error}</p>}
          </div>
          <time className="shrink-0 text-[11.5px] text-faint" dateTime={e.created_at} title={fullDate(e.created_at)}>
            {timeAgo(e.created_at)}
          </time>
        </li>
      ))}
    </ol>
  );
}
