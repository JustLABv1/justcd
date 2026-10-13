# Connections UI audit

Reviewed 13 October 2026. Scope: Git sources, Kubernetes, Credentials, and Sharing.

| Tab | Before | After | Why |
| --- | --- | --- | --- |
| Git sources | Repository connections and branch discovery both used “connect” language. Paused discovery left a disabled primary action. | Separate “Connected repositories” and “Application discovery” sections. Use “Connect Git source”, “Add discovery branch”, and an enabled “Resume discovery” action. Label the scanned commit. | Users can distinguish repository access from the branches that create and update applications. A paused branch has a visible next step. |
| Kubernetes | Clusters and namespaces appeared in separate sections with a second cluster selector. | Nest namespace access inside each expandable cluster row. Keep connection details, credentials, and test actions with that cluster. Remove the separate selector and retain cluster-specific links, inline loading and retry. Open detailed permission checks and suggested RBAC from each namespace’s View checks button in the shared modal. Keep advanced self-test options in a disclosure. | Users see a cluster and its deployment permissions together, with a clear parent–child relationship. |
| Credentials | Git and Kubernetes credentials appeared in one undifferentiated list. Scope was implicit for workspace credentials. Operator environment-variable details appeared in the product UI. | Group Git and Kubernetes credentials. Label both workspace-private and instance-wide scope. Explain credential reuse, rotation, and privacy when sharing. Keep format-specific requirements in the credential editor. | Users can find the right credential and understand who can use it without infrastructure implementation details. |
| Sharing | Incoming offers, accepted access, outgoing offers, and history were mixed together. Load failures could resemble an empty list. Saving an unchanged Git credential could clear its association. | Prioritize incoming offers, separate accepted incoming access and outgoing shares, and collapse past offers and revoked access. Give failures a retry state; explain history-only states. Choose an available connection type when offering. Disable unchanged credential saves and preserve the saved credential in the request. | Users can see what needs a decision, what is currently usable, and what is historical. The credential association cannot be accidentally cleared by an unchanged save. |

Each tab now has a task-oriented description and its primary action inside the selected tab. The existing shared tabs, inventories, dialogs, selects, buttons, and disclosures are reused.

On narrow screens, inventory icons and labels stay together, actions align within the row, long credential labels wrap, and the workspace navigation shortcut remains readable.

## Verification

- Frontend typecheck and lint for touched files passed.
- Twenty-four browser checks passed using isolated API fixtures against the running frontend. Fixtures covered populated data, all four empty states, viewer permissions, sharing failures and history-only states, namespace selection and error recovery, discovery and offering dialogs, and 320 px layouts.
- Desktop and mobile captures were visually inspected. The collaborative browser was used for the initial live audit; an isolated headless browser provided state checks and captures after collaborative screenshot failures.
- The browser audit made no server writes and reported no page errors.
- The credential-save regression was checked by confirming an unchanged saved credential leaves Save disabled. Source review also verified the request preserves the saved credential when no draft selection exists.
