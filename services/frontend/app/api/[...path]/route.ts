import type { NextRequest } from "next/server"

const backendURL = (process.env.JUSTCD_API_URL ?? "http://localhost:8080").replace(/\/$/, "")

async function proxy(
  request: NextRequest,
  context: { params: Promise<{ path: string[] }> },
) {
  const { path } = await context.params
  const target = new URL(`/api/${path.map(encodeURIComponent).join("/")}`, backendURL)
  target.search = request.nextUrl.search

  const headers = new Headers()
  for (const name of ["accept", "content-type", "cookie", "x-csrf-token"]) {
    const value = request.headers.get(name)
    if (value) headers.set(name, value)
  }

  const response = await fetch(target, {
    method: request.method,
    headers,
    body: request.method === "GET" || request.method === "HEAD" ? undefined : await request.arrayBuffer(),
    cache: "no-store",
    redirect: "manual",
  })

  const responseHeaders = new Headers()
  for (const name of ["content-type", "cache-control", "location", "www-authenticate"]) {
    const value = response.headers.get(name)
    if (value) responseHeaders.set(name, value)
  }
  for (const cookie of response.headers.getSetCookie()) {
    responseHeaders.append("set-cookie", cookie)
  }
  responseHeaders.set("cache-control", "no-store")

  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers: responseHeaders,
  })
}

export const GET = proxy
export const HEAD = proxy
export const POST = proxy
export const PUT = proxy
export const PATCH = proxy
export const DELETE = proxy
