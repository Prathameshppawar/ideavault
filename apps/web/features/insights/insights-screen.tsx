"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Telescope, Wand2 } from "lucide-react";
import { PageHeader } from "@/components/ui/primitives";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/overlay";
import { ThinkingPanel } from "./thinking-panel";
import { PromptPanel } from "./prompt-panel";

export function InsightsScreen() {
  const params = useSearchParams();
  const router = useRouter();
  const tab = params.get("tab") === "prompt" ? "prompt" : "thinking";
  return (
    <div className="mx-auto max-w-6xl px-5 py-8 sm:px-8 sm:py-10">
      <PageHeader
        eyebrow="Reflection"
        title="Insights"
        description="Patterns in how you think and prompt, drawn from what you actually recorded — every observation links to its evidence."
      />
      <Tabs value={tab} onValueChange={(t) => router.replace(t === "thinking" ? "?" : `?tab=${t}`, { scroll: false })}>
        <TabsList>
          <TabsTrigger value="thinking">
            <Telescope className="h-3.5 w-3.5" /> Thinking patterns
          </TabsTrigger>
          <TabsTrigger value="prompt">
            <Wand2 className="h-3.5 w-3.5" /> Prompt analyzer
          </TabsTrigger>
        </TabsList>
        <TabsContent value="thinking">
          <ThinkingPanel />
        </TabsContent>
        <TabsContent value="prompt">
          <PromptPanel />
        </TabsContent>
      </Tabs>
    </div>
  );
}
