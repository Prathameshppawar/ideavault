import { cn } from "@/lib/format";

/** Tool permission category (READ / ANALYZE / WRITE / EXTERNAL / DESTRUCTIVE), coloured by meaning. */
const CATEGORY_TONE: Record<string, string> = {
  READ: "text-k-evidence border-k-evidence/30",
  ANALYZE: "text-k-insight border-k-insight/30",
  WRITE: "text-k-decision border-k-decision/30",
  EXTERNAL: "text-k-assumption border-k-assumption/30",
  DESTRUCTIVE: "text-danger border-danger/30",
};

export function CategoryTag({ category }: { category: string }) {
  return (
    <span className={cn("inline-flex h-[18px] items-center rounded-[3px] border px-1 text-[10px] font-semibold uppercase tracking-wider", CATEGORY_TONE[category] ?? "border-border text-muted")}>
      {category}
    </span>
  );
}
