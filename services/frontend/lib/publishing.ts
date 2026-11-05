import { api } from "@/lib/api"

export type PublishingLimits = { archiveBytes: number; expandedBytes: number }
export const publishingSize = (bytes: number) => `${bytes / (1024 * 1024)} MiB`

export async function publishProject<T>(
  metadata: unknown,
  archive: File,
  limits: PublishingLimits,
  onUploadProgress?: (percent: number) => void
): Promise<T> {
  if (archive.size > limits.archiveBytes)
    throw new Error(
      `The ZIP archive exceeds ${publishingSize(limits.archiveBytes)}.`
    )
  const body = new FormData()
  // Authorization metadata precedes the binary upload on the backend stream.
  body.append("metadata", JSON.stringify(metadata))
  body.append("archive", archive)
  return api<T>(
    "/api/v1/publishing",
    { method: "POST", body },
    onUploadProgress
  )
}

export type PublishingProfileSettings = {
  serviceCatalog?: string
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
  services?: ServiceOptions
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

export type DatabasePlan = {
  credentialMode?: "platform" | "generated"
  username?: string
  allowConfigure?: boolean
  allowEnvMapping?: boolean
  allowCustomPassword?: boolean
  envNames?: Record<string, string>
  name: string
  postgresVersion: string
  instances: number
  instanceProfile?: string
  storage: string
  storageClass?: string
  backupStorage?: string
  databaseName: string
  credentialsSecret: string
  usernameKey: string
  passwordKey: string
  bootstrapSecret: string
  bootstrapKey: string
  requiredVaultSecret?: string
}
export type VaultSecretBinding = {
  allowPathSuffix?: boolean
  allowEnvMapping?: boolean
  id: string
  name: string
  authRef: string
  vaultNamespace?: string
  mount: string
  path: string
  type: string
  refreshAfter: string
  destination: string
  envs: { name: string; key: string }[]
}
export type ServiceCatalog = {
  allowEnvironment?: boolean
  database?: DatabasePlan
  vaultSecrets?: VaultSecretBinding[]
}
export type ServiceSelection = {
  database: boolean
  vaultSecrets: string[]
  databaseConfig?: {
    name?: string
    username?: string
    envNames?: Record<string, string>
  }
  vaultConfig?: Record<
    string,
    { pathSuffix?: string; envs?: { name: string; key: string }[] }
  >
  environment?: { name: string; value: string }[]
}
export type ServiceOptions = {
  allowEnvironment?: boolean
  database?: Partial<DatabasePlan>
  databaseVaultSecret?: string
  databaseName?: string
  vaultSecrets: {
    id: string
    name: string
    path?: string
    allowPathSuffix?: boolean
    allowEnvMapping?: boolean
    envs?: { name: string; key: string }[]
  }[]
}

/** The workspace the user selected in the app shell, if any. */
export function selectedWorkspaceId(): string {
  try {
    return window.localStorage.getItem("justcd.selectedWorkspaceId") ?? ""
  } catch {
    return ""
  }
}

/** Template list URL; with a workspace it also returns that workspace's own templates. */
export function templatesUrl(workspaceId: string = selectedWorkspaceId()) {
  return workspaceId
    ? `/api/v1/publishing/templates?workspaceId=${encodeURIComponent(workspaceId)}`
    : "/api/v1/publishing/templates"
}
