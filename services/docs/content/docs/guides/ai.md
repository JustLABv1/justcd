---
title: "AI assistance"
---

JustCD can connect to your own LiteLLM instance or another OpenAI-compatible
Chat Completions gateway. AI is optional and disabled by default. Analysis is
read-only: it never applies manifests, grants approval, changes ignore rules,
or performs a rollback.

## Connect LiteLLM

1. As an instance administrator, open **Settings → AI**.
2. Enter the API base URL, such as `https://litellm.example.com/v1`. A root URL
   such as `https://litellm.example.com` also works. Include any reverse-proxy
   path prefix before `/v1`. The backend must be able to reach this URL; private
   network endpoints and HTTP are supported for local gateways.
3. Enter a model alias configured on your LiteLLM instance and, if required, a
   dedicated virtual API key. Set the maximum output tokens (256–8192).
4. Save the connection, then use **Test saved connection**. This makes a small
   model request with synthetic evidence and validates structured JSON output.
5. Enable the connection. A workspace owner must also enable **Workspace
   settings → AI → Allow AI analysis in this workspace**.

The key is encrypted using the instance encryption key and is included in offline
key rotation. It is never returned to the browser. Leaving its field blank keeps
the saved key; removing it clears it. Changing the endpoint clears the previous
key unless you supply a replacement, preventing accidental credential forwarding.
Redirects from the configured endpoint are rejected.

LiteLLM uses an [OpenAI-compatible interface](https://docs.litellm.ai/docs/).
Configure model routing, retention, budgets, quotas, and provider credentials on
LiteLLM. JustCD calls `/v1/chat/completions` with the selected alias, a bearer key
when present, and a bounded `max_tokens` value. The model must follow instructions
to return JSON. No embeddings, vector database, or separate model SDK is required.

## Features

Use the outlined AI action in the application or deployment-group header:
**Diagnose with AI**, **Explain with AI**, **Summarize with AI**, or **Compare with AI**,
depending on the current view. The button is hidden until AI is enabled for both the instance
and workspace. Results open in a dialog.

- **Application → Changes:** explain the selected plan and its drift. Analysis
  uses that exact stored plan and digest, including ignored changes, recreation
  requirements, and current approval records.
- **Application → Overview:** diagnose health from stored resource conditions,
  operation progress, recent plans, and live Kubernetes events where permitted.
- **Application → Activity:** summarize recent deployments or select one of the
  ten most recent operations.
- **Deployment group:** compare targets using revisions, filtered rendered
  manifests, observed health, and deployment history. Ask follow-up questions
  within that group.
- **Application header → AI action:** ask scoped questions about the application.
- **Workspace settings → AI:** ask questions across the workspace. All members
  can use enabled AI; only owners can change workspace opt-in.

Findings link to evidence from JustCD. Answers identify the model and generation
time. Re-run analysis after changing configuration or deploying; an answer is a
snapshot, not a continuously updated health assessment. Conversations remain in
browser component memory and are not stored as chat history by JustCD. Provider
logging and retention are controlled by your gateway.

## Context and limits

The backend checks workspace membership and validates application, group, and
plan ownership before sending context. AI cannot fetch arbitrary URLs or execute
tools. Context includes deployment metadata, up to three recent plans per
application, ten recent operations and health transitions, and up to eighty
resource observations per application. A selected plan replaces the recent plan
set. Plan evidence includes up to one hundred changes, thirty ignored changes,
and eighty filtered rendered resources. Larger sets are marked as truncated.
Scopes above thirty applications or 200 KB of serialized evidence are rejected;
narrow the question to an application or smaller group.

Raw Helm values, Kubernetes Secret/ConfigMap contents, environment values,
commands, annotations, and arbitrary custom-resource specs are excluded. Only
allowlisted Kubernetes structural fields are included in manifest projections,
so omitted configuration can limit diagnosis. Diagnostic strings use JustCD's
credential redactor, but arbitrary messages and user questions can still contain
sensitive information. Workspace owners should review the configured gateway's
data policy before enabling AI.

Live event reads use the application's existing namespace credentials and are
limited to observed resource UIDs. Optional container logs require an explicit
checkbox on each diagnosis panel. Up to two observed pods and two containers per
pod contribute at most fifty lines / 4096 bytes each. The live pod UID is checked
before fetching logs. Log redaction is best-effort. Current cluster agents reject
log subresources; those connections report that logs are unavailable, while
permitted events and stored observations remain usable. Permission errors and
short diagnostic timeouts produce incomplete-evidence notices.

Requests are limited to one active analysis per user, at least five seconds
between requests, and eight active analyses per backend process. Model requests
time out after sixty seconds; total analysis time is bounded to seventy-five
seconds. These are request limits, not a monetary budget. Set virtual-key budgets
on LiteLLM, especially for deployments with multiple backend replicas.

AI connection changes, workspace opt-in, and analysis requests are audited.
Questions, context, raw log tails, API keys, and generated answers are not placed
in the analysis audit event. AI findings are advisory; the ordinary plan review,
permission checks, and exact approval rules remain authoritative.
