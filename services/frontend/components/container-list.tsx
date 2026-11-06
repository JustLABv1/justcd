"use client"

import { useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Copy01Icon, Tick02Icon } from "@hugeicons/core-free-icons"
import { useToast } from "@/components/toast-provider"
import { Button } from "@/components/ui/button"
import { StatusBadge, type StatusTone } from "@/components/ui-kit"
import { parseImage } from "@/lib/container-image"
import type { ContainerDetails } from "@/lib/types"

function CopyImage({ image, name }: { image: string; name: string }) {
  const [copied, setCopied] = useState(false)
  const toast = useToast()
  return <Button type="button" variant="ghost" size="icon-xs" aria-label={copied ? `Copied image of ${name}` : `Copy image reference of ${name}`} onClick={() => { navigator.clipboard.writeText(image).then(() => { setCopied(true); window.setTimeout(() => setCopied(false), 1500) }, () => toast.error("Could not copy to the clipboard.")) }}>
    <HugeiconsIcon icon={copied ? Tick02Icon : Copy01Icon} strokeWidth={1.8} aria-hidden="true" />
  </Button>
}

/** Registry host, then the path with break opportunities after every slash. */
function Repository({ repository }: { repository: string }) {
  const [first, ...rest] = repository.split("/")
  const hasRegistry = rest.length > 0 && (first.includes(".") || first.includes(":") || first === "localhost")
  const path = (hasRegistry ? rest : [first, ...rest]).join("/")
  return <>
    {hasRegistry && <span className="block text-muted-foreground">{first}</span>}
    <span className="block break-words">{path.split("/").map((part, index, parts) => <span key={index}>{part}{index < parts.length - 1 && <>/<wbr /></>}</span>)}</span>
  </>
}

function containerTone(container: ContainerDetails): StatusTone {
  if (container.state === "running") return container.ready ? "success" : "warning"
  if (container.state === "terminated") return container.reason === "Completed" ? "success" : "destructive"
  if (container.state === "waiting") return container.reason && !["ContainerCreating", "PodInitializing"].includes(container.reason) ? "destructive" : "warning"
  return "neutral"
}

/** Images and per-container state for a pod, or the images a workload's template asks for. */
export function ContainerList({ containers, title }: { containers: ContainerDetails[]; title: string }) {
  if (!containers.length) return null
  return <section aria-label={title} className="space-y-2 border-t pt-4">
    <h4 className="text-sm font-semibold">{title} ({containers.length})</h4>
    <ul className="space-y-2">
      {containers.map((container) => {
        const image = parseImage(container.image)
        const state = container.state ? container.reason || container.state : ""
        const failing = container.state === "waiting" && Boolean(container.reason) && container.reason !== "ContainerCreating" && container.reason !== "PodInitializing"
        return <li key={`${container.init ? "init" : "app"}-${container.name}`} className="space-y-2 rounded-lg border bg-muted/15 p-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="min-w-0 text-sm font-medium break-all">{container.name}{container.init && <span className="ml-1.5 text-xs font-normal text-muted-foreground">init</span>}</span>
            {state && <StatusBadge status={state} tone={containerTone(container)} />}
          </div>
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0 space-y-1.5">
              <span title={container.image} className="inline-block max-w-full rounded-md bg-muted px-1.5 py-0.5 font-mono text-xs font-medium break-all">{image.digest ? "@" : ":"}{image.version}</span>
              <p title={container.image} className="font-mono text-xs leading-5"><Repository repository={image.repository} /></p>
            </div>
            <CopyImage image={container.image} name={container.name} />
          </div>
          {(container.restartCount ?? 0) > 0 && <p className={`text-xs ${failing ? "text-destructive" : "text-muted-foreground"}`}>{container.restartCount} {container.restartCount === 1 ? "restart" : "restarts"}</p>}
        </li>
      })}
    </ul>
  </section>
}
