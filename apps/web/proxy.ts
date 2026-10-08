import { NextResponse, type NextRequest } from "next/server";

// Fast-path auth redirect: if there is no session cookie, send app routes to /login.
// The API remains the source of truth for authentication (it validates every request).
const PUBLIC = ["/login", "/setup", "/api", "/_next", "/favicon", "/icon"];

export function proxy(req: NextRequest) {
  const { pathname, search } = req.nextUrl;
  if (PUBLIC.some((p) => pathname.startsWith(p)) || pathname.includes(".")) return NextResponse.next();
  if (!req.cookies.get("iv_session")) {
    const url = req.nextUrl.clone();
    url.pathname = "/login";
    url.search = `?next=${encodeURIComponent(pathname + search)}`;
    return NextResponse.redirect(url);
  }
  return NextResponse.next();
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
