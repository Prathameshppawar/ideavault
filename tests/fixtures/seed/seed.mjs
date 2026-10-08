#!/usr/bin/env node
// Local development / E2E seed: builds a realistic vault through the public API.
// It is NOT used by the application at runtime. Usage:
//   API=http://localhost:8080 EMAIL=you@example.com PASSWORD=... node tests/fixtures/seed/seed.mjs
// Creates the owner if the vault is empty, otherwise logs in.
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const API = (process.env.API || "http://localhost:8080").replace(/\/$/, "");
const EMAIL = process.env.EMAIL || "owner@example.com";
const PASSWORD = process.env.PASSWORD || "correct-horse-battery";
const here = dirname(fileURLToPath(import.meta.url));
const fixtures = join(here, "..", "imports");
let cookie = "";

async function call(method, path, body, extraHeaders = {}) {
  const headers = { Accept: "application/json", "X-IdeaVault-CSRF": "1", ...extraHeaders };
  if (cookie) headers.Cookie = cookie;
  let payload;
  if (body instanceof FormData) payload = body;
  else if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const res = await fetch(API + path, { method, headers, body: payload });
  const setCookie = res.headers.get("set-cookie");
  if (setCookie) cookie = setCookie.split(";")[0];
  const text = await res.text();
  const json = text ? JSON.parse(text) : null;
  if (!res.ok) throw new Error(`${method} ${path} → ${res.status}: ${json?.error ?? text}`);
  return json;
}

const post = (p, b) => call("POST", p, b);
const get = (p) => call("GET", p);

async function login() {
  const st = await get("/v1/auth/status");
  if (st.setup_required) await post("/v1/auth/setup", { email: EMAIL, password: PASSWORD, display_name: "Owner" });
  else await post("/v1/auth/login", { email: EMAIL, password: PASSWORD });
}

async function record(ideaId, branchId, kind, statement, extra = {}) {
  return post("/v1/knowledge", { idea_id: ideaId, branch_id: branchId, kind, statement, origin: "SOURCE", source_excerpt: statement, ...extra });
}

async function checkpoint(ideaId, branchId, title) {
  return post(`/v1/ideas/${ideaId}/checkpoints`, { branch_id: branchId, title });
}

async function seedBiker() {
  const { idea, branch } = await post("/v1/ideas", {
    title: "Biker Community Platform",
    origin_text: "I have an idea for a biker community platform where riders can create trips, invite friends and share routes. Safety matters because groups ride long distances.",
    summary: "A community platform for motorcycle riders to plan group trips, share routes and ride more safely together.",
    tags: ["community", "mobility", "safety"],
  });
  const b = branch.id;
  await record(idea.id, b, "assumption", "Riders already coordinate trips in WhatsApp groups and would switch for better route sharing", { risk: "HIGH", validation_method: "Interview 10 ride-group organisers" });
  await record(idea.id, b, "question", "Should trips be public by default or invite-only?");
  const d1 = await record(idea.id, b, "decision", "Focus v1 on trip planning for small private groups", {
    rationale: "Private groups reduce moderation and safety risk while we learn",
    alternatives: [{ option: "Open public trip discovery", reason_rejected: "Requires moderation and trust & safety from day one" }],
  });
  await checkpoint(idea.id, b, "Problem framing");
  const noMarket = await record(idea.id, b, "decision", "We will not build a marketplace in v1", {
    rationale: "Moderation and payments would delay launch by months",
    alternatives: [{ option: "Escrow marketplace for paid guided rides", reason_rejected: "Compliance burden" }],
  });
  await record(idea.id, b, "evidence", "Two competing rider marketplaces shut down in 2025 citing fraud costs", { stance: "SUPPORTS", target_item_id: noMarket.id, strength: "MODERATE" });
  await record(idea.id, b, "insight", "Safety features (live location, regroup points) are the differentiator, not social feeds", { importance: "HIGH" });
  await record(idea.id, b, "action", "Prototype the trip planner with regroup points", { priority: "HIGH" });
  const cp2 = await checkpoint(idea.id, b, "Community-first, no marketplace");
  await record(idea.id, b, "evidence", "8 of 10 organisers interviewed said regroup coordination is their biggest pain", { stance: "SUPPORTS", strength: "STRONG", target_item_id: d1.id });
  await record(idea.id, b, "insight", "Organisers, not riders, are the first customers", { importance: "HIGH" });
  await record(idea.id, b, "decision", "Build a marketplace for verified ride guides only", {
    supersedes: noMarket.id,
    reverses: true,
    rationale: "Interviews showed organisers want to pay verified guides; verification limits fraud",
  });
  const cp3 = await checkpoint(idea.id, b, "Research: organisers first");
  await post(`/v1/checkpoints/${cp2.id}/fork`, {
    name: "Marketplace-free direction",
    description: "Explore the community-only path from before the marketplace reversal, keeping the interview research.",
    selections: [{ checkpoint_id: cp3.id, kinds: ["evidence", "insight"] }],
  });
  await post("/v1/artifacts/generate", { idea_id: idea.id, type: "ACTION_PLAN", generator: "template" });
  await post("/v1/artifacts/generate", { idea_id: idea.id, type: "IMPLEMENTATION_PROMPT", generator: "template" });
  await post("/v1/context-packs", { idea_id: idea.id, objective: "Pressure-test the guide marketplace decision" });
  await post("/v1/deltas", {
    idea_id: idea.id,
    provider: "chatgpt",
    text: "User: Here's my plan for the rider app.\n\nChatGPT: I'd suggest you should not build a marketplace at all; keep it community only. Also I think riders will pay for premium route packs. What about insurance partnerships?",
  });
  return idea;
}

async function seedClinic() {
  const { idea, branch } = await post("/v1/ideas", {
    title: "Clinic Management Platform",
    origin_text: "Small clinics still run on paper appointment books; I want a simple scheduling and records tool they can run on a single laptop.",
    summary: "Lightweight scheduling and patient records for small clinics.",
    tags: ["health", "saas"],
  });
  const b = branch.id;
  await record(idea.id, b, "decision", "Start with appointment scheduling before medical records", { rationale: "Records require compliance work; scheduling is immediately useful" });
  await record(idea.id, b, "assumption", "Clinic receptionists will adopt a tool if it works offline", { risk: "MEDIUM" });
  await record(idea.id, b, "question", "Which records regulations apply in the first target market?");
  await checkpoint(idea.id, b, "Scheduling first");
  await post(`/v1/ideas/${idea.id}/conclude`, { outcome: "PARK", note: "Parking until I have a clinic partner to pilot with." });
  return idea;
}

async function seedAutomation() {
  const { idea, branch } = await post("/v1/ideas", {
    title: "AI Automation Business",
    origin_text: "Offer AI automation for small businesses: inbox triage, invoice extraction, follow-up drafting.",
    tags: ["ai", "services"],
  });
  await record(idea.id, branch.id, "insight", "Invoice extraction is the most repeatable service", { importance: "MEDIUM" });
  await record(idea.id, branch.id, "question", "Productise as SaaS or stay a service business?");
  return idea;
}

async function importFixture(rel, extra = {}) {
  const form = new FormData();
  const data = await readFile(join(fixtures, rel));
  form.append("file", new Blob([data]), rel.split("/").pop());
  for (const [k, v] of Object.entries(extra)) form.append(k, String(v));
  return call("POST", "/v1/imports", form);
}

async function waitImport(id, want) {
  for (let i = 0; i < 60; i++) {
    const d = await get(`/v1/imports/${id}`);
    if (want.includes(d.import.status)) return d;
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(`import ${id} did not reach ${want}`);
}

async function main() {
  await login();
  const ideas = await get("/v1/ideas");
  if (ideas.ideas.some((i) => i.title === "Biker Community Platform" && i.stats?.checkpoints >= 3)) {
    console.log("Seed data already present; nothing to do.");
    return;
  }
  const biker = await seedBiker();
  await seedClinic();
  await seedAutomation();
  // Import Center: a ChatGPT export previewed and committed into a new idea.
  const imp = await importFixture("chatgpt/conversations.json");
  const prev = await waitImport(imp.id, ["PREVIEW", "FAILED"]);
  const items = prev.items.filter((it) => it.status === "PENDING");
  if (items.length) {
    await post(`/v1/imports/${imp.id}/commit`, { extract: true, items: items.map((it, i) => ({ item_id: it.id, include: true, new_idea: i === 0, idea_id: i === 0 ? undefined : biker.id })) });
    await waitImport(imp.id, ["COMPLETED", "PARTIAL", "FAILED"]);
  }
  await importFixture("claude/conversations.json");
  console.log("Seeded: Biker Community Platform (3 checkpoints, reversal, selective fork, artifacts, context pack, delta), Clinic (parked), AI Automation, imports.");
}

main().catch((e) => {
  console.error(e.message);
  process.exit(1);
});
