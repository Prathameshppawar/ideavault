"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { FlaskConical, KeyRound, Layers, Route } from "lucide-react";
import { PageHeader } from "@/components/ui/primitives";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/overlay";
import { ProvidersTab } from "./providers-tab";
import { RegistryTab } from "./registry-tab";
import { RoutingTab } from "./routing-tab";
import { LabTab } from "./lab-tab";

const TABS = ["providers", "registry", "routing", "lab"] as const;
type Tab = (typeof TABS)[number];

export function ModelsScreen() {
  const params = useSearchParams();
  const router = useRouter();
  const raw = params.get("tab");
  const tab: Tab = (TABS as readonly string[]).includes(raw ?? "") ? (raw as Tab) : "providers";
  const setTab = (t: string) => {
    const p = new URLSearchParams();
    p.set("tab", t);
    router.replace(`?${p.toString()}`, { scroll: false });
  };
  return (
    <div className="mx-auto max-w-6xl px-5 py-8 sm:px-8 sm:py-10">
      <PageHeader
        eyebrow="Platform"
        title="Models"
        description="Which AI providers IdeaVault can use, which model handles each kind of work, and how they compare on your own prompts."
      />
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList className="overflow-x-auto">
          <TabsTrigger value="providers">
            <KeyRound className="h-3.5 w-3.5" /> Providers
          </TabsTrigger>
          <TabsTrigger value="registry">
            <Layers className="h-3.5 w-3.5" /> Registry
          </TabsTrigger>
          <TabsTrigger value="routing">
            <Route className="h-3.5 w-3.5" /> Routing
          </TabsTrigger>
          <TabsTrigger value="lab">
            <FlaskConical className="h-3.5 w-3.5" /> Lab
          </TabsTrigger>
        </TabsList>
        <TabsContent value="providers">
          <ProvidersTab />
        </TabsContent>
        <TabsContent value="registry">
          <RegistryTab />
        </TabsContent>
        <TabsContent value="routing">
          <RoutingTab onGoToProviders={() => setTab("providers")} />
        </TabsContent>
        <TabsContent value="lab">
          <LabTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}
