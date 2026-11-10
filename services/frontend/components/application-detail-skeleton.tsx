import { Card } from "@/components/panelui/card"
import { Skeleton } from "@/components/ui/skeleton"

/** Mirrors the compact overview so loading does not introduce a different page hierarchy. */
export function ApplicationDetailSkeleton() {
  return <div role="status" aria-label="Loading application" className="space-y-5">
    <span className="sr-only">Loading application…</span>
    <div aria-hidden="true" className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="space-y-3"><Skeleton className="h-8 w-48 motion-reduce:animate-none" /><Skeleton className="h-4 w-72 max-w-[70vw] motion-reduce:animate-none" /><Skeleton className="h-5 w-36 motion-reduce:animate-none" /></div>
        <div className="flex flex-wrap items-center gap-2"><Skeleton className="h-9 w-28 motion-reduce:animate-none" /><Skeleton className="h-9 w-28 motion-reduce:animate-none" /></div>
      </div>
      <Skeleton className="h-10 w-full max-w-xl rounded-2xl motion-reduce:animate-none" />
      <Card size="sm"><Card.Content className="flex flex-wrap items-center justify-between gap-4"><div className="space-y-2"><Skeleton className="h-3 w-28 motion-reduce:animate-none" /><Skeleton className="h-5 w-36 motion-reduce:animate-none" /><Skeleton className="h-4 w-40 motion-reduce:animate-none" /></div><Skeleton className="h-9 w-28 motion-reduce:animate-none" /></Card.Content></Card>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">{[0, 1, 2].map(item => <Card key={item} size="sm"><Card.Content className="space-y-3"><Skeleton className="h-4 w-24 motion-reduce:animate-none" /><Skeleton className="h-7 w-28 motion-reduce:animate-none" /><Skeleton className="h-4 w-32 motion-reduce:animate-none" /></Card.Content></Card>)}</div>
      <div className="grid grid-cols-1 items-start gap-5 lg:grid-cols-[minmax(0,1.6fr)_minmax(280px,1fr)]">
        <Card><Card.Header className="flex flex-wrap items-center justify-between gap-3"><Skeleton className="h-5 w-32 motion-reduce:animate-none" /><Skeleton className="h-8 w-36 motion-reduce:animate-none" /></Card.Header><Card.Content><Skeleton className="h-56 w-full motion-reduce:animate-none" /></Card.Content></Card>
        <Card size="sm"><Card.Header className="flex items-center justify-between gap-3"><Skeleton className="h-4 w-20 motion-reduce:animate-none" /><Skeleton className="h-8 w-24 motion-reduce:animate-none" /></Card.Header><Card.Content className="space-y-3">{[0, 1].map(item => <div key={item} className="flex justify-between gap-4"><Skeleton className="h-4 w-16 motion-reduce:animate-none" /><Skeleton className="h-4 w-24 motion-reduce:animate-none" /></div>)}</Card.Content></Card>
      </div>
    </div>
  </div>
}
