import { ServiceStatePage } from "@/components/service-state"

export const metadata = {
  title: "Backend offline · JustCD",
  robots: { index: false },
}

export default function OfflinePage() {
  return <ServiceStatePage state="offline" />
}
