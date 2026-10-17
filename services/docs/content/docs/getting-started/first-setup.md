---
title: "First setup"
---

After creating the first administrator, JustCD opens `/onboarding`. This
readiness checklist is derived from current persisted configuration and saved
connection tests, so it resumes across sessions and completed steps can always
be revisited. It is guidance rather than a gate: experienced administrators
can continue to use normal workspace and settings navigation.

The checklist verifies database availability and public URL configuration,
records an administrator's confirmation that the stable encryption key is
backed up, and guides the Git, Kubernetes, and first-application workflow.
Kubernetes readiness uses discovery and `SelfSubjectAccessReview` calls only;
it does not mutate workloads. Failures identify configuration, permission,
connectivity, or service-health problems and include remediation. Credential
values remain encrypted in the backend and are never returned to the browser.
The same guided cluster workflow is available later from every workspace's
Connections tab, where administrators can reuse an existing credential or add
one inline before registering and verifying another cluster.

1. Create the first administrator at `/signup`, then create a workspace and add a local
   user or configure an OIDC provider.
2. Add a global Kubernetes token or static kubeconfig credential, then register
   the Kubernetes API endpoint and CA. Leave cluster-scope access disabled
   unless the application genuinely manages cluster-wide resources.
3. Bind one or more namespaces to the workspace. Each binding may use its own
   credential or inherit the cluster default.
4. Add an HTTPS/SSH Git source and a credential when needed, then create an
   application with a tracked branch, manifest path, renderer, and namespace
   bindings.
   Alternatively, enable **Manage applications from Git** on the workspace's Git
   sources page and place `justcd.yaml` beside each overlay or chart. See
   [Applications managed by Git](/guides/repository-applications) for the file
   format, discovery, and removal behavior.
5. Build a plan and review its resource-level diff before syncing. Plans expire
   after 15 minutes. Deletes and cluster-scoped changes require a workspace
   owner’s one-time approval. The executor rechecks the plan, resource UID,
   resource version, and ownership immediately before mutation.

For OIDC, use the `redirectUrl` returned by provider creation as the exact
callback URL in the identity provider. The public URL must point to the Next.js
application (whose same-origin API proxy forwards callbacks to the backend).
Group-to-workspace role mappings are configured in Connections & access.
