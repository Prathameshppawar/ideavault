"use client";

import { memo, useState, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import { Check, Copy } from "lucide-react";
import { parseIvLink } from "@/lib/entities";
import { EntityChip } from "@/components/ui/badges";
import { cn } from "@/lib/format";

function textOf(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (node && typeof node === "object" && "props" in node) return textOf((node as { props: { children?: ReactNode } }).props.children);
  return "";
}

function CodeBlock({ children }: { children?: ReactNode }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="group relative">
      <pre>{children}</pre>
      <button
        type="button"
        onClick={() => {
          void navigator.clipboard.writeText(textOf(children));
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        }}
        className="absolute right-2 top-2 rounded border border-border bg-surface px-1.5 py-1 text-faint opacity-0 transition-opacity hover:text-fg group-hover:opacity-100"
        aria-label="Copy code"
      >
        {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  );
}

const components: Components = {
  a({ href, children }) {
    const iv = parseIvLink(href);
    if (iv) return <EntityChip type={iv.type} id={iv.id} label={textOf(children)} />;
    const safe = href && /^(https?:|mailto:|\/|#)/i.test(href) ? href : undefined;
    const external = safe?.startsWith("http");
    return (
      <a href={safe} target={external ? "_blank" : undefined} rel={external ? "noopener noreferrer nofollow" : undefined}>
        {children}
      </a>
    );
  },
  pre({ children }) {
    return <CodeBlock>{children}</CodeBlock>;
  },
  img({ src, alt }) {
    // Remote images in untrusted markdown could leak data; only allow https.
    const s = typeof src === "string" && src.startsWith("https://") ? src : undefined;
    // eslint-disable-next-line @next/next/no-img-element
    return s ? <img src={s} alt={alt ?? ""} loading="lazy" referrerPolicy="no-referrer" /> : null;
  },
};

/** Allow our iv:// scheme through react-markdown's URL sanitizer (default blocks unknown schemes). */
function urlTransform(url: string): string {
  if (url.startsWith("iv://")) return url;
  if (/^(https?:|mailto:|\/|#)/i.test(url)) return url;
  return "";
}

/**
 * Renders Markdown safely: raw HTML is never rendered (react-markdown escapes it),
 * links to iv:// entities become chips, code blocks are highlighted and copyable.
 */
export const Markdown = memo(function Markdown({ children, className }: { children: string; className?: string }) {
  return (
    <div className={cn("prose-iv", className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[[rehypeHighlight, { detect: false, ignoreMissing: true }]]} components={components} urlTransform={urlTransform}>
        {children}
      </ReactMarkdown>
    </div>
  );
});
