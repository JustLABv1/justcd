"use client"

import { FormSelect } from "@/components/ui/form-select"
import { FormField, SwitchField } from "@/components/ui-kit"

export type PRCommentMode = "summary" | "action-required" | "off"

export function PRCommentSettings({
  id,
  mode,
  onModeChange,
  onDraftsChange,
  commentOnDrafts,
  disabled = false,
}: {
  id: string
  mode: PRCommentMode
  onModeChange: (mode: PRCommentMode) => void
  commentOnDrafts: boolean
  onDraftsChange: (value: boolean) => void
  disabled?: boolean
}) {
  return (
    <div className="space-y-4">
      <FormField
        label="PR comments"
        htmlFor={`${id}-mode`}
        hint="JustCD updates one summary per pull request, shared by its applications. Existing summaries also show when an issue is resolved."
      >
        <FormSelect
          id={`${id}-mode`}
          value={mode}
          disabled={disabled}
          onValueChange={(value) => onModeChange(value as PRCommentMode)}
          items={[
            { value: "summary", label: "Updated summary" },
            { value: "action-required", label: "Only when action is required" },
            { value: "off", label: "Off" },
          ]}
        />
      </FormField>
      <SwitchField
        id={`${id}-drafts`}
        label="Comment on draft pull requests"
        description="Off by default. Comments wait until the PR is ready for review. Preview deployments follow their configured policy."
        checked={commentOnDrafts}
        onCheckedChange={onDraftsChange}
        disabled={disabled || mode === "off"}
      />
    </div>
  )
}
