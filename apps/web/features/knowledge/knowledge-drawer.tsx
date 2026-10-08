"use client";

import Link from "next/link";
import { Maximize2 } from "lucide-react";
import { entityMeta } from "@/lib/entities";
import { Dialog, SheetContent } from "@/components/ui/overlay";
import { useKnowledgeExplanation } from "@/features/ideas/hooks";
import { KnowledgeExplanationView } from "./knowledge-explanation";

/** Right-side drawer explaining one knowledge item. Controlled by the caller (usually ?item= in the URL). */
export function KnowledgeDrawer({
  itemId,
  onOpenChange,
  onNavigate,
  readOnly,
  readOnlyReason,
}: {
  itemId: string | null;
  onOpenChange: (open: boolean) => void;
  onNavigate: (id: string) => void;
  readOnly?: boolean;
  readOnlyReason?: string;
}) {
  const ex = useKnowledgeExplanation(itemId);
  const item = ex.data?.item;
  const title = item && item.id === itemId ? `${item.label} · ${entityMeta(item.kind).label}` : "Knowledge";
  return (
    <Dialog open={Boolean(itemId)} onOpenChange={onOpenChange}>
      {itemId && (
        <SheetContent title={title}>
          <KnowledgeExplanationView
            key={itemId}
            id={itemId}
            onNavigate={onNavigate}
            readOnly={readOnly}
            readOnlyReason={readOnlyReason}
            headerAction={
              <Link href={`/knowledge/${itemId}`} className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[12px] text-muted hover:bg-surface-2 hover:text-fg">
                <Maximize2 className="h-3 w-3" /> Open as page
              </Link>
            }
          />
        </SheetContent>
      )}
    </Dialog>
  );
}
