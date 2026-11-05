"use client"

import { reviewTextRows } from "@/lib/line-diff"
import { Disclosure } from "@/components/ui/collapsible"
import { PublishingCard } from "@/components/applications/publishing-fields"

export type RecipeSettingChange = {
  name: string
  before: string
  after: string
}
export type RecipeFileChange = {
  path: string
  before: string
  after: string
  exists: boolean
}

/**
 * The review shared by every build-file update: the changed settings and a
 * line diff of each generated file, so nothing is committed unseen.
 */
export function RecipeReview({
  images,
  changes,
  emptyText,
}: {
  images: RecipeSettingChange[]
  changes: RecipeFileChange[]
  emptyText: string
}) {
  return (
    <>
      <PublishingCard contentClassName="space-y-3">
        <h3 className="text-sm font-medium">Build settings</h3>
        <dl className="space-y-3">
          {images.map((image) => (
            <div key={image.name}>
              <dt className="text-sm font-medium">{image.name}</dt>
              <dd className="mt-1 space-y-1 text-xs break-all">
                <p className="text-muted-foreground">
                  From: {image.before || "Not configured"}
                </p>
                <p>To: {image.after || "Not configured"}</p>
              </dd>
            </div>
          ))}
        </dl>
        {!images.length && (
          <p className="text-sm text-muted-foreground">{emptyText}</p>
        )}
      </PublishingCard>
      {changes.length > 0 && (
        <h3 className="text-sm font-medium">Generated files</h3>
      )}
      {changes.map((file) => (
        <PublishingCard key={file.path}>
          <Disclosure
            summary={`${file.exists ? "Update" : "Add"} ${file.path}`}
          >
            <pre
              className="mt-3 max-h-96 overflow-auto rounded-lg border bg-background p-3 text-xs"
              aria-label={`Changes to ${file.path}`}
            >
              <code>
                {reviewTextRows(
                  file.exists ? file.before : undefined,
                  file.after
                ).map((row, i) => (
                  <span key={i}>
                    {row.changed ? (
                      <>
                        {row.before !== undefined && (
                          <span className="block bg-destructive/10 text-destructive">
                            − {row.before}
                            {"\n"}
                          </span>
                        )}
                        {row.after !== undefined && (
                          <span className="block bg-success/10 text-success">
                            + {row.after}
                            {"\n"}
                          </span>
                        )}
                      </>
                    ) : (
                      <span className="block">
                        {" "}
                        {row.after}
                        {"\n"}
                      </span>
                    )}
                  </span>
                ))}
              </code>
            </pre>
          </Disclosure>
        </PublishingCard>
      ))}
    </>
  )
}
