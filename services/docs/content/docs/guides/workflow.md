---
title: "Delivery workflow"
description: "From a workspace and Git source to a reviewed deployment."
---

## 1. Choose a workspace and connect your sources

Create or select a workspace. Owners manage workspace configuration and access;
deployers work with deployment plans and operations, and viewers inspect state.
Use the workspace connections to add Git sources and Kubernetes clusters, verify
connectivity, and bind the namespaces the workspace may deploy into. A namespace
binding selects a credential or inherits the workspace cluster default.

Use [outbound agents](/operations/cluster-agents) for private Kubernetes APIs.
JustCD login roles and Kubernetes RBAC are separate: a user role cannot grant
permissions that the deployment credential does not have.

## 2. Define desired state

Create an application with a Git source, tracked revision, manifest path, renderer,
and target namespace bindings. Or enable **Manage applications from Git** and commit
[justcd.yaml](/guides/repository-applications) beside each overlay or chart.
Application groups share source/render settings across child applications; each
child has its own target, plan, approvals, and health.

## 3. Build and review a plan

Build a plan from the application detail page. Verify the source commit and destination,
then inspect creates, updates, deletes, resource diffs, policy findings, and ownership
conflicts. Planning does not deploy workloads. Kubernetes dry runs predict admission
changes; incomplete discovery or missing permissions stops safe planning.

Plans expire after 15 minutes. Changing Git, configuration, policy, or live resources
can invalidate a plan; rebuild and review the new plan instead of reusing an old approval.
The [plan review guide](/guides/plan-review) explains filters and high-risk evidence.

## 4. Approve and deploy

Deletes and cluster-scoped changes need a workspace owner's one-time approval for
the exact plan. Other approval rules can raise the required threshold. Apply a manual
application's reviewed plan after the required approvals are present. The executor
rechecks the plan and resource identity before mutation.

With `auto-safe`, background checks may apply eligible namespaced creates and updates.
Changes that need approval wait. For an active auto-sync application, the final valid
approval can queue the reviewed plan. Manual applications, rollback, and application
deletion still require Apply. Review [controls and guardrails](/guides/controls) before
enabling automatic delivery.

## 5. Observe and recover

Use the application overview for drift and [runtime health](/guides/application-health),
the Topology tab for resource relationships, and operation history for per-resource
progress and errors. Audit records identify who changed what and which commit and plan
were used. A failed operation may have applied some steps; inspect the live state and
build a fresh plan before retrying.

Pause reconciliation before a manual intervention. Resource actions pause the application
and leave it paused; resuming can replace a hotfix with Git's desired configuration.
For interrupted operations, see [progress and recovery](/operations/operation-progress).
