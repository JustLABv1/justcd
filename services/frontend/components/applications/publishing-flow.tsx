"use client"

import { useCallback, useState } from "react"
import { Flow } from "@/components/panelui/flow"
import { PublishingSection } from "./publishing-fields"
import type { ApplicationTemplate } from "./template-catalog"
import type {
  ApprovedPublishingProfile,
  ServiceSelection,
} from "@/lib/publishing"

type Step = {
  id: string
  title: string
  detail: string
  badge: string
  color?: "info" | "success" | "warning" | "primary"
}

const columnGap = 214
const rowGap = 160

function buildDetail(runtime: ApplicationTemplate["runtime"]) {
  switch (runtime) {
    case "static":
      return "GitLab packages your site into an image and stores it."
    case "python-static":
      return "GitLab runs your build, packages the site and stores it."
    default:
      return "GitLab builds your app image and stores it."
  }
}

/** Whether the canvas has room to lay the steps out in a row. */
function useRoom(least: number) {
  const [wide, setWide] = useState(true)
  const watch = useCallback(
    (node: HTMLElement | null) => {
      if (!node) return
      const observer = new ResizeObserver(([entry]) =>
        setWide(entry.contentRect.width >= least)
      )
      observer.observe(node)
      return () => observer.disconnect()
    },
    [least]
  )
  return [wide, watch] as const
}

/**
 * Shows, in plain words, what JustCD does after the user uploads their
 * project. It is read-only and built from the template and, when known, the
 * chosen profile and services, so it never promises more than will happen.
 */
export function PublishingFlow({
  template,
  profile,
  services,
  name,
  description = "JustCD does these steps for you. You do not need to use Git or Kubernetes yourself.",
}: {
  template: ApplicationTemplate
  profile?: ApprovedPublishingProfile
  services?: ServiceSelection
  name?: string
  description?: string
}) {
  const [wide, watch] = useRoom(900)
  const manual = profile?.syncPolicy === "manual"
  const host =
    profile?.hostSuffix && name ? `${name}.${profile.hostSuffix}` : undefined
  const namespace = profile
    ? profile.namespaceMode === "per-application"
      ? `${profile.namespacePrefix ?? ""}${name || "<app-name>"}`
      : profile.namespace
    : undefined
  const steps: Step[] = [
    {
      id: "upload",
      title: "Your project",
      detail: "You upload your project as a ZIP file.",
      badge: "You",
    },
    {
      id: "justcd",
      title: "JustCD",
      detail: "Checks your files and adds the build and deployment setup.",
      badge: "Automatic",
      color: "info",
    },
    {
      id: "gitlab",
      title: "GitLab",
      detail: "Keeps your code in a private project with a build pipeline.",
      badge: "Automatic",
      color: "info",
    },
    {
      id: "build",
      title: "Build",
      detail: buildDetail(template.runtime),
      badge: "Automatic",
      color: "info",
    },
    {
      id: "cluster",
      title: profile?.clusterName || "Your platform",
      detail: profile
        ? `${manual ? "You review, then JustCD deploys it" : "JustCD deploys it"}${host ? `. Your app: ${host}` : ` to ${namespace}.`}`
        : "JustCD deploys your app to the Kubernetes cluster and gives you its address.",
      badge: manual ? "You approve" : "Automatic",
      color: manual ? "warning" : "success",
    },
  ]
  const database = !!services?.database
  const vault = services?.vaultSecrets.length ?? 0
  const extras = [
    database && {
      id: "database",
      title: "Database",
      detail: "A dedicated PostgreSQL database for your app.",
    },
    vault > 0 && {
      id: "vault",
      title: vault === 1 ? "Vault secret" : `${vault} Vault secrets`,
      detail: "Synchronized from Vault; values never enter Git.",
    },
  ].filter(Boolean) as { id: string; title: string; detail: string }[]

  const last = steps.length - 1
  const place = (index: number) =>
    wide ? { x: index * columnGap, y: 0 } : { x: 0, y: index * rowGap }
  const placeExtra = (index: number) =>
    wide
      ? {
          x: last * columnGap - index * columnGap,
          y: 210,
        }
      : { x: 0, y: (steps.length + index) * rowGap }
  // Equal heights keep the connecting lines straight.
  const nodeSize = wide ? "h-40 w-44" : "h-32 w-72"
  const rows = wide ? 1 : steps.length + extras.length
  const height = wide ? (extras.length ? 420 : 250) : rows * rowGap + 20

  return (
    <PublishingSection
      title="What happens when you publish"
      description={description}
    >
      <div
        ref={watch}
        className="overflow-hidden rounded-2xl bg-card ring-1 ring-border dark:bg-muted/45 dark:ring-0"
        style={{ height }}
      >
        <Flow
          // Laid out and fitted afresh when the shape of the diagram changes.
          key={`${wide}-${extras.map((e) => e.id).join()}`}
          fitViewOnMount
          fitViewPadding={wide ? 24 : 12}
          panOnDrag={false}
          zoomOnPinch={false}
          label={`What happens when you publish a ${template.name} app: ${steps.map((s) => s.title).join(", then ")}`}
          className="h-full rounded-none border-0"
        >
          <Flow.Background variant="dots" gap={22} />
          {steps.map((step, index) => (
            <Flow.Node
              key={step.id}
              id={step.id}
              position={place(index)}
              draggable={false}
              className={nodeSize}
            >
              <Flow.NodeHeader>
                <Flow.NodeDescription>Step {index + 1}</Flow.NodeDescription>
                <Flow.NodeBadge color={step.color ?? "muted"}>
                  {step.badge}
                </Flow.NodeBadge>
              </Flow.NodeHeader>
              <Flow.NodeContent>
                <Flow.NodeTitle>{step.title}</Flow.NodeTitle>
                <Flow.NodeDescription className="line-clamp-4 whitespace-normal">
                  {step.detail}
                </Flow.NodeDescription>
              </Flow.NodeContent>
            </Flow.Node>
          ))}
          {extras.map((extra, index) => (
            <Flow.Node
              key={extra.id}
              id={extra.id}
              position={placeExtra(index)}
              draggable={false}
              className={nodeSize}
            >
              <Flow.NodeContent>
                <Flow.NodeTitle>{extra.title}</Flow.NodeTitle>
                <Flow.NodeDescription className="whitespace-normal">
                  {extra.detail}
                </Flow.NodeDescription>
              </Flow.NodeContent>
            </Flow.Node>
          ))}
          {steps.slice(1).map((step, index) => (
            <Flow.Edge
              key={step.id}
              from={steps[index].id}
              to={step.id}
              variant="smoothstep"
              gap={10}
              arrow
              animated
              fromSide={wide ? "right" : "bottom"}
              toSide={wide ? "left" : "top"}
            />
          ))}
          {extras.map((extra, index) => (
            <Flow.Edge
              key={extra.id}
              from={wide || index === 0 ? "cluster" : extras[index - 1].id}
              to={extra.id}
              variant="smoothstep"
              dashed
              color="info"
              fromSide="bottom"
              toSide="top"
            />
          ))}
        </Flow>
      </div>
      <ol className="sr-only">
        {steps.map((step) => (
          <li key={step.id}>
            {step.title}: {step.detail}
          </li>
        ))}
        {extras.map((extra) => (
          <li key={extra.id}>
            {extra.title}: {extra.detail}
          </li>
        ))}
      </ol>
    </PublishingSection>
  )
}
