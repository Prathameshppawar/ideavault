import { Skeleton } from "@/components/ui/primitives";

/** Suspense fallback for the platform screens: header + body skeleton at the page's width. */
export function PlatformFallback() {
  return (
    <div className="mx-auto max-w-6xl px-5 py-8 sm:px-8 sm:py-10" aria-busy="true" aria-label="Loading">
      <Skeleton className="mb-3 h-3 w-20" />
      <Skeleton className="mb-3 h-9 w-56" />
      <Skeleton className="mb-10 h-4 w-96 max-w-full" />
      <div className="space-y-3">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    </div>
  );
}
