import { useId, type ReactNode } from "react"
import { Frame, FramePanel } from "@/components/reui/frame"

export function SettingsSection({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  const id = useId()
  return <section aria-labelledby={id} className="grid min-w-0 gap-3 md:grid-cols-[200px_minmax(0,1fr)] md:gap-8">
    <div className="space-y-1.5 md:pt-4">
      <h2 id={id} className="text-sm font-semibold">{title}</h2>
      <p className="text-sm leading-5 text-muted-foreground">{description}</p>
    </div>
    <div className="min-w-0">{children}</div>
  </section>
}

export function SettingsGroup({ children }: { children: ReactNode }) {
  return <Frame dense stacked spacing="default">{children}</Frame>
}

export function SettingsRow({ label, description, value, action }: { label: string; description?: ReactNode; value?: ReactNode; action?: ReactNode }) {
  return <FramePanel className="flex min-w-0 flex-wrap items-center gap-x-5 gap-y-3 sm:flex-nowrap">
    <div className="min-w-0 flex-1 basis-40 space-y-1">
      <h3 className="text-sm font-medium">{label}</h3>
      {description && <div className="text-sm leading-5 text-muted-foreground">{description}</div>}
    </div>
    {value && <div className="min-w-0 text-sm sm:max-w-[45%]">{value}</div>}
    {action && <div className="shrink-0">{action}</div>}
  </FramePanel>
}
