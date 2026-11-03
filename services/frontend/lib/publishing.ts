import { api } from "@/lib/api"

export type PublishingLimits = { archiveBytes: number; expandedBytes: number }
export const publishingSize = (bytes: number) => `${bytes / (1024 * 1024)} MiB`

export async function publishProject<T>(
  metadata: unknown,
  archive: File,
  limits: PublishingLimits
): Promise<T> {
  if (archive.size > limits.archiveBytes)
    throw new Error(
      `The ZIP archive exceeds ${publishingSize(limits.archiveBytes)}.`
    )
  const body = new FormData()
  // Authorization metadata precedes the binary upload on the backend stream.
  body.append("metadata", JSON.stringify(metadata))
  body.append("archive", archive)
  return api<T>("/api/v1/publishing", { method: "POST", body })
}
