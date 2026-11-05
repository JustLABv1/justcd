"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { useTheme } from "next-themes"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  Add01Icon, Audit01Icon, Folder01Icon, Home01Icon, Layers01Icon, Link01Icon, Moon01Icon, Rocket01Icon,
  Search01Icon, Settings02Icon, SidebarLeftIcon, Sun01Icon, SunMoonIcon, Task01Icon,
} from "@hugeicons/core-free-icons"
import { Command } from "@/components/panelui/command"
import { NavTile, type Tone } from "@/components/nav-tile"
import { LogoMark } from "@/components/logo"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import type { Application, ListResponse, Workspace } from "@/lib/types"

type PaletteApplication = Pick<Application, "id" | "name" | "workspaceId">

type CommandPaletteProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  isAdmin: boolean
  workspaces: Workspace[]
  workspaceId: string
  onSelectWorkspace: (id: string) => void
  onToggleSidebar: () => void
  onboardingIncomplete: boolean
}

const icon = (source: typeof Home01Icon, tone: Tone = "blue") => <NavTile icon={source} tone={tone} />

const adminSections = [
  { id: "oidc", title: "OIDC providers", keywords: ["sso", "sign-in", "groups"] },
  { id: "users", title: "Platform users", keywords: ["accounts", "local users", "access"] },
  { id: "ai", title: "AI settings", keywords: ["litellm", "model"] },
  { id: "maintenance", title: "Maintenance mode", keywords: ["restrict", "lock"] },
]

/** Global ⌘K launcher: jump to pages, applications and workspaces, or start common actions. */
export function CommandPalette({ open, onOpenChange, isAdmin, workspaces, workspaceId, onSelectWorkspace, onToggleSidebar, onboardingIncomplete }: CommandPaletteProps) {
  const router = useRouter()
  const { setTheme } = useTheme()
  const [applications, setApplications] = useState<PaletteApplication[]>([])
  const [loading, setLoading] = useState(false)
  const workspaceIds = workspaces.map((workspace) => workspace.id).join(",")

  // Applications are fetched when the palette opens so the list is fresh without polling.
  useEffect(() => {
    if (!open || !workspaceIds) return
    const controller = new AbortController()
    void Promise.resolve().then(async () => {
      if (controller.signal.aborted) return
      setLoading(true)
      try {
        const grouped = await Promise.all(workspaceIds.split(",").map((id) =>
          api<ListResponse<Application>>(`/api/v1/applications?workspaceId=${encodeURIComponent(id)}`, { signal: controller.signal })
            .then((response) => response.items.map(({ id: applicationId, name, workspaceId: owner }) => ({ id: applicationId, name, workspaceId: owner })), () => [])))
        if (!controller.signal.aborted) setApplications(grouped.flat())
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })
    return () => controller.abort()
  }, [open, workspaceIds])

  const go = (href: string) => () => router.push(href)
  const workspaceName = (id: string) => workspaces.find((workspace) => workspace.id === id)?.name
  const scoped = (id: string) => encodeURIComponent(id)

  return (
    <Command.Dialog open={open} onOpenChange={onOpenChange} hotkey="mod+k" label="Command menu">
      <Command label="JustCD commands">
        <Command.Input icon={<LogoMark className="size-6 text-foreground" />} placeholder="Search pages, applications and actions…" />
        <Command.List>
          <Command.Group heading="Go to">
            <Command.Item icon={icon(Home01Icon, "blue")} kind="Page" keywords={["home", "dashboard"]} onSelect={go("/")}>Overview</Command.Item>
            <Command.Item icon={icon(Layers01Icon, "violet")} kind="Page" keywords={["apps", "deployments"]} onSelect={go("/applications")}>Applications</Command.Item>
            <Command.Item icon={icon(Task01Icon, "amber")} kind="Page" keywords={["review", "inbox", "pending"]} onSelect={go("/approvals")}>Approvals</Command.Item>
            <Command.Item icon={icon(Audit01Icon, "teal")} kind="Page" keywords={["history", "log", "events"]} onSelect={go("/audit")}>Audit trail</Command.Item>
            {workspaceId && <Command.Item icon={icon(Link01Icon, "green")} kind="Page" keywords={["git", "cluster", "kubernetes", "credentials"]} onSelect={go(`/workspaces/${scoped(workspaceId)}/connections`)}>Connections</Command.Item>}
            {workspaceId && <Command.Item icon={icon(Settings02Icon, "blue")} kind="Page" onSelect={go(`/workspaces/${scoped(workspaceId)}`)}>Workspace settings</Command.Item>}
            {isAdmin && onboardingIncomplete && <Command.Item icon={icon(Rocket01Icon, "blue")} kind="Page" keywords={["onboarding", "setup", "checklist"]} onSelect={go("/onboarding")}>Get started</Command.Item>}
            {isAdmin && <Command.Item icon={icon(Settings02Icon, "blue")} kind="Page" keywords={["admin", "instance"]} onSelect={go("/settings")}>Instance settings</Command.Item>}
            <Command.Item icon={icon(Settings02Icon, "blue")} kind="Page" keywords={["password", "mfa", "2fa", "passkey"]} onSelect={go("/settings/security")}>Account security</Command.Item>
          </Command.Group>

          <Command.Group heading="Create">
            <Command.Item icon={icon(Add01Icon, "green")} kind="Action" keywords={["create", "deploy", "add"]} onSelect={go("/applications/new")}>New application</Command.Item>
            <Command.Item icon={icon(Rocket01Icon, "green")} kind="Action" keywords={["template", "publish", "build"]} onSelect={go("/applications/publish")}>Publish from a template</Command.Item>
            <Command.Item icon={icon(Add01Icon, "green")} kind="Action" keywords={["create", "team"]} onSelect={go("/workspaces/new")}>New workspace</Command.Item>
            {workspaceId && <Command.Item icon={icon(Link01Icon, "green")} kind="Action" keywords={["kubernetes", "connect", "add"]} onSelect={go(`/workspaces/${scoped(workspaceId)}/clusters/new`)}>Connect cluster</Command.Item>}
            {workspaceId && <Command.Item icon={icon(Link01Icon, "green")} kind="Action" keywords={["repository", "github", "gitlab", "connect", "add"]} onSelect={go(`/workspaces/${scoped(workspaceId)}/git-sources/new`)}>Connect Git source</Command.Item>}
          </Command.Group>

          {applications.length > 0 && <Command.Group heading="Applications">
            {applications.map((application) => <Command.Item key={application.id} value={`application-${application.id}`} textValue={application.name} icon={icon(Layers01Icon, "violet")} subtitle={workspaces.length > 1 ? workspaceName(application.workspaceId) : undefined} kind="Application" onSelect={go(`/applications/${scoped(application.id)}`)}>{application.name}</Command.Item>)}
          </Command.Group>}

          {workspaces.length > 0 && <Command.Group heading="Switch workspace">
            {workspaces.map((workspace) => <Command.Item key={workspace.id} value={`workspace-${workspace.id}`} textValue={workspace.name} icon={icon(Folder01Icon, "amber")} kind={workspace.id === workspaceId ? "Current" : "Workspace"} keywords={["workspace", "switch"]} onSelect={() => onSelectWorkspace(workspace.id)}>{workspace.name}</Command.Item>)}
          </Command.Group>}

          {isAdmin && <Command.Group heading="Administration">
            {adminSections.map((section) => <Command.Item key={section.id} icon={icon(Settings02Icon, "rose")} kind="Settings" keywords={section.keywords} onSelect={go(`/settings/${section.id}`)}>{section.title}</Command.Item>)}
          </Command.Group>}

          <Command.Group heading="Preferences">
            <Command.Item icon={icon(SidebarLeftIcon, "slate")} kind="Action" shortcut={["⌘", "B"]} keywords={["collapse", "expand", "navigation"]} onSelect={onToggleSidebar}>Toggle sidebar</Command.Item>
            <Command.Item icon={icon(Sun01Icon, "amber")} kind="Theme" keywords={["appearance", "light mode"]} onSelect={() => setTheme("light")}>Light theme</Command.Item>
            <Command.Item icon={icon(Moon01Icon, "violet")} kind="Theme" keywords={["appearance", "dark mode"]} onSelect={() => setTheme("dark")}>Dark theme</Command.Item>
            <Command.Item icon={icon(SunMoonIcon, "slate")} kind="Theme" keywords={["appearance", "auto"]} onSelect={() => setTheme("system")}>System theme</Command.Item>
          </Command.Group>

          {loading && <Command.Loading>Loading applications…</Command.Loading>}
          <Command.Empty />
        </Command.List>
        <Command.Footer>
          <Command.FooterAction keys={["↵"]}>Open</Command.FooterAction>
          <Command.FooterAction keys={["↑", "↓"]}>Navigate</Command.FooterAction>
          <Command.FooterAction keys={["esc"]}>Close</Command.FooterAction>
        </Command.Footer>
      </Command>
    </Command.Dialog>
  )
}

/** Icon button that opens the palette; the ⌘K hint lives in its tooltip. */
export function CommandPaletteButton({ onClick }: { onClick: () => void; collapsed?: boolean; compact?: boolean }) {
  return <Tooltip>
    <TooltipTrigger render={<Button type="button" variant="ghost" size="icon" aria-label="Search and commands" aria-keyshortcuts="Control+K Meta+K" onClick={onClick} />}>
      <HugeiconsIcon icon={Search01Icon} strokeWidth={1.8} className="size-4" aria-hidden="true" />
    </TooltipTrigger>
    <TooltipContent side="bottom">Search · ⌘K</TooltipContent>
  </Tooltip>
}
