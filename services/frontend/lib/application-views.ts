import type { GroupMode } from "./application-grouping"
import type { FilterQuery } from "@/components/reui/filters/filters-types"

export type ApplicationViewConfig = {
  groupBy: GroupMode
  labelKey: string
  query: string
  status: string
  clusterId: string
  layout: "cards" | "list"
  sort: string
  favoritesOnly: boolean
  filters: FilterQuery
}
export type ApplicationSavedView = {
  id: string
  name: string
  configuration: ApplicationViewConfig
  isDefault: boolean
}
export type ApplicationPreferences = {
  views: ApplicationSavedView[]
  favoriteApplicationIds: string[]
}

export function applicationViewSignature(config: ApplicationViewConfig): string {
  return JSON.stringify({ ...config, labelKey: config.labelKey ?? "" }, (_, value) =>
    value && typeof value === "object" && !Array.isArray(value)
      ? Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b)))
      : value
  )
}
