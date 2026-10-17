---
title: "JustCD documentation"
description: "Deploy from Git with a reviewed plan, scoped Kubernetes access, and an audit trail."
---

JustCD reads Kubernetes manifests from Git and compares them with the live cluster.
You review the plan before applying changes. YAML, Kustomize, and Helm are supported;
CI continues to build and test your images in your existing provider.

## Start here

- [Install with Docker Compose](/getting-started/docker-compose) or [Helm](/getting-started/helm).
- [Complete first setup](/getting-started/first-setup): administrator, workspace, Git, cluster, namespace, and application.
- [Learn the delivery workflow](/guides/workflow) and [application controls](/guides/controls).
- [Configure applications from Git](/guides/repository-applications), [pull requests](/guides/pull-requests), and [branch tests](/guides/test-branch).
- [Operate production](/operations/production-operations), [secure access](/operations/security-hardening), and [monitor health](/guides/application-health).

## How the pieces fit

The **frontend** handles browser access and proxies the backend API. The **backend**
fetches Git, renders manifests, plans and executes operations, and encrypts credentials
in **PostgreSQL**. It contacts Kubernetes directly or uses an outbound **cluster agent**
for private clusters. This **docs service** runs independently and has no access to those
credentials or your database. See [architecture](/reference/architecture).

Workspace roles and namespace bindings control access. A plan records its exact commit,
resource changes, digest, and expiry. Deletes and cluster-scoped changes require owner
approval; `auto-safe` only applies eligible namespaced creates and updates and never
automatically prunes.

## For contributors

[Run the services locally](/getting-started/local-development) or
[develop and host these docs](/getting-started/docs-service). Historical implementation
assessments are kept in the History section; current guides describe the supported workflows.
