import type { Metadata } from "next";
import { Suspense } from "react";
import { AuthForm } from "@/features/auth/auth-form";

export const metadata: Metadata = { title: "Create your vault" };

export default function SetupPage() {
  return (
    <Suspense>
      <AuthForm mode="setup" />
    </Suspense>
  );
}
