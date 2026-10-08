import Link from "next/link";

export default function NotFound() {
  return (
    <div className="flex min-h-full flex-col items-center justify-center px-6 py-24 text-center">
      <p className="font-display text-6xl text-fg">404</p>
      <p className="mt-3 text-[15px] text-muted">This thought doesn&apos;t exist (yet).</p>
      <Link href="/" className="mt-6 text-sm font-medium text-accent underline underline-offset-4">
        Back to IdeaVault
      </Link>
    </div>
  );
}
