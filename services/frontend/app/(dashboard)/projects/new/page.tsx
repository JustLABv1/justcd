"use client"

import { useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormField, PageHeading, Panel } from "@/components/ui-kit"
import { apiPost, errorMessage } from "@/lib/api"
import type { Project } from "@/lib/types"

export default function NewProjectPage() {
  const router = useRouter()
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError("")
    try {
      const project = await apiPost<Project>("/api/v1/projects", { name, description })
      router.push(`/projects/${project.id}`)
    } catch (cause) { setError(errorMessage(cause)) } finally { setBusy(false) }
  }
  return <>
    <PageHeading eyebrow="Project setup" title="Create a project" description="Create a delivery scope first. You can then add repository credentials, Kubernetes access, and applications." />
    <div className="max-w-2xl"><Panel title="Project details" description="This name is visible to project members."><form className="space-y-5 p-5" onSubmit={submit}>
      <FormField label="Project name" htmlFor="project-name"><Input id="project-name" placeholder="payments-platform" value={name} onChange={(event) => setName(event.target.value)} required maxLength={100} /></FormField>
      <FormField label="Description" htmlFor="project-description" hint="Optional; briefly explain what this project deploys."><textarea id="project-description" className="min-h-24 w-full rounded-lg border bg-transparent px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring" placeholder="Services and infrastructure for…" value={description} onChange={(event) => setDescription(event.target.value)} maxLength={500} /></FormField>
      {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
      <div className="flex justify-end gap-2 border-t pt-4"><Button type="button" variant="outline" onClick={() => router.back()}>Cancel</Button><Button type="submit" disabled={busy}>{busy ? "Creating…" : "Create project"}</Button></div>
    </form></Panel></div>
  </>
}
