"use client"

import { useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { FormField, PageHeading, Panel } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { apiPost, errorMessage } from "@/lib/api"
import type { Project } from "@/lib/types"

export default function NewProjectPage() {
  const router = useRouter()
  const toast = useToast()
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [busy, setBusy] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true)
    try {
      const project = await apiPost<Project>("/api/v1/projects", { name, description })
      toast.success("Project created.")
      router.push(`/projects/${project.id}`)
    } catch (cause) { toast.error(errorMessage(cause), cause) } finally { setBusy(false) }
  }
  return <>
    <PageHeading title="Create a project" description="Create a delivery scope first. You can then add repository credentials, Kubernetes access, and applications." />
    <div className="max-w-2xl"><Panel title="Project details" description="This name is visible to project members."><form className="space-y-5 p-5" onSubmit={submit}>
      <FormField label="Project name" htmlFor="project-name"><Input id="project-name" placeholder="payments-platform" value={name} onChange={(event) => setName(event.target.value)} required maxLength={100} /></FormField>
      <FormField label="Description" htmlFor="project-description" hint="Optional; briefly explain what this project deploys."><Textarea id="project-description" className="min-h-24" placeholder="Services and infrastructure for…" value={description} onChange={(event) => setDescription(event.target.value)} maxLength={500} /></FormField>
      <div className="flex justify-end gap-2 border-t pt-4"><Button type="button" variant="outline" onClick={() => router.back()}>Cancel</Button><Button type="submit" loading={busy} loadingText="Creating project…">Create project</Button></div>
    </form></Panel></div>
  </>
}
