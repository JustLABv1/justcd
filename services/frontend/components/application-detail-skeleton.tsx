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
      <div className="grid gap-3 rounded-xl border bg-card p-4 sm:grid-cols-3 sm:divide-x sm:p-5">
        {[0, 1, 2].map((item) => <div key={item} className="space-y-3 sm:px-3 first:sm:pl-0 last:sm:pr-0">
          <Skeleton className="h-3 w-20 motion-reduce:animate-none" />
          <Skeleton className="h-4 w-32 max-w-full motion-reduce:animate-none" />
        </div>)}
      </div>
      <div className="flex gap-6 overflow-hidden border-b pb-3">
        {[64, 72, 84, 136, 112].map((width, item) => <Skeleton key={item} className="h-4 shrink-0 motion-reduce:animate-none" style={{ width }} />)}
      </div>
      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.5fr)_minmax(280px,1fr)]">
        <div className="rounded-xl border bg-card">
          <div className="space-y-2 border-b p-5"><Skeleton className="h-4 w-32 motion-reduce:animate-none" /><Skeleton className="h-3 w-64 max-w-full motion-reduce:animate-none" /></div>
          <div className="space-y-5 p-5">
            <div className="grid grid-cols-3 gap-2">{[0, 1, 2].map((item) => <Skeleton key={item} className="h-14 motion-reduce:animate-none" />)}</div>
            <Skeleton className="h-3 w-52 max-w-full motion-reduce:animate-none" />
            <Skeleton className="h-9 w-28 motion-reduce:animate-none" />
          </div>
        </div>
        <div className="rounded-xl border bg-card">
          <div className="space-y-2 border-b p-5"><Skeleton className="h-4 w-36 motion-reduce:animate-none" /><Skeleton className="h-3 w-52 max-w-full motion-reduce:animate-none" /></div>
          <div className="grid grid-cols-3 gap-3 p-5">{[0, 1, 2, 3, 4, 5].map((item) => <Skeleton key={item} className="h-8 motion-reduce:animate-none" />)}</div>
        </div>
      </div>
    </div>
  </div>
}
