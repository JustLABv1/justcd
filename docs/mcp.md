# Read-only MCP

JustCD exposes a stateless Streamable HTTP MCP server at `/api/v1/mcp`, on the
backend or through the normal frontend API proxy. It uses the
[official Go SDK](https://github.com/modelcontextprotocol/go-sdk) for protocol
negotiation, discovery, tool schemas and JSON-RPC errors. Responses use JSON;
there are no persistent MCP sessions or long-lived GET streams. Current SDK
clients and legacy clients using `initialize`/`notifications/initialized` work.

This endpoint supplies deployment **metadata** for assistants such as JustAI.
It never creates a plan, approves, syncs, rolls back, deletes, renders a source,
or connects to Kubernetes. Database reads run in a read-only transaction.
Unknown tools (including mutation names) fail. Tool annotations advertise
read-only behavior; the registered implementation enforces that boundary.

## Authentication and client setup

Use a trusted client that supports custom HTTP headers. MCP reuses JustCD's
existing login session and workspace permissions, including OIDC memberships.
Every HTTP request, including discovery, requires these headers:

```text
Cookie: justcd_session=<session>; justcd_csrf=<csrf>
X-CSRF-Token: <csrf>
```

Session credentials retain the user's normal REST permissions. They are **not**
a separately scoped read-only API token: connect only trusted clients, preferably
using an account with workspace `viewer` membership and no instance-admin role.
Revoking the session, disabling the user, or removing membership takes effect on
subsequent requests. Session expiry requires signing in again. This endpoint does
not advertise an OAuth discovery flow or accept a bearer API key.

For local-password login, the following standalone Python script securely prompts
for a password, signs in, and writes a header file with owner-only permissions.
Run it outside the assistant and keep the generated file out of Git and chat:

```sh
export JUSTCD_URL=https://cd.example.com
python3 - <<'PY'
import getpass, http.cookiejar, json, os, urllib.request
base = os.environ['JUSTCD_URL'].rstrip('/')
jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
body = json.dumps({'email': input('Email: '), 'password': getpass.getpass()}).encode()
request = urllib.request.Request(base + '/api/v1/auth/login', data=body,
    headers={'Content-Type': 'application/json'})
with opener.open(request) as response:
    response.read()
cookies = {cookie.name: cookie.value for cookie in jar}
headers = {'Cookie': 'justcd_session=' + cookies['justcd_session'] +
           '; justcd_csrf=' + cookies['justcd_csrf'],
           'X-CSRF-Token': cookies['justcd_csrf']}
fd = os.open('justcd-mcp-headers.json', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as output:
    json.dump(headers, output)
PY
```

For OIDC, complete the normal browser sign-in and configure the same cookies and
CSRF header through the client's private header settings. Do not send cookies to
a model. In JustAI or another MCP host, configure Streamable HTTP, URL
`https://cd.example.com/api/v1/mcp`, and the two headers above. The host must send
them on **every** POST. A host that supports only OAuth/bearer credentials needs
a session-aware adapter; unattended API-token support is tracked separately.

After connecting, list tools, call `list_workspaces`, then use a returned workspace
ID with `list_applications`. Use returned application IDs with the remaining
tools. Example `tools/call` arguments:

```json
{"name":"list_applications","arguments":{"workspaceId":"<workspace-id>","limit":25}}
```

To check the transport without a host, this sends a legacy initialization request
using the protected header file (no secrets in shell arguments):

```sh
python3 - <<'PY'
import json, os, urllib.request
headers = json.load(open('justcd-mcp-headers.json'))
headers.update({'Content-Type': 'application/json',
                'Accept': 'application/json, text/event-stream'})
body = {'jsonrpc':'2.0', 'id':1, 'method':'initialize', 'params': {
    'protocolVersion':'2025-11-25', 'capabilities':{},
    'clientInfo':{'name':'justcd-check','version':'1'}}}
request = urllib.request.Request(os.environ['JUSTCD_URL'].rstrip('/') + '/api/v1/mcp',
    data=json.dumps(body).encode(), headers=headers)
with urllib.request.urlopen(request) as response:
    print(response.read().decode())
PY
```

An SDK handles initialization, version headers and tool calls automatically.
Delete the header file after configuring your client's private credential store.

## Available tools

| Tool | Scope | Returned metadata |
| --- | --- | --- |
| `list_workspaces` | Visible workspaces | ID and name |
| `list_applications` | Required `workspaceId`, optional `applicationId` | ID, name, renderer, health, sync policy, pause/decommission flags, check time, last synced revision |
| `list_plans` | Required workspace and application IDs | ID, status, digest, revision, change count, creation and expiry |
| `list_operations` | Required workspace and application IDs | ID, plan ID, operation type, status, start and finish times |
| `list_application_audit` | Required workspace and application IDs | Application events only: ID, action, resource identity and time |

All tools accept `limit` (default 25, maximum 100) and `offset` (0–10000).
Continue with `nextOffset`, repeating the same workspace/application scope.
Offset pagination is a bounded metadata window; concurrent writes may shift
pages. It is not an audit export. Ordering is deterministic within each query.
Workspace permissions and application ownership are checked on every call, and
the data query itself includes the workspace restriction. Instance administrators
retain their existing access to all workspaces.

Application settings and Helm values, manifests, Secret payloads, annotations,
diffs, connection URLs/credentials, operation messages and arbitrary audit
`details` are excluded at the SQL projection. This intentionally omits sensitive
diagnostics rather than attempting to redact arbitrary nested payloads. Names
and revisions are user-authored metadata and must be treated as untrusted data.

Requests are limited to 64 KiB, metadata pages to 128 KiB and queries to five
seconds. Oversized responses fail with a request for a smaller page. Configure
`JUSTCD_PUBLIC_URL` and `JUSTCD_FRONTEND_URL` to the actual externally used URLs:
the MCP endpoint validates Host and Origin against these values. Local development
allows loopback hosts on different ports when a configured URL is loopback.
Cross-site browser requests fail; the frontend proxy preserves Host, Origin and
MCP protocol headers. There is no cross-origin CORS bypass.

## Verification

`bash scripts/integration.sh smoke` runs the PostgreSQL protocol/security tests.
With frontend dependencies installed, set `JUSTCD_E2E_MCP_PROXY=1` to also start an
isolated Next server and verify actual client discovery/list/call through its API
proxy. Tests cover authentication and revocation, viewers, workspace/application
isolation on every page, unknown mutations, payload omission, malformed requests,
body/pagination limits, and unchanged plan/operation state.
