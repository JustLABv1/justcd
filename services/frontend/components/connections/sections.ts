import {
  Folder01Icon,
  GitBranchIcon,
  Key01Icon,
  ServerStack01Icon,
  Share08Icon,
} from "@hugeicons/core-free-icons"

/** Workspace-level connection sections (route ids under /workspaces/[id]/connections/[id]). */
export const workspaceConnectionSections = [
  { id: "git-sources", title: "Git sources", description: "Repositories tracked by this workspace", icon: GitBranchIcon },
  { id: "clusters", title: "Kubernetes clusters", description: "Direct API connections and outbound cluster agents", icon: ServerStack01Icon },
  { id: "namespaces", title: "Namespace access", description: "Target namespaces and per-namespace access", icon: Folder01Icon },
  { id: "credentials", title: "Credentials", description: "Encrypted Git and Kubernetes secrets", icon: Key01Icon },
  { id: "shares", title: "Shared connections", description: "Offers, accepted shares, and access status", icon: Share08Icon },
] as const
