"use client"

import Link from "next/link"
import { Breadcrumb } from "@/components/panelui/breadcrumb"

export type Crumb = { label: string; href?: string }

/** The trail above a nested page. Deep paths fold their middle crumbs instead of wrapping. */
export function PageBreadcrumb({ crumbs, className, size }: { crumbs: Crumb[]; className?: string; size?: "sm" | "default" }) {
  if (crumbs.length < 2) return null
  return <Breadcrumb className={className} size={size} maxItems={4}>
    <Breadcrumb.List>
      {crumbs.map((crumb, index) => <Breadcrumb.Item key={`${crumb.label}-${index}`}>
        {crumb.href && index < crumbs.length - 1
          ? <Breadcrumb.Link render={<Link href={crumb.href} />}>{crumb.label}</Breadcrumb.Link>
          : <Breadcrumb.Page>{crumb.label}</Breadcrumb.Page>}
      </Breadcrumb.Item>)}
    </Breadcrumb.List>
  </Breadcrumb>
}
