"use client"

import { useEffect, useRef, useState, type DragEvent } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  Cancel01Icon,
  CloudUploadIcon,
  FileZipIcon,
} from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { api, errorMessage } from "@/lib/api"
import { Input } from "@/components/ui/input"

function fileSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
}

/** The native file input supplies the browser picker; all visible actions use
 * shared buttons. The drop surface adds file handling without a second button. */
export function ArchiveUpload({
  id,
  file,
  onFileChange,
  maxBytes,
  disabled = false,
  validation,
  onValidationChange,
}: {
  validation?: { workspaceId: string; runtime: string; entryPoint: string }
  onValidationChange?: (valid: boolean) => void
  id: string
  file: File | null
  onFileChange: (file: File | null) => void
  maxBytes?: number
  disabled?: boolean
}) {
  const inputRef = useRef<HTMLInputElement>(null)
  const browseRef = useRef<HTMLButtonElement>(null)
  const dragDepth = useRef(0)
  const [dragging, setDragging] = useState(false)
  const [error, setError] = useState("")

  const [inspection, setInspection] = useState<{ state: "checking" | "valid" | "invalid"; message: string } | null>(null)
  const validationEnabled = Boolean(validation)
  const workspaceId = validation?.workspaceId
  const runtime = validation?.runtime
  const entryPoint = validation?.entryPoint
  useEffect(() => {
    if (!validationEnabled || !onValidationChange) return
    let active = true
    const controller = new AbortController()
    Promise.resolve().then(async () => {
      if (!active) return
      onValidationChange(false)
      setInspection(null)
      if (!file || !workspaceId || !runtime || !entryPoint) return
      setInspection({ state: "checking", message: "Checking ZIP contents and project structure…" })
      try {
        const body = new FormData()
        body.append("metadata", JSON.stringify({ workspaceId, config: { runtime, entryPoint } }))
        body.append("archive", file)
        const result = await api<{ message: string }>("/api/v1/publishing/validate-archive", { method: "POST", body, signal: controller.signal })
        if (active) { setInspection({ state: "valid", message: result.message }); onValidationChange(true) }
      } catch (cause) {
        if (active) setInspection({ state: "invalid", message: errorMessage(cause) })
      }
    })
    return () => { active = false; controller.abort() }
  }, [file, workspaceId, runtime, entryPoint, onValidationChange, validationEnabled])

  function choose(files: File[]) {
    if (disabled || !files.length) return
    if (files.length !== 1) {
      setError("Choose one ZIP archive at a time.")
      return
    }
    const next = files[0]
    if (!next.name.toLowerCase().endsWith(".zip")) {
      setError("Choose a ZIP archive containing your application.")
      return
    }
    if (maxBytes && next.size > maxBytes) {
      setError(`This archive exceeds the ${fileSize(maxBytes)} upload limit.`)
      return
    }
    if (next.size === 0) {
      setError(
        "This archive is empty. Choose a ZIP containing your application."
      )
      return
    }
    setError("")
    onFileChange(next)
  }
  function dragEnter(event: DragEvent<HTMLDivElement>) {
    event.preventDefault()
    if (disabled || !event.dataTransfer.types.includes("Files")) return
    dragDepth.current += 1
    setDragging(true)
  }
  function drop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault()
    dragDepth.current = 0
    setDragging(false)
    if (!disabled) choose(Array.from(event.dataTransfer.files))
  }

  return (
    <div className="space-y-2">
      <Input
        id={id}
        ref={inputRef}
        type="file"
        accept=".zip,application/zip,application/x-zip-compressed"
        className="hidden"
        tabIndex={-1}
        aria-hidden="true"
        disabled={disabled}
        onChange={(event) => {
          choose(Array.from(event.target.files ?? []))
          event.target.value = ""
        }}
      />
      <div
        role="group"
        aria-label="Application ZIP upload"
        aria-describedby={error ? `${id}-error` : undefined}
        aria-disabled={disabled || undefined}
        onDragEnter={dragEnter}
        onDragOver={(event) => {
          event.preventDefault()
          event.dataTransfer.dropEffect = disabled ? "none" : "copy"
        }}
        onDragLeave={(event) => {
          event.preventDefault()
          dragDepth.current = Math.max(0, dragDepth.current - 1)
          if (!dragDepth.current) setDragging(false)
        }}
        onDrop={drop}
        className={`flex min-w-0 flex-wrap items-center gap-x-4 gap-y-3 rounded-xl border border-dashed px-4 py-4 transition-colors duration-150 sm:flex-nowrap ${disabled ? "opacity-50" : ""} ${dragging ? "border-primary bg-primary/5 ring-2 ring-primary/15" : error ? "border-destructive bg-destructive/5" : "border-border bg-muted/25 dark:bg-background/40"}`}
      >
        <span
          aria-hidden="true"
          className={`flex size-10 shrink-0 items-center justify-center rounded-xl ${file ? "bg-muted text-muted-foreground" : "bg-primary/10 text-primary"}`}
        >
          <HugeiconsIcon
            icon={file ? FileZipIcon : CloudUploadIcon}
            className="size-5"
            strokeWidth={1.7}
          />
        </span>
        <span
          className="min-w-0 flex-1 basis-[calc(100%-3.5rem)] sm:basis-0"
          aria-live="polite"
          aria-atomic="true"
        >
          <span
            className="block truncate text-sm font-medium"
            title={file?.name}
          >
            {dragging
              ? "Drop your ZIP to select it"
              : file
                ? file.name
                : "Drop your application ZIP here"}
          </span>
          <span className="mt-1 block text-xs leading-5 text-muted-foreground">
            {file
              ? `${fileSize(file.size)} · Selected for the next build`
              : "or browse files on your computer"}
          </span>
        </span>
        <div className="flex w-full shrink-0 items-center gap-1.5 pl-14 sm:ml-auto sm:w-auto sm:pl-0">
          <Button
            ref={browseRef}
            type="button"
            variant="outline"
            disabled={disabled}
            onClick={() => inputRef.current?.click()}
            aria-controls={id}
            className="h-9 rounded-lg px-3.5"
          >
            {file ? "Replace file" : "Browse files"}
          </Button>
          {file && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              disabled={disabled}
              aria-label="Remove selected archive"
              onClick={() => {
                setError("")
                onFileChange(null)
                browseRef.current?.focus()
                if (inputRef.current) inputRef.current.value = ""
              }}
            >
              <HugeiconsIcon
                icon={Cancel01Icon}
                className="size-4"
                strokeWidth={1.8}
              />
            </Button>
          )}
        </div>
      </div>
      {file && inspection && <p role={inspection.state === "invalid" ? "alert" : "status"} className={`text-xs leading-5 ${inspection.state === "invalid" ? "text-destructive" : inspection.state === "valid" ? "text-success" : "text-muted-foreground"}`}>{inspection.message}</p>}
      {error && (
        <p
          id={`${id}-error`}
          role="alert"
          className="text-xs leading-5 text-destructive"
        >
          {error}
        </p>
      )}
    </div>
  )
}
