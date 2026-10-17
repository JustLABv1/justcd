import { NextResponse } from "next/server"

// Resolve deployment configuration at request time, including in standalone images.
export const dynamic = "force-dynamic"

export async function GET(_request: Request, { params }: { params: Promise<{ slug?: string[] }> }) {
  const { slug = [] } = await params
  const page = slug.map(encodeURIComponent).join("/")
  const configuredURL = process.env.JUSTCD_DOCS_URL?.trim()
  let destination: URL

  if (configuredURL) {
    try {
      destination = new URL(configuredURL)
      if (!["http:", "https:"].includes(destination.protocol)) throw new Error("Invalid protocol")
    } catch {
      return NextResponse.json({ error: "JUSTCD_DOCS_URL must be an absolute HTTP or HTTPS URL." }, { status: 503 })
    }
    // The docs service is built for hosting at the root of a dedicated origin.
    destination.pathname = `/${page}`
    destination.search = ""
    destination.hash = ""
  } else {
    destination = new URL(`https://github.com/JustLABv1/justcd/blob/main/services/docs/content/docs/${page || "index"}.md`)
  }

  return NextResponse.redirect(destination, { status: 307, headers: { "Cache-Control": "no-store" } })
}
