import { ServiceStatePage } from "@/components/service-state"

export const metadata = {
  title: "Maintenance · JustCD",
  robots: { index: false },
}

export default function MaintenancePage() {
  return <ServiceStatePage state="maintenance" />
}
