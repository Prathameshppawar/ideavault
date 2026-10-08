import type { Metadata } from "next";
import { Suspense } from "react";
import { SearchScreen } from "@/features/search/search-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Search" };

export default function SearchPage() {
  return (
    <Suspense fallback={<div className="flex h-app items-center justify-center"><Spinner /></div>}>
      <SearchScreen />
    </Suspense>
  );
}
