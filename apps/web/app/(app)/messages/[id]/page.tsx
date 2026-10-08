import type { Metadata } from "next";
import { Suspense } from "react";
import { MessageRedirect } from "@/features/conversations/message-redirect";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Message" };

export default function MessagePage() {
  return (
    <Suspense fallback={<div className="flex h-app items-center justify-center"><Spinner /></div>}>
      <MessageRedirect />
    </Suspense>
  );
}
