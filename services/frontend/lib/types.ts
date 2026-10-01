export type User = {
  id: string
  email: string
  displayName: string
  isAdmin: boolean
  disabled: boolean
  deletedAt?: string
  createdAt: string
}

export type Workspace = {
  id: string
  name: string
  description: string
  role: "owner" | "deployer" | "viewer"
  approvalPolicy: ApprovalPolicy
  createdAt: string
}

export type ApprovalRule = {
  requiredApprovals: number
  approverRoles: string[]
  approverUserIds: string[]
}

export type ApprovalPolicy = {
  sync: ApprovalRule
  deletion: ApprovalRule
}

export type ApprovalPolicyOverride = {
  sync?: ApprovalRule
  deletion?: ApprovalRule
}

export type WorkspaceMember = {
  id: string
  email: string
  displayName: string
  role: string
  disabled: boolean
  managedBySSO: boolean
  editable: boolean
}

export type Credential = {
  id: string
  workspaceId?: string
  name: string
  kind: "git-ssh" | "git-https" | "kubernetes-token" | "kubeconfig"
  username?: string
  expiresAt?: string
  createdAt: string
}

export type Cluster = {
  id: string
  workspaceId?: string
  name: string
  apiServer: string
  insecureSkipVerify: boolean
  defaultCredentialId?: string
  clusterScopeCredentialId?: string
  maxConcurrentOperations: number
  operationsPerMinute: number
  shared?: boolean
  ownerWorkspaceName?: string
  createdAt: string
}

export type NamespaceBinding = { namespace: string; credentialId?: string }

export type OnboardingStep = {
  id: string
  title: string
  status: "complete" | "action_required" | "failed"
  category?: "configuration" | "permission" | "connectivity" | "service_health" | string
  summary: string
  remediation?: string
  href?: string
  docsHref?: string
}

export type OnboardingStatus = {
  steps: OnboardingStep[]
  completed: number
  total: number
  nextStepId?: string
  firstWorkspaceId?: string
}

export type KubernetesPermissionCheck = {
  id: string
  title: string
  scope: string
  namespace?: string
  apiGroup: string
  resource: string
  verb: string
  status: "passed" | "missing" | "unknown" | "failed"
  allowed: boolean
}

export type KubernetesPermissionFailure = {
  code: string
  category: string
  message: string
  remediation: string
  retryable: boolean
}

export type KubernetesPermissionScope = {
  status: string
  checks: KubernetesPermissionCheck[]
  failure?: KubernetesPermissionFailure
}

export type KubernetesPermissionReport = {
  status: string
  namespace: string
  serverVersion?: string
  canReadPods: boolean
  checkedAt: string
  checks: KubernetesPermissionCheck[]
  clusterScope?: KubernetesPermissionScope
  suggestions: {
    roleYaml?: string
    roleBindingYaml?: string
    clusterRoleYaml?: string
    clusterRoleBindingYaml?: string
  }
  failure?: KubernetesPermissionFailure
}

export type KubernetesPermissionTest = {
  workspaceId: string
  clusterId: string
  namespace: string
  report: KubernetesPermissionReport
  checkedAt: string
}

export type GitSource = {
  id: string
  workspaceId: string
  name: string
  repositoryUrl: string
  credentialId?: string
  shared?: boolean
  ownerWorkspaceName?: string
  createdAt: string
}

export type WorkspaceConnectionShare = {
  id: string
  kind: "cluster" | "git-source"
  resourceId: string
  resourceName: string
  repositoryUrl?: string
  ownerWorkspaceId: string
  ownerWorkspaceName: string
  targetWorkspaceId: string
  targetWorkspaceName: string
  credentialId?: string
  status: "pending" | "accepted" | "declined" | "revoked"
  createdAt: string
  acceptedAt?: string
}

export type RepositoryConfiguration = {
  id: string
  workspaceId: string
  sourceId: string
  revision: string
  enabled: boolean
  lastCheckedAt?: string
  lastCommit: string
  lastError: string
  createdAt: string
}

export type Application = {
  createNamespaces: boolean
  helmReleaseName?: string
  repositoryConfigurationId?: string
  configurationPath?: string
  configurationCommit?: string
  configurationMissing?: boolean
  id: string
  workspaceId: string
  name: string
  sourceId: string
  revision: string
  manifestPath: string
  renderer: "yaml" | "kustomize" | "helm"
  kustomizeHelmEnabled: boolean
  kustomizeNamespaceOverride: boolean
  helmValuesFiles: string[]
  helmValuesYaml: string
  applicationGroupId?: string
  targetManifestPath: string
  namespaceManifestPaths: Record<string, string>
  targetHelmValuesFiles: string[]
  targetHelmValuesYaml: string
  namespaceHelmValues: Record<string, { files: string[]; yaml: string }>
  clusterId: string
  namespaces: NamespaceBinding[]
  syncPolicy: "manual" | "auto-safe"
  pollSeconds: number
  retryPolicy: RetryPolicy
  retryAttemptCount: number
  retryNextAt?: string
  retryTerminalReason?: string
  retryLastErrorCode?: string
  lastCheckedAt?: string
  lastSyncedRevision?: string
  health: string
  healthCondition?: ApplicationHealthCondition
  statusIssues: { source: string; summary: string; observedAt: string }[]
  decommissioning: boolean
  autoSyncPaused?: boolean
  rollbackResumeAvailable?: boolean
  rollbackResumeRequiresRevision?: boolean
  approvalPolicyOverride?: ApprovalPolicyOverride
  createdAt: string
}

export type ApplicationHealthStatus = "Healthy" | "Progressing" | "Degraded" | "Suspended" | "Missing" | "Unknown" | "Partial"
export type KubernetesCondition = { type: string; status: string; reason?: string; message?: string; lastTransitionTime?: string }
export type ResourceHealthSummary = {
  desiredReplicas?: number
  readyReplicas?: number
  updatedReplicas?: number
  availableReplicas?: number
  desiredScheduled?: number
  numberScheduled?: number
  updatedScheduled?: number
  numberReady?: number
  numberAvailable?: number
  numberMisscheduled?: number
  completions?: number
  active?: number
  succeeded?: number
  failed?: number
  suspended?: boolean
  failureReason?: string
  failureMessage?: string
  conditions?: KubernetesCondition[]
}
export type ResourceHealthAssessment = {
  identity: Identity
  status: ApplicationHealthStatus
  reason: string
  message: string
  phase?: string
  readiness?: string
}
export type ApplicationHealthCondition = {
  status: ApplicationHealthStatus
  reason: string
  message: string
  lastTransitionTime: string
  observedAt?: string
  resources: ResourceHealthAssessment[]
  warnings: string[]
}
export type ApplicationHealthTransition = {
  id: number
  status: ApplicationHealthStatus
  reason: string
  message: string
  resources: ResourceHealthAssessment[]
  changedAt: string
}

export type ApplicationGroup = {
  id: string
  workspaceId: string
  name: string
  sourceId: string
  revision: string
  manifestPath: string
  renderer: "helm" | "kustomize"
  kustomizeHelmEnabled: boolean
  kustomizeNamespaceOverride: boolean
  helmValuesFiles: string[]
  helmValuesYaml: string
  syncPolicy: "manual" | "auto-safe"
  pollSeconds: number
  createdAt: string
  updatedAt: string
}

export type ApplicationGroupResponse = {
  group: ApplicationGroup
  applications: Application[]
}

export type Identity = {
  clusterId?: string
  apiVersion: string
  kind: string
  namespace: string
  name: string
  clusterScoped?: boolean
}

export type OwnershipConflict = {
  identity: Identity
  uid: string
  resourceVersion: string
  owner?: string
  ownerMissing?: boolean
  desiredFingerprint: string
  hasOwnerReferences: boolean
}

export type Change = {
  kind: "create" | "update" | "delete"
  takeover?: boolean
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
  managedByGit?: boolean
  id: string
  applicationId: string
  identity: Identity
  path?: string
  reason: string
  createdBy: string
  createdAt: string
}

export type IgnoreSelector = {
  managedByGit?: boolean
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
  approvalKind?: "sync" | "deletion" | string
  requiredApprovals?: number
  approverRoles?: string[]
  approverUserIds?: string[]
  rollback?: {
    kind: "successful_sync" | "pre_operation" | "git_revision"
    id?: string
    operationId?: string
    revision?: string
    createdAt?: string
    resourceCount?: number
    settings: { sourceId: string; revision: string; manifestPath: string; targetManifestPath?: string; namespaceManifestPaths?: Record<string, string>; renderer: string; kustomizeHelmEnabled: boolean; kustomizeNamespaceOverride: boolean; clusterId: string; namespaces: string[] }
  }
  digest: string
}

export type PlanApproval = {
  id: string
  actorId: string
  displayName: string
  email: string
  comment?: string
  role?: string
  eligible: boolean
  expiresAt: string
}

export type PlanApprovalSummary = {
  planId: string
  planDigest: string
  approvalKind: string
  requiredApprovals: number
  approvedApprovals: number
  approverRoles: string[]
  approverUserIds: string[]
  currentUserApproved: boolean
  canApprove: boolean
  approvals: PlanApproval[]
}

export type PlanRecord = {
  id: string
  plan: Plan
  trigger?: { sourceKey: string; provider: string; deliveryId: string; ref: string; reportedCommit: string }
  resources?: Identity[]
  createdBy: string
  createdAt: string
  expiresAt: string
  status: string
}

export type ManagedResource = { identity: Identity; uid: string; resourceVersion: string; adopted?: boolean }

export type TopologyNode = {
  id: string
  identity: Identity
  source: "desired" | "managed" | "kubernetes" | "sample"
  uid?: string
  resourceVersion?: string
  phase?: string
  readiness?: string
  healthSummary?: ResourceHealthSummary
  observedAt?: string
}

export type TopologyEdge = { from: string; to: string; relation: string }
export type ResourceTopology = { planId?: string; nodes: TopologyNode[]; edges: TopologyEdge[]; warnings: string[] }

export type Operation = {
  id: string
  applicationId: string
  planId?: string
  actorId?: string
  type?: "sync" | "rollback" | "resource_action"
  rollbackCheckpointId?: string
  status: string
  message: string
  progress?: { phase?: string; total: number; completed: Identity[]; current?: Identity }
  attemptCount: number
  errorCode?: string
  nextRetryAt?: string
  terminalReason?: string
  startedAt: string
  finishedAt?: string
}

export type RetryPolicy = {
  enabled: boolean
  maxAttempts: number
  initialDelaySeconds: number
  maxDelaySeconds: number
  jitterPercent: number
}

export type RollbackTarget = {
  id: string
  kind: "successful_sync" | "pre_operation"
  operationId?: string
  revision?: string
  resourceCount: number
  createdAt: string
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
