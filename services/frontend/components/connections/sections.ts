import {
  GitBranchIcon,
  Key01Icon,
  ServerStack01Icon,
  Share08Icon,
} from "@hugeicons/core-free-icons"

/** Workspace-level connection sections (route ids under /workspaces/[id]/connections/[id]). */
export const workspaceConnectionSections = [
  { id: "git-sources", title: "Git sources", description: "Repositories tracked by this workspace", icon: GitBranchIcon },
  { id: "clusters", title: "Kubernetes", description: "Clusters and the namespaces this workspace can deploy to", icon: ServerStack01Icon },
  { id: "credentials", title: "Credentials", description: "Encrypted Git and Kubernetes secrets", icon: Key01Icon },
  { id: "shares", title: "Sharing", description: "Offers, accepted shares, and access status", icon: Share08Icon },
] as const
