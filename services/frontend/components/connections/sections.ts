import {
  GitBranchIcon,
  Key01Icon,
  ServerStack01Icon,
  Share08Icon,
} from "@hugeicons/core-free-icons"

/** Workspace-level connection sections (route ids under /workspaces/[id]/connections/[id]). */
export const workspaceConnectionSections = [
  { id: "git-sources", title: "Git sources", description: "Connect repositories and choose branches for automatic application discovery.", icon: GitBranchIcon },
  { id: "clusters", title: "Kubernetes", description: "Connect clusters, then grant this workspace access to specific namespaces.", icon: ServerStack01Icon },
  { id: "credentials", title: "Credentials", description: "Manage the Git and Kubernetes credentials available to this workspace.", icon: Key01Icon },
  { id: "shares", title: "Sharing", description: "Offer connections to other workspaces or accept access offered to this one.", icon: Share08Icon },
] as const
