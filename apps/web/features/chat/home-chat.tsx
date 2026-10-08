"use client";

import { useSearchParams } from "next/navigation";
import { ChatView } from "./chat-view";
import { ConversationRail } from "./conversation-rail";

export function HomeChat() {
  const params = useSearchParams();
  const q = params.get("q");
  const ideaId = params.get("idea");
  return (
    <div className="flex h-app">
      <ConversationRail />
      <div className="min-w-0 flex-1">
        <ChatView
          key={q ?? "home"}
          conversationId={null}
          ideaId={ideaId}
          initialPrompt={q}
          onConversationCreated={(id) => window.history.replaceState(null, "", `/conversations/${id}`)}
        />
      </div>
    </div>
  );
}
