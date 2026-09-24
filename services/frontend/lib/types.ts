export type User = {
  id: string
  email: string
  displayName: string
  isAdmin: boolean
  createdAt: string
}

export type Project = {
  id: string
  name: string
  description: string
  role: "owner" | "deployer" | "viewer"
  createdAt: string
}

export type Credential = {
  id: string
  projectId?: string
  name: string
  kind: "git-ssh" | "git-https" | "kubernetes-token" | "kubeconfig"
  username?: string
  expiresAt?: string
  createdAt: string
}

export type Cluster = {
  id: string
  name: string
  apiServer: string
  insecureSkipVerify: boolean
  defaultCredentialId?: string
  clusterScopeCredentialId?: string
  createdAt: string
}

export type NamespaceBinding = { namespace: string; credentialId?: string }

export type GitSource = {
  id: string
  projectId: string
  name: string
  repositoryUrl: string
  credentialId?: string
  createdAt: string
}

export type Application = {
  id: string
  projectId: string
  name: string
  sourceId: string
  revision: string
  manifestPath: string
  renderer: "yaml" | "kustomize" | "helm"
  kustomizeHelmEnabled: boolean
  kustomizeNamespaceOverride: boolean
  clusterId: string
  namespaces: NamespaceBinding[]
  syncPolicy: "manual" | "auto-safe"
  pollSeconds: number
  lastCheckedAt?: string
  lastSyncedRevision?: string
  health: string
  decommissioning: boolean
  createdAt: string
}

export type Identity = {
  clusterId?: string
  apiVersion: string
  kind: string
  namespace: string
  name: string
  clusterScoped?: boolean
}

export type Change = {
  kind: "create" | "update" | "delete"
  identity: Identity
  liveUid?: string
  liveResourceVersion?: string
  desiredFingerprint?: string
  liveFingerprint?: string
  before?: unknown
  after?: unknown
  changedPaths?: string[]
  ignoredPaths?: string[]
  ignoreReason?: string
}

export type FieldExclusion = { identity: Identity; path: string }
export type PlanSelection = { resources?: Identity[]; fields?: FieldExclusion[] }
export type IgnoreRule = {
  id: string
  applicationId: string
  identity: Identity
  path?: string
  reason: string
  createdBy: string
  createdAt: string
}

export type IgnoreSelector = {
  id: string
  apiVersion?: string
  kind?: string
  labelKey?: string
  labelValue?: string
  reason: string
  createdBy?: string
  createdAt?: string
}

export type Plan = {
  applicationId: string
  decommission?: boolean
  revision: string
  bindings: { clusterId: string; namespace: string; credentialRef: string; clusterScope?: boolean }[]
  changes: Change[]
  ignored?: Change[]
  selection?: PlanSelection
  ignoreRulesDigest?: string
  requiresApproval: boolean
  digest: string
}

export type PlanRecord = {
  id: string
  plan: Plan
  resources?: Identity[]
  createdBy: string
  createdAt: string
  expiresAt: string
  status: string
}

export type ManagedResource = { identity: Identity; uid: string; resourceVersion: string }

export type TopologyNode = {
  id: string
  identity: Identity
  source: "desired" | "managed" | "kubernetes" | "sample"
  uid?: string
  resourceVersion?: string
  phase?: string
  readiness?: string
  observedAt?: string
}

export type TopologyEdge = { from: string; to: string; relation: string }
export type ResourceTopology = { planId?: string; nodes: TopologyNode[]; edges: TopologyEdge[]; warnings: string[] }

export type Operation = {
  id: string
  applicationId: string
  planId?: string
  actorId?: string
  status: string
  message: string
  progress?: { phase?: string; total: number; completed: Identity[]; current?: Identity }
  startedAt: string
  finishedAt?: string
}

export type OIDCProvider = {
  id: string
  name: string
  issuer?: string
  clientId?: string
  redirectUrl?: string
  groupsClaim?: string
  enabled?: boolean
}

export type ListResponse<T> = { items: T[] }
