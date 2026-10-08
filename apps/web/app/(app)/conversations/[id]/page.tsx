import type { Metadata } from "next";
import { Suspense } from "react";
import { ConversationScreen } from "@/features/conversations/conversation-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Conversation" };

export default function ConversationPage() {
  return (
    <Suspense fallback={<div className="flex h-app items-center justify-center"><Spinner /></div>}>
      <ConversationScreen />
    </Suspense>
  );
}
