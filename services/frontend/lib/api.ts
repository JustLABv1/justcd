export type APIErrorCategory =
  | "validation"
  | "authentication"
  | "authorization"
  | "not_found"
  | "request"
  | "conflict"
  | "rate_limit"
  | "dependency"
  | "git"
  | "kubernetes"
  | "render"
  | "plan"
  | "sync"
  | "database"
  | "internal"
  | "network"
  | "protocol"
  | string

export type APIErrorMetadata = {
  schemaVersion: number | null
  code: string
  category: APIErrorCategory
  retryable: boolean
  remediation: string
  remediationUrl: string | null
}

export class APIError extends Error {
  constructor(
    message: string,
    status: number,
    payload: unknown,
    request: { method: string; path: string },
    cause?: unknown,
    metadata: Partial<APIErrorMetadata> = {}
  ) {
    super(message, cause === undefined ? undefined : { cause })
    this.name = "APIError"
    this.status = status
    this.payload = payload
    this.request = request
    this.schemaVersion = metadata.schemaVersion ?? null
    this.code = metadata.code ?? "request.failed"
    this.category = metadata.category ?? "request"
    this.retryable = metadata.retryable ?? false
    this.remediation =
      metadata.remediation ?? "Review the request and try again."
    this.remediationUrl = metadata.remediationUrl ?? null
  }

  readonly status: number
  readonly payload: unknown
  readonly request: { method: string; path: string }
  readonly schemaVersion: number | null
  readonly code: string
  readonly category: APIErrorCategory
  readonly retryable: boolean
  readonly remediation: string
  readonly remediationUrl: string | null
}

function fallbackErrorMetadata(status: number): APIErrorMetadata {
  if (status === 0) {
    return {
      schemaVersion: null,
      code: "network.unreachable",
      category: "network",
      retryable: false,
      remediation:
        "Check your network connection and refresh the current state before retrying.",
      remediationUrl: null,
    }
  }
  if (status === 400 || status === 422) {
    return {
      schemaVersion: null,
      code: "request.invalid",
      category: "validation",
      retryable: false,
      remediation:
        "Review the submitted values and correct the invalid fields.",
      remediationUrl: null,
    }
  }
  if (status === 401) {
    return {
      schemaVersion: null,
      code: "authentication.required",
      category: "authentication",
      retryable: false,
      remediation: "Sign in again to continue.",
      remediationUrl: "/login",
    }
  }
  if (status === 403) {
    return {
      schemaVersion: null,
      code: "authorization.denied",
      category: "authorization",
      retryable: false,
      remediation:
        "Confirm that your account has the required project role or administrator access.",
      remediationUrl: null,
    }
  }
  if (status === 404) {
    return {
      schemaVersion: null,
      code: "resource.not_found",
      category: "not_found",
      retryable: false,
      remediation:
        "Refresh the page and confirm that the resource still exists.",
      remediationUrl: null,
    }
  }
  if (status === 408) {
    return {
      schemaVersion: null,
      code: "request.timeout",
      category: "request",
      retryable: false,
      remediation:
        "Refresh the current state before trying this request again.",
      remediationUrl: null,
    }
  }
  if (status === 409) {
    return {
      schemaVersion: null,
      code: "state.conflict",
      category: "conflict",
      retryable: false,
      remediation: "Refresh and review the latest state before trying again.",
      remediationUrl: null,
    }
  }
  if (status === 429) {
    return {
      schemaVersion: null,
      code: "request.rate_limited",
      category: "rate_limit",
      retryable: true,
      remediation: "Wait briefly, then retry the request.",
      remediationUrl: null,
    }
  }
  if (status === 502 || status === 503 || status === 504) {
    return {
      schemaVersion: null,
      code: "dependency.unavailable",
      category: "dependency",
      retryable: true,
      remediation:
        "Check the connected service and retry when it is available.",
      remediationUrl: null,
    }
  }
  if (status >= 500) {
    return {
      schemaVersion: null,
      code: "internal.error",
      category: "internal",
      retryable: false,
      remediation:
        "Retry after checking JustCD service health. If the problem continues, contact an administrator.",
      remediationUrl: null,
    }
  }
  return {
    schemaVersion: null,
    code: "request.failed",
    category: "request",
    retryable: false,
    remediation: "Review the request and try again.",
    remediationUrl: null,
  }
}

function errorMetadata(status: number, payload: unknown): APIErrorMetadata {
  const fallback = fallbackErrorMetadata(status)
  if (typeof payload !== "object" || payload === null || Array.isArray(payload))
    return fallback
  const value = payload as Record<string, unknown>
  if (value.schemaVersion !== 1) return fallback

  const remediationUrl =
    value.remediationUrl === undefined
      ? fallback.remediationUrl
      : typeof value.remediationUrl === "string" &&
          value.remediationUrl.startsWith("/") &&
          !value.remediationUrl.startsWith("//") &&
          !value.remediationUrl.includes("\\")
        ? value.remediationUrl
        : null

  return {
    schemaVersion: 1,
    code:
      typeof value.code === "string" && value.code.trim()
        ? value.code
        : fallback.code,
    category:
      typeof value.category === "string" && value.category.trim()
        ? value.category
        : fallback.category,
    retryable:
      typeof value.retryable === "boolean"
        ? value.retryable
        : fallback.retryable,
    remediation:
      typeof value.remediation === "string" && value.remediation.trim()
        ? value.remediation
        : fallback.remediation,
    remediationUrl,
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
  if (init.body && !headers.has("content-type"))
    headers.set("content-type", "application/json")
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
    throw new APIError(
      errorMessage(cause),
      0,
      null,
      { method, path },
      cause,
      fallbackErrorMetadata(0)
    )
  }

  const contentType = response.headers.get("content-type") ?? ""
  let payload: unknown
  try {
    payload = contentType.includes("json")
      ? await response.json()
      : await response.text()
  } catch (cause) {
    throw new APIError(
      "The server returned a response that could not be read.",
      response.status,
      null,
      { method, path },
      cause,
      {
        schemaVersion: null,
        code: "response.unreadable",
        category: "protocol",
        retryable: false,
        remediation:
          "Retry the request. If it continues, check the JustCD API service and response format.",
      }
    )
  }
  if (!response.ok) {
    const message =
      typeof payload === "object" && payload && "error" in payload
        ? String((payload as { error: unknown }).error)
        : response.statusText || "Request failed"
    throw new APIError(
      message,
      response.status,
      payload,
      { method, path },
      undefined,
      errorMetadata(response.status, payload)
    )
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
  return error instanceof Error
    ? error.message
    : "Something went wrong. Please try again."
}
