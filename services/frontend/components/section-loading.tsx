import { Skeleton } from "@/components/ui/skeleton"

/** Skeleton lines for a section whose data is still loading. `lines` is 2 or 3. */
export function SectionLoading({ label, lines = 3, className = "space-y-4 p-5 sm:p-6" }: { label: string; lines?: 2 | 3; className?: string }) {
  return <div role="status" aria-label={label} className={className}>
    <span className="sr-only">{label}…</span>
    <div aria-hidden="true" className="space-y-3">
      <Skeleton className="h-5 w-2/3 max-w-72 motion-reduce:animate-none" />
      <Skeleton className="h-4 w-full max-w-lg motion-reduce:animate-none" />
      {lines === 3 && <Skeleton className="h-4 w-4/5 max-w-md motion-reduce:animate-none" />}
    </div>
  </div>
}
