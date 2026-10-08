"use client";

import { useCallback, useEffect, useImperativeHandle, useRef, useState, type Ref } from "react";
import { forceCollide, forceLink, forceManyBody, forceSimulation, forceX, forceY, type Simulation } from "d3-force";
import { select } from "d3-selection";
import { zoom as d3zoom, zoomIdentity, type ZoomBehavior, type ZoomTransform } from "d3-zoom";
import { entityMeta } from "@/lib/entities";
import { truncate } from "@/lib/format";
import { bounds, edgeStyle, fitTransform, labelBounds, neighbourhood, seedNearIdeas, type ULink, type UNode } from "./graph-model";

export interface GraphCanvasHandle {
  fit: (animate?: boolean) => void;
  zoomBy: (factor: number) => void;
  centerOn: (id: string) => void;
}

/** Resolved theme colours (CSS variables read from the document). */
export type GraphColors = Record<string, string>;

interface Props {
  nodes: UNode[];
  links: ULink[];
  colors: GraphColors;
  serif: string;
  sans: string;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  reducedMotion: boolean;
  label: string;
  /** Screen space covered by overlays (legend, panel) that fitting should avoid. */
  insets?: { left: number; right: number; bottomRatio?: number };
  ref?: Ref<GraphCanvasHandle>;
}

function toneVar(type: string) {
  return `--k-${entityMeta(type).tone}`;
}

/**
 * Force-directed universe drawn on a <canvas> (fast for hundreds–thousands of nodes),
 * with d3-zoom pan/zoom, hover tooltip, click-to-select and keyboard zoom/pan.
 */
export function GraphCanvas({ nodes, links, colors, serif, sans, selectedId, onSelect, reducedMotion, label, insets, ref }: Props) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const simRef = useRef<Simulation<UNode, ULink> | null>(null);
  const zoomRef = useRef<ZoomBehavior<HTMLCanvasElement, unknown> | null>(null);
  const transformRef = useRef<ZoomTransform>(zoomIdentity);
  const sizeRef = useRef({ w: 0, h: 0, dpr: 1 });
  const hoverRef = useRef<UNode | null>(null);
  const interactedRef = useRef(false);
  const needsFitRef = useRef(true);
  const rafRef = useRef(0);
  const tweenRef = useRef(0);
  const dataRef = useRef({ nodes, links, colors, serif, sans, selectedId, focus: null as Set<string> | null });
  const insetsRef = useRef({ left: 0, right: 0, bottomRatio: 0 });
  const [tip, setTip] = useState<{ x: number; y: number; w: number; node: UNode } | null>(null);

  // ---------- drawing ----------
  const draw = useCallback(() => {
    rafRef.current = 0;
    const canvas = canvasRef.current;
    const { w, h, dpr } = sizeRef.current;
    if (!canvas || !w || !h) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const { nodes, links, colors, serif, sans, selectedId, focus } = dataRef.current;
    const t = transformRef.current;
    const k = t.k;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);
    ctx.translate(t.x, t.y);
    ctx.scale(k, k);

    // Edges: structural hairlines first, semantic relationships on top.
    for (const pass of [true, false]) {
      for (const l of links) {
        if (l.structural !== pass) continue;
        const s = l.source as UNode;
        const tg = l.target as UNode;
        if (s.x === undefined || tg.x === undefined || s.y === undefined || tg.y === undefined) continue;
        const lit = !focus || (focus.has(s.id) && focus.has(tg.id));
        ctx.beginPath();
        if (l.structural) {
          ctx.globalAlpha = lit ? (focus ? 0.9 : 0.55) : 0.08;
          ctx.strokeStyle = colors["--border-strong"];
          ctx.lineWidth = 1 / k;
          ctx.setLineDash([]);
          ctx.moveTo(s.x, s.y);
          ctx.lineTo(tg.x, tg.y);
          ctx.stroke();
          continue;
        }
        const st = edgeStyle(l.type);
        const col = st.tone ? colors[`--k-${st.tone}`] : colors["--text-faint"];
        ctx.globalAlpha = lit ? 0.95 : 0.1;
        ctx.strokeStyle = col;
        ctx.lineWidth = 1.6 / k;
        ctx.setLineDash(st.dash ? st.dash.map((d) => d / k) : []);
        const dx = tg.x - s.x;
        const dy = tg.y - s.y;
        const len = Math.hypot(dx, dy) || 1;
        const ux = dx / len;
        const uy = dy / len;
        const ex = tg.x - ux * (tg.r + 2 / k);
        const ey = tg.y - uy * (tg.r + 2 / k);
        ctx.moveTo(s.x + ux * s.r, s.y + uy * s.r);
        ctx.lineTo(ex, ey);
        ctx.stroke();
        if (st.directed && len > s.r + tg.r + 8 / k) {
          const a = 6 / k;
          ctx.setLineDash([]);
          ctx.beginPath();
          ctx.moveTo(ex, ey);
          ctx.lineTo(ex - ux * a - uy * a * 0.55, ey - uy * a + ux * a * 0.55);
          ctx.lineTo(ex - ux * a + uy * a * 0.55, ey - uy * a - ux * a * 0.55);
          ctx.closePath();
          ctx.fillStyle = col;
          ctx.fill();
        }
      }
    }
    ctx.setLineDash([]);

    // Nodes.
    const hover = hoverRef.current;
    for (const n of nodes) {
      if (n.x === undefined || n.y === undefined) continue;
      const lit = !focus || focus.has(n.id);
      const col = colors[toneVar(n.type)] || colors["--text-muted"];
      ctx.globalAlpha = (lit ? 1 : 0.14) * (n.dead ? 0.55 : 1);
      ctx.beginPath();
      ctx.arc(n.x, n.y, n.r, 0, Math.PI * 2);
      if (n.dead) {
        ctx.fillStyle = colors["--bg"];
        ctx.fill();
        ctx.lineWidth = 1.5 / k;
        ctx.strokeStyle = col;
        ctx.stroke();
      } else {
        ctx.fillStyle = col;
        ctx.fill();
        // 2px surface ring keeps overlapping marks legible.
        ctx.lineWidth = (n.type === "idea" ? 2.5 : 1.25) / k;
        ctx.strokeStyle = colors["--bg"];
        ctx.stroke();
      }
      if (n.id === selectedId || n === hover) {
        ctx.globalAlpha = 1;
        ctx.beginPath();
        ctx.arc(n.x, n.y, n.r + 3.5 / k, 0, Math.PI * 2);
        ctx.lineWidth = (n.id === selectedId ? 2 : 1.25) / k;
        ctx.strokeStyle = n.id === selectedId ? colors["--accent"] : colors["--text-muted"];
        ctx.stroke();
      }
    }

    // Labels: ideas (largest first, skipping ones that would collide); knowledge refs when zoomed in
    // or in focus; the hovered/selected node always, in full.
    ctx.textBaseline = "middle";
    const placed: { x0: number; y0: number; x1: number; y1: number }[] = [];
    const collides = (r: { x0: number; y0: number; x1: number; y1: number }) => placed.some((p) => r.x0 < p.x1 && r.x1 > p.x0 && r.y0 < p.y1 && r.y1 > p.y0);
    const ideaNodes = nodes.filter((n) => n.type === "idea" && n.x !== undefined && n.y !== undefined);
    ideaNodes.sort((a, b) => Number(b.id === selectedId || b === hover) - Number(a.id === selectedId || a === hover) || b.r - a.r);
    for (const n of ideaNodes) {
      const lit = !focus || focus.has(n.id);
      const emphasised = n.id === selectedId || n === hover;
      // ~13px on screen; shrinks only when zoomed far out, hidden once unreadable.
      const screen = 13 * Math.min(1, k / 0.5);
      if (screen < 7 && !emphasised) continue;
      const size = Math.max(screen, 7) / k;
      ctx.font = `${size}px ${serif || "Georgia, serif"}`;
      const text = truncate(n.label || "Untitled", emphasised ? 60 : 32);
      const y = n.y! + n.r + 5 / k + size / 2;
      const half = ctx.measureText(text).width / 2;
      const box = { x0: n.x! - half, y0: y - size / 2, x1: n.x! + half, y1: y + size / 2 };
      if (!emphasised && collides(box)) continue;
      placed.push(box);
      ctx.textAlign = "center";
      ctx.globalAlpha = lit ? 1 : 0.25;
      ctx.lineJoin = "round";
      ctx.lineWidth = 3.5 / k;
      ctx.strokeStyle = colors["--bg"];
      ctx.strokeText(text, n.x!, y);
      ctx.fillStyle = colors["--text"];
      ctx.fillText(text, n.x!, y);
    }
    for (const n of nodes) {
      if (n.type === "idea" || n.x === undefined || n.y === undefined) continue;
      const lit = !focus || focus.has(n.id);
      const emphasised = n.id === selectedId || n === hover;
      if (!(emphasised || (lit && (k >= 1.6 || (focus && focus.size < 40))))) continue;
      const size = 10.5 / k;
      ctx.font = `${emphasised ? 600 : 500} ${size}px ${sans || "system-ui, sans-serif"}`;
      ctx.textAlign = "left";
      const text = emphasised ? truncate(n.type === "conversation" || n.type === "artifact" || n.type === "branch" ? n.label : `${n.label} ${n.title}`, 48) : truncate(n.label, 18);
      const x = n.x + n.r + 4 / k;
      ctx.globalAlpha = 1;
      ctx.lineJoin = "round";
      ctx.lineWidth = 3 / k;
      ctx.strokeStyle = colors["--bg"];
      ctx.strokeText(text, x, n.y);
      ctx.fillStyle = emphasised ? colors["--text"] : colors["--text-muted"];
      ctx.fillText(text, x, n.y);
    }
    ctx.globalAlpha = 1;
  }, []);

  const schedule = useCallback(() => {
    if (!rafRef.current) rafRef.current = requestAnimationFrame(draw);
  }, [draw]);

  // ---------- transforms ----------
  const applyTransform = useCallback((target: { k: number; x: number; y: number }, animate: boolean) => {
    const canvas = canvasRef.current;
    const z = zoomRef.current;
    if (!canvas || !z) return;
    const sel = select(canvas);
    cancelAnimationFrame(tweenRef.current);
    const to = zoomIdentity.translate(target.x, target.y).scale(target.k);
    if (!animate) {
      z.transform(sel, to);
      return;
    }
    const from = transformRef.current;
    const start = performance.now();
    const dur = 420;
    const step = (now: number) => {
      const p = Math.min(1, (now - start) / dur);
      const e = 1 - Math.pow(1 - p, 3);
      const k = from.k * Math.pow(target.k / from.k, e);
      z.transform(sel, zoomIdentity.translate(from.x + (target.x - from.x) * e, from.y + (target.y - from.y) * e).scale(k));
      if (p < 1) tweenRef.current = requestAnimationFrame(step);
    };
    tweenRef.current = requestAnimationFrame(step);
  }, []);

  const fit = useCallback(
    (animate = true) => {
      const { w, h } = sizeRef.current;
      const b = bounds(dataRef.current.nodes);
      if (!b || !w || !h) return;
      const { left, right } = insetsRef.current;
      const vw = Math.max(160, w - left - right);
      const pad = Math.min(64, Math.max(20, Math.min(vw, h) * 0.06));
      // Idea labels are screen-sized, so their graph-space extent depends on the scale:
      // fit nodes first, then grow the bounds by the labels at that scale and fit again.
      const k1 = fitTransform(b, vw, h, pad).k;
      const lb = labelBounds(dataRef.current.nodes, k1) ?? b;
      const t = fitTransform(lb, vw, h, pad);
      applyTransform({ ...t, x: t.x + (vw === w - left - right ? left : 0) }, animate && !reducedMotion);
    },
    [applyTransform, reducedMotion],
  );

  useImperativeHandle(
    ref,
    () => ({
      fit: (animate = true) => {
        interactedRef.current = false;
        fit(animate);
      },
      zoomBy: (f: number) => {
        const t = transformRef.current;
        const { w, h } = sizeRef.current;
        const k = Math.max(0.08, Math.min(8, t.k * f));
        const cx = (w / 2 - t.x) / t.k;
        const cy = (h / 2 - t.y) / t.k;
        interactedRef.current = true;
        applyTransform({ k, x: w / 2 - cx * k, y: h / 2 - cy * k }, !reducedMotion);
      },
      centerOn: (id: string) => {
        const n = dataRef.current.nodes.find((x) => x.id === id);
        const { w, h } = sizeRef.current;
        if (!n || n.x === undefined || n.y === undefined || !w) return;
        const k = Math.max(transformRef.current.k, 1.4);
        const { left, right, bottomRatio } = insetsRef.current;
        const cx = left + Math.max(160, w - left - right) / 2;
        const cy = (h * (1 - bottomRatio)) / 2;
        interactedRef.current = true;
        applyTransform({ k, x: cx - n.x * k, y: cy - n.y * k }, !reducedMotion);
      },
    }),
    [applyTransform, fit, reducedMotion],
  );

  useEffect(() => {
    insetsRef.current = { left: insets?.left ?? 0, right: insets?.right ?? 0, bottomRatio: insets?.bottomRatio ?? 0 };
  }, [insets?.left, insets?.right, insets?.bottomRatio]);

  // ---------- latest props for the draw loop ----------
  useEffect(() => {
    if (hoverRef.current && !nodes.includes(hoverRef.current)) hoverRef.current = null;
    const focusId = hoverRef.current?.id ?? selectedId;
    dataRef.current = { nodes, links, colors, serif, sans, selectedId, focus: neighbourhood(links, focusId) };
    schedule();
  }, [nodes, links, colors, serif, sans, selectedId, schedule]);

  // ---------- zoom + resize (mount) ----------
  useEffect(() => {
    const canvas = canvasRef.current;
    const wrap = wrapRef.current;
    if (!canvas || !wrap) return;
    const z = d3zoom<HTMLCanvasElement, unknown>()
      .scaleExtent([0.08, 8])
      .on("zoom", (e: { transform: ZoomTransform; sourceEvent: unknown }) => {
        transformRef.current = e.transform;
        if (e.sourceEvent) {
          interactedRef.current = true;
          setTip(null);
        }
        schedule();
      });
    zoomRef.current = z;
    const sel = select(canvas);
    sel.call(z).on("dblclick.zoom", null);
    const ro = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      const dpr = Math.min(2, window.devicePixelRatio || 1);
      const first = sizeRef.current.w === 0 && width > 0;
      sizeRef.current = { w: width, h: height, dpr };
      canvas.width = Math.round(width * dpr);
      canvas.height = Math.round(height * dpr);
      canvas.style.width = `${width}px`;
      canvas.style.height = `${height}px`;
      if (first || needsFitRef.current || !interactedRef.current) {
        needsFitRef.current = false;
        fit(false);
      }
      schedule();
    });
    ro.observe(wrap);
    return () => {
      ro.disconnect();
      sel.on(".zoom", null);
      cancelAnimationFrame(rafRef.current);
      cancelAnimationFrame(tweenRef.current);
      rafRef.current = 0;
    };
  }, [fit, schedule]);

  // ---------- simulation ----------
  useEffect(() => {
    if (!nodes.length) {
      simRef.current?.stop();
      schedule();
      return;
    }
    const fresh = nodes.every((n) => n.x === undefined);
    seedNearIdeas(nodes);
    const big = nodes.length > 800;
    const sim = forceSimulation<UNode, ULink>(nodes)
      .force(
        "link",
        forceLink<UNode, ULink>(links)
          .id((d) => d.id)
          .distance((l) => {
            const s = l.source as UNode;
            if (!l.structural) return 80;
            if (l.type === "contains") return 18 + s.r + Math.min(40, Math.sqrt(s.degree) * 6);
            if (l.type === "has_branch") return 50;
            return 46;
          })
          .strength((l) => (l.structural ? 0.55 : 0.08)),
      )
      .force(
        "charge",
        forceManyBody<UNode>()
          .strength((n) => (n.type === "idea" ? -420 : n.type === "branch" ? -110 : -22))
          .distanceMax(big ? 300 : 600)
          .theta(big ? 1.2 : 0.9),
      )
      .force("collide", forceCollide<UNode>((n) => n.r + (n.type === "idea" ? 14 : 1.5)).iterations(big ? 1 : 2))
      .force("x", forceX<UNode>(0).strength(0.04))
      .force("y", forceY<UNode>(0).strength(0.045))
      .alphaDecay(big ? 0.05 : 0.0228)
      .on("tick", schedule);
    simRef.current = sim;

    if (reducedMotion || big) {
      // Settle synchronously: no animated layout (respecting reduced motion / keeping large graphs snappy).
      sim.stop();
      const ticks = big ? 160 : Math.ceil(Math.log(sim.alphaMin()) / Math.log(1 - sim.alphaDecay()));
      for (let i = 0; i < ticks; i++) sim.tick();
      if (fresh || !interactedRef.current) fit(false);
      schedule();
    } else {
      if (fresh) {
        sim.stop();
        for (let i = 0; i < 90; i++) sim.tick();
        fit(false);
        sim.alpha(0.35).restart();
      } else {
        sim.alpha(0.3).restart();
      }
      sim.on("end", () => {
        if (!interactedRef.current) fit(true);
      });
    }
    return () => {
      sim.on("tick", null).on("end", null);
      sim.stop();
    };
  }, [nodes, links, reducedMotion, fit, schedule]);

  // ---------- pointer ----------
  const nodeAt = (clientX: number, clientY: number): UNode | null => {
    const canvas = canvasRef.current;
    if (!canvas) return null;
    const rect = canvas.getBoundingClientRect();
    const [gx, gy] = transformRef.current.invert([clientX - rect.left, clientY - rect.top]);
    const k = transformRef.current.k;
    let best: UNode | null = null;
    let bestD = Infinity;
    for (const n of dataRef.current.nodes) {
      if (n.x === undefined || n.y === undefined) continue;
      const d = Math.hypot(n.x - gx, n.y - gy);
      // Hit target is larger than the mark (≥ ~10px on screen).
      if (d <= Math.max(n.r + 3 / k, 10 / k) && d < bestD) {
        best = n;
        bestD = d;
      }
    }
    return best;
  };

  const setHover = (n: UNode | null) => {
    if (hoverRef.current === n) return;
    hoverRef.current = n;
    const d = dataRef.current;
    d.focus = neighbourhood(d.links, n?.id ?? d.selectedId);
    schedule();
  };

  return (
    <div ref={wrapRef} className="absolute inset-0">
      <canvas
        ref={canvasRef}
        tabIndex={0}
        role="img"
        aria-label={label}
        className="block h-full w-full cursor-grab touch-none outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent/50 active:cursor-grabbing"
        onPointerMove={(e) => {
          if (e.buttons) return;
          const n = nodeAt(e.clientX, e.clientY);
          setHover(n);
          e.currentTarget.style.cursor = n ? "pointer" : "";
          const rect = e.currentTarget.getBoundingClientRect();
          setTip(n ? { x: e.clientX - rect.left, y: e.clientY - rect.top, w: rect.width, node: n } : null);
        }}
        onPointerLeave={() => {
          setHover(null);
          setTip(null);
        }}
        onClick={(e) => {
          const n = nodeAt(e.clientX, e.clientY);
          onSelect(n ? n.id : null);
        }}
        onKeyDown={(e) => {
          const t = transformRef.current;
          const { w, h } = sizeRef.current;
          const pan = 60;
          const zoomTo = (f: number) => {
            const k = Math.max(0.08, Math.min(8, t.k * f));
            const cx = (w / 2 - t.x) / t.k;
            const cy = (h / 2 - t.y) / t.k;
            applyTransform({ k, x: w / 2 - cx * k, y: h / 2 - cy * k }, false);
          };
          const moves: Record<string, () => void> = {
            "+": () => zoomTo(1.25),
            "=": () => zoomTo(1.25),
            "-": () => zoomTo(0.8),
            "0": () => fit(!reducedMotion),
            ArrowLeft: () => applyTransform({ k: t.k, x: t.x + pan, y: t.y }, false),
            ArrowRight: () => applyTransform({ k: t.k, x: t.x - pan, y: t.y }, false),
            ArrowUp: () => applyTransform({ k: t.k, x: t.x, y: t.y + pan }, false),
            ArrowDown: () => applyTransform({ k: t.k, x: t.x, y: t.y - pan }, false),
          };
          const fn = moves[e.key];
          if (fn) {
            e.preventDefault();
            interactedRef.current = true;
            fn();
          }
        }}
      />
      {tip && nodes.includes(tip.node) && (
        <div
          role="tooltip"
          className="pointer-events-none absolute z-10 max-w-[18rem] rounded-[var(--radius-md)] border border-border bg-surface px-3 py-2 shadow-md"
          style={{
            left: Math.max(8, Math.min(tip.x + 14, tip.w - 290)),
            top: tip.y + 14,
          }}
        >
          <p className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide" style={{ color: `var(${toneVar(tip.node.type)})` }}>
            <span className="h-1.5 w-1.5 rounded-full" style={{ background: `var(${toneVar(tip.node.type)})` }} />
            {entityMeta(tip.node.type).label}
            {tip.node.dead && tip.node.status && <span className="text-faint line-through">{tip.node.status.toLowerCase()}</span>}
          </p>
          <p className="mt-0.5 text-[13px] font-medium leading-snug text-fg">{truncate(tip.node.label || "Untitled", 80)}</p>
          {tip.node.title && tip.node.title !== tip.node.label && <p className="mt-0.5 line-clamp-3 text-[12px] leading-snug text-muted">{tip.node.title}</p>}
        </div>
      )}
    </div>
  );
}
