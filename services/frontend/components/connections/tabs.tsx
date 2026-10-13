"use client"

import type { ReactNode } from "react"
import { useRouter } from "next/navigation"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { workspaceConnectionSections } from "@/components/connections/sections"

export function ConnectionTabs({ workspaceId, section, children }: { workspaceId: string; section: string; children: ReactNode }) {
  const router = useRouter()
  return <Tabs value={section} onValueChange={(value) => router.push(`/workspaces/${workspaceId}/connections/${value}`)}>
    <TabsList aria-label="Connection sections" layout="responsive" className="sticky top-0 z-10 bg-background">
      {workspaceConnectionSections.map((item) => <TabsTrigger key={item.id} value={item.id}>{item.title}</TabsTrigger>)}
    </TabsList>
    <TabsContent value={section}>{children}</TabsContent>
  </Tabs>
}
