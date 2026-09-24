export class APIError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly payload: unknown,
    readonly request: { method: string; path: string },
    cause?: unknown,
  ) {
    super(message, cause === undefined ? undefined : { cause })
    this.name = "APIError"
  }
}

function csrfToken() {
  if (typeof document === "undefined") return ""
  const match = document.cookie.match(/(?:^|;\s*)justcd_csrf=([^;]+)/)
  return match ? decodeURIComponent(match[1]) : ""
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase()
  const headers = new Headers(init.headers)
  if (init.body && !headers.has("content-type")) headers.set("content-type", "application/json")
  if (method !== "GET" && method !== "HEAD") {
    const csrf = csrfToken()
    if (csrf) headers.set("x-csrf-token", csrf)
  }
  let response: Response
  try {
    response = await fetch(path, {
      ...init,
      headers,
      cache: "no-store",
      credentials: "same-origin",
    })
  } catch (cause) {
    if (cause instanceof Error && cause.name === "AbortError") throw cause
    throw new APIError(errorMessage(cause), 0, null, { method, path }, cause)
  }

  const contentType = response.headers.get("content-type") ?? ""
  let payload: unknown
  try {
    payload = contentType.includes("json") ? await response.json() : await response.text()
  } catch (cause) {
    throw new APIError(
      "The server returned a response that could not be read.",
      response.status,
      null,
      { method, path },
      cause,
    )
  }
  if (!response.ok) {
    const message = typeof payload === "object" && payload && "error" in payload
      ? String((payload as { error: unknown }).error)
      : response.statusText || "Request failed"
    throw new APIError(message, response.status, payload, { method, path })
  }
  return payload as T
}

export function apiPost<T>(path: string, value?: unknown) {
  return api<T>(path, { method: "POST", body: JSON.stringify(value ?? {}) })
}

export function apiDelete<T>(path: string) {
  return api<T>(path, { method: "DELETE" })
}

export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "Something went wrong. Please try again."
}
