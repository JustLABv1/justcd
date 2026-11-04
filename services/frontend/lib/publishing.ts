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

export type PublishingProfileSettings = {
  namespaceMode?: string
  namespacePrefix?: string
  templateId: string
  gitlabUrl: string
  group: string
  clusterId: string
  namespace: string
  webServerImage?: string
  baseImage: string
  builderImage: string
  registryPrefix: string
  hostSuffix: string
  pullSecret: string
  gitImage?: string
  buildEngine?: string
  caVariable: string
  runnerTag: string
  exposure?: string
  exposureLabels?: string
  exposureAnnotations?: string
  ingressClass?: string
  ingressTls?: string
  templateParameters?: string
  templateVersion?: number
  runnerServiceAccount?: string
  runnerScc?: string
  builderSetup?: string
  secretMode: string
  vaultAddress: string
  vaultRole: string
  vaultPath: string
  syncPolicy: string
}
export type PublishingProfile = {
  currentRevision: number
  id: string
  workspaceId: string
  name: string
  description: string
  enabled: boolean
  settings: PublishingProfileSettings
  version: number
  applications: number
}
export type ApprovedPublishingProfile = {
  currentRevision: number
  namespaceMode?: string
  namespacePrefix?: string
  id: string
  name: string
  description: string
  templateId: string
  clusterId: string
  clusterName: string
  namespace: string
  hostSuffix: string
  syncPolicy: string
}
export type PublicationResult = {
  applicationId: string
  profileId?: string
  projectUrl: string
  repositoryUrl: string
  pipelineUrl: string
  syncPolicy: string
  applicationUrl: string
}
