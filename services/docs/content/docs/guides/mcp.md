---
title: MCP access
description: Connect an assistant to workspace-scoped application status and deployment plans.
---

JustCD serves MCP over Streamable HTTP at `/api/v1/mcp`, using the official Go MCP SDK. Connect through your usual JustCD HTTPS hostname. The frontend API proxy supports this endpoint. No separate process is required.

Each personal credential is restricted to one workspace and offers 7, 30, or 90 days, 1 year (365 days), and no expiry. The default is 30 days when allowed by the instance policy; otherwise the creation dialog selects the first allowed lifetime. Only a hash is stored. Access always remains limited by the account's current workspace role; disabling the account or removing workspace access stops access. Credentials cannot be used with the general REST API.

## Create a credential

Open **MCP access** in your account menu (`/settings/mcp`) to copy the server URL, create a workspace-scoped API key, and revoke existing keys. The new key is displayed only once. Read access is selected by default; deployers and owners can optionally enable plan generation.

For programmatic credential management, use an authenticated browser session to call `POST /api/v1/mcp-credentials`, with the session cookie, `Content-Type: application/json`, and the matching `X-CSRF-Token` header used by other JustCD mutations:

```json
{
  "name": "My assistant",
  "workspaceId": "your-workspace-id",
  "allowPlan": false,
  "expiresInDays": 30
}
```

The response contains `credential` metadata and a `token`. Store the token securely: it is returned only once. Do not put it in Git or shared configuration. A viewer can create a read credential; enabling `allowPlan` requires deployer or owner access.

List your credential metadata with `GET /api/v1/mcp-credentials`. Revoke one with `DELETE /api/v1/mcp-credentials/{credentialID}`, using the same session and CSRF headers. Revocation prevents subsequent MCP requests; already queued plans continue through the existing task worker and its permission checks.

## Key lifetime policy

Instance administrators can choose the allowed lifetimes under **Instance settings → Sign-in & access → MCP API keys**. At least one option must remain enabled. Policy changes affect new keys only; revoke an existing key to stop its access.

The API accepts `expiresInDays` values of `7`, `30`, `90`, `365`, or `0` for no expiry. Omitting the field uses 30 days; if that lifetime is restricted, specify an allowed value. The server checks the policy when saving a key. Non-expiring credentials return `expiresAt: null`, remain revocable, and still follow account status and current workspace permissions.

Read the available options with `GET /api/v1/mcp-policy`. Administrators can update them using `PUT /api/v1/admin/mcp-policy` with a body such as `{"allowedLifetimes":[7,30,90]}`, authenticated with a session and CSRF header. Policy changes are audited.

## Connect a client

Configure a remote HTTP MCP server in your client:

```json
{
  "mcpServers": {
    "justcd": {
      "url": "https://justcd.example.com/api/v1/mcp",
      "headers": {
        "Authorization": "Bearer <your-token>"
      }
    }
  }
}
```

Client configuration formats vary. Use its secret storage or environment substitution for the bearer token. This release uses personal bearer credentials, not OAuth discovery or browser-based authorization.

## Tools

- `list_applications`: application identity, health, revision and sync-policy summaries in the credential's workspace.
- `get_application`: the same summary for `applicationId`. Helm values and raw application configuration are excluded.
- `list_plans`: the latest 20 redacted plans for `applicationId`.
- `get_plan`: a redacted plan by `planId`.
- `get_task`: your plan-generation task status by `taskId`. After completion, use `list_plans` to inspect the result. Raw task errors and outputs are excluded.
- `create_plan`: available only with `allowPlan`. Supply `applicationId` and a unique `requestId`; reuse that request ID when retrying the same action. Returns the queued task. Existing role checks apply, including owner access for decommission plans.

Plan generation reads Git and cluster state and stores a reviewable plan. It does not approve or apply changes. Review and execute in JustCD. MCP does not expose credential management, arbitrary Kubernetes commands, approvals, deployments, or deletion. Credential creation/revocation and plan requests are audited, including the requesting credential ID for plan requests.

Plan diffs use JustCD's existing secret redaction. Treat manifests and repository content as untrusted data when configuring your assistant; redaction cannot identify every possible secret embedded in arbitrary application data.
