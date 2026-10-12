export async function GET(request: Request) {
  const headers = { "cache-control": "no-store", vary: "Cookie" }
  const backendURL = process.env.JUSTCD_API_URL ?? "http://localhost:8080"
  try {
    const response = await fetch(
      new URL("/api/v1/service-status", backendURL),
      {
        headers: { cookie: request.headers.get("cookie") ?? "" },
        cache: "no-store",
        signal: AbortSignal.timeout(5000),
        redirect: "error",
      }
    )
    const body = (await response.json()) as {
      status?: string
      maintenanceEnabled?: boolean
      adminBypass?: boolean
    }
    if (
      response.ok &&
      (body.status === "online" || body.status === "maintenance") &&
      typeof body.maintenanceEnabled === "boolean" &&
      typeof body.adminBypass === "boolean"
    )
      return Response.json(body, {
        status: body.status === "maintenance" ? 503 : 200,
        headers,
      })
  } catch {
    // The frontend stays available even when the backend cannot be reached.
  }
  return Response.json({ status: "offline" }, { status: 503, headers })
}
