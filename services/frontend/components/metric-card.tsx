"use client"
import Link from "next/link"
import type { ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Layers01Icon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Frame, FramePanel, FrameTitle } from "@/components/reui/frame"
import { IconTile } from "@/components/reui/icon-tile"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import type { HealthLevel } from "@/lib/connection-health"

export function MetricCard({
  title,
  subtitle,
  icon,
  value,
  valueClassName,
  suffix,
  status,
  loading,
  href,
  onClick,
  children,
}: {
  title: string
  subtitle: string
  icon: typeof Layers01Icon
  value: string
  valueClassName?: string
  suffix?: string
  status?: { label: string; level: HealthLevel }
  loading?: boolean
  href?: string
  onClick?: () => void
  children: ReactNode
}) {
  return (
    <Frame className="h-full min-w-0">
      <FramePanel className="flex min-w-0 flex-col gap-4">
        <div className="flex min-w-0 items-start gap-3">
          <IconTile variant="elevated" size="sm" aria-hidden="true">
            <HugeiconsIcon icon={icon} strokeWidth={1.8} />
          </IconTile>
          <div className="min-w-0">
            <FrameTitle className="font-medium">{title}</FrameTitle>
            <p className="mt-0.5 text-xs text-muted-foreground">{subtitle}</p>
          </div>
        </div>
        <div className="flex flex-wrap items-baseline gap-2">
          {loading ? (
            <Skeleton className="h-7 w-20 motion-reduce:animate-none" />
          ) : (
            <p
              className={`text-2xl font-semibold tracking-tight tabular-nums ${valueClassName ?? ""}`}
            >
              {value}
              {suffix && (
                <span className="ml-1 text-sm font-normal text-muted-foreground">
                  {" "}
                  {suffix}
                </span>
              )}
            </p>
          )}
          {status && (
            <Badge
              size="xs"
              variant={
                status.level === "critical"
                  ? "destructive-light"
                  : status.level === "warning"
                    ? "warning-light"
                    : status.level === "healthy"
                      ? "success-light"
                      : "secondary"
              }
            >
              {status.label}
            </Badge>
          )}
        </div>
        <div className="mt-auto flex min-h-16 flex-col justify-end gap-2.5">
          {children}
        </div>
        <Button
          variant="ghost"
          render={href ? <Link href={href} /> : undefined}
          nativeButton={!href}
          onClick={onClick}
          className="absolute inset-0 z-10 h-full w-full rounded-[inherit] hover:bg-muted/10 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        >
          <span className="sr-only">Open {title.toLowerCase()}</span>
        </Button>
      </FramePanel>
    </Frame>
  )
}
