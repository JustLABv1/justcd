import "./globals.css"
import { ThemeProvider } from "@/components/theme-provider"
import { ToastProvider } from "@/components/toast-provider"
import { ServiceAvailability } from "@/components/service-availability"
import { ServiceStatePage } from "@/components/service-state"
import { connection } from "next/server"

export const metadata = {
  title: "JustCD — continuous delivery",
  description:
    "Git-driven Kubernetes delivery with explicit plans, diffs, and safe approvals.",
}

export default async function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode
}>) {
  await connection()
  return (
    <html lang="en" suppressHydrationWarning className="font-sans antialiased">
      <body>
        <ThemeProvider>
          <ToastProvider>
            {process.env.JUSTCD_MAINTENANCE === "true" ? (
              <ServiceStatePage state="maintenance" />
            ) : (
              <ServiceAvailability>{children}</ServiceAvailability>
            )}
          </ToastProvider>
        </ThemeProvider>
      </body>
    </html>
  )
}
