export async function GET() {
  const headers = { "cache-control": "no-store" }
  if (process.env.JUSTCD_MAINTENANCE === "true") {
    return Response.json({ status: "maintenance" }, { status: 503, headers })
  }
  const backendURL = process.env.JUSTCD_API_URL ?? "http://localhost:8080"
  try {
    const response = await fetch(new URL("/healthz", backendURL), {
      cache: "no-store",
      signal: AbortSignal.timeout(5000),
      redirect: "error",
    })
    const body = (await response.json()) as { status?: string }
    if (response.ok && body.status === "ok")
      return Response.json({ status: "online" }, { headers })
  } catch {
    // The frontend stays available even when the backend cannot be reached.
  }
  return Response.json({ status: "offline" }, { status: 503, headers })
}
