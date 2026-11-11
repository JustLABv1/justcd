import type { NextRequest } from "next/server"

const backendURL = (
  process.env.JUSTCD_API_URL ?? "http://localhost:8080"
).replace(/\/$/, "")

async function proxy(
  request: NextRequest,
  context: { params: Promise<{ path: string[] }> }
) {
  const { path } = await context.params
  const target = new URL(
    `/api/${path.map(encodeURIComponent).join("/")}`,
    backendURL
  )
  target.search = request.nextUrl.search

  const headers = new Headers()
  for (const name of [
    "accept",
    "content-type",
    "cookie",
    "x-csrf-token",
    "idempotency-key",
    "origin",
    "sec-fetch-site",
    "mcp-protocol-version",
  ]) {
    const value = request.headers.get(name)
    if (value) headers.set(name, value)
  }

  // Only machine protocol endpoints accept bearer credentials.
  if (path[0] === "v1" && (path[1] === "agents" || (path[1] === "mcp" && path.length === 2))) {
    const authorization = request.headers.get("authorization")
    if (authorization) headers.set("authorization", authorization)
  }

  const response = await fetch(target, {
    method: request.method,
    headers,
    body:
      request.method === "GET" || request.method === "HEAD"
        ? undefined
        : request.body,
    duplex: "half",
    signal: request.signal,
    cache: "no-store",
    redirect: "manual",
  } as RequestInit & { duplex: "half" })

  const responseHeaders = new Headers()
  for (const name of [
    "content-type",
    "cache-control",
    "location",
    "www-authenticate",
    "retry-after",
    "content-disposition",
    "x-content-type-options",
  ]) {
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
