import { Skeleton } from "@/components/ui/skeleton"

export function ApplicationDetailSkeleton() {
  return <div role="status" aria-label="Loading application" className="space-y-6">
    <span className="sr-only">Loading application…</span>
    <div aria-hidden="true" className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-3"><Skeleton className="h-8 w-48 motion-reduce:animate-none" /><Skeleton className="h-7 w-20 rounded-full motion-reduce:animate-none" /></div>
          <Skeleton className="h-4 w-72 max-w-[70vw] motion-reduce:animate-none" />
        </div>
        <div className="flex items-center gap-2">
          <Skeleton className="h-9 w-28 motion-reduce:animate-none" />
          <Skeleton className="h-9 w-28 motion-reduce:animate-none" />
        </div>
      </div>
      <div className="flex gap-6 overflow-hidden border-b pb-3">
        {[64, 72, 84, 72, 112, 64].map((width, item) => <Skeleton key={item} className="h-4 shrink-0 motion-reduce:animate-none" style={{ width }} />)}
      </div>
      <Skeleton className="h-12 w-full rounded-xl motion-reduce:animate-none" />
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1.6fr)_minmax(280px,1fr)]">
        <div className="rounded-xl border bg-card">
          <div className="flex items-center justify-between gap-3 border-b px-5 py-4"><Skeleton className="h-4 w-32 motion-reduce:animate-none" /><Skeleton className="h-6 w-20 rounded-full motion-reduce:animate-none" /></div>
          <div className="space-y-4 p-5">
            <Skeleton className="h-8 w-44 motion-reduce:animate-none" />
            <div className="flex gap-2">{[0, 1, 2].map((item) => <Skeleton key={item} className="h-5 w-20 rounded-full motion-reduce:animate-none" />)}</div>
            <Skeleton className="h-9 w-36 motion-reduce:animate-none" />
          </div>
        </div>
        <div className="rounded-xl border bg-card">
          <div className="border-b px-5 py-4"><Skeleton className="h-4 w-24 motion-reduce:animate-none" /></div>
          <div className="space-y-3 p-5">{[0, 1, 2, 3].map((item) => <div key={item} className="flex justify-between gap-4"><Skeleton className="h-4 w-20 motion-reduce:animate-none" /><Skeleton className="h-4 w-32 motion-reduce:animate-none" /></div>)}</div>
        </div>
      </div>
      <div className="rounded-xl border bg-card">
        <div className="flex items-center justify-between gap-3 px-5 py-4"><Skeleton className="h-4 w-32 motion-reduce:animate-none" /><Skeleton className="h-8 w-20 motion-reduce:animate-none" /></div>
        <div className="space-y-3 border-t p-5">{[0, 1, 2].map((item) => <Skeleton key={item} className="h-5 w-full motion-reduce:animate-none" />)}</div>
      </div>
    </div>
  </div>
}
