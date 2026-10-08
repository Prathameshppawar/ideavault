import type { Metadata } from "next";
import { Suspense } from "react";
import { HomeChat } from "@/features/chat/home-chat";

export const metadata: Metadata = { title: "Chat" };

export default function HomePage() {
  return (
    <Suspense>
      <HomeChat />
    </Suspense>
  );
}
