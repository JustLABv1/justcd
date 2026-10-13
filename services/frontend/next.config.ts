import type { NextConfig } from "next"

const nextConfig: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Referrer-Policy", value: "no-referrer" },
          {
            key: "Permissions-Policy",
            value: "camera=(), microphone=(), geolocation=()",
          },
          {
            key: "Content-Security-Policy",
            value:
              "default-src 'self'; script-src 'self' 'unsafe-inline'" +
              (process.env.NODE_ENV === "development" ? " 'unsafe-eval'" : "") +
              "; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'" +
              (process.env.NODE_ENV === "development" ? " ws: wss:" : "") +
              "; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'",
          },
          ...(process.env.JUSTCD_ENV === "development"
            ? []
            : [
                { key: "Strict-Transport-Security", value: "max-age=31536000" },
              ]),
        ],
      },
    ]
  },
}

export default nextConfig
