# Large and high-risk plan review

Resource review groups the saved plan by risk, action, namespace, Kubernetes kind,
or application-group child. Risk chips are filters; risks overlap, so their sum
can exceed the resource count. Applied deletions, ownership transfers, cluster
scope, known immutable-field edits, and excluded resources/fields remain distinct.
Search includes resource identity, child name, changed/ignored paths, and the
redacted manifest text. Neither filtering nor searching edits the plan or digest.
Exclusion drafts are labelled separately and block approval/apply until saved as
a new plan or reset.

Sensitive values are redacted on both sides of the manifest diff. When those
values change, the review shows a **Sensitive fields changed** notice listing
the saved changed-field paths, even if the displayed manifests look identical.
Ownership-only updates are labelled separately from text changes.

The group page reads each child's current plan with a maximum of four concurrent
requests. It reports missing plans and read failures, and links to that child's
independent approval/apply flow. It does not create plans or combine approvals.

## Rules and evidence

New plans persist `approvalReasons` in JSONB through migration 043. Each reason
records its governing workspace/application policy or mandatory safeguard and
its affected resource identities. The reasons participate in the plan digest.
The review explains the saved count/eligibility, links to affected resources
(clearing conflicting view filters), and provides policy-setting links where the
workspace/application is available. Resource links and confirmation summaries
use pages of 20 so keyboard users can reach every item without thousands of
mounted links.

Deletion selects the deletion policy for the whole plan. Field ownership
transfer restricts approvers to owners and requires at least one approval;
its count retains any stronger effective policy requirement. Cluster-wide changes
require one owner when ordinary sync approvals are disabled. Rollbacks always
require owner review, with deletion policy raising the owner count when higher.
Ignored entries do not contribute approval resource scope. Older saved plans
retain their digest and security checks on recheck; the UI explicitly says that
rule provenance is unavailable until refreshed. Actual policy changes continue
invalidating the plan.

Immutable-field warnings are evidence-based advisory warnings, not a replacement
operation. The current backend does not automatically delete/recreate resources.
The frontend checks known Deployment/DaemonSet/StatefulSet selector paths,
StatefulSet service/claim templates, Job selector/template, RBAC roleRef, and
immutable Secret/ConfigMap data against the reviewed payload. Redacted values
retain changed-path evidence. Kubernetes may allow some conditional edits (for
example suspended Job scheduling changes), and custom resource immutability is
not inferred. An update may be rejected; a deliberate replacement needs separate
review. Ownership conflicts have a blocking explanation and open the existing
live-owner review dialog.

## Rendering and accessibility

The resource navigator and diff use measured TanStack Virtual rows (the same
virtualization library installed with ReUI DataGrid). A navigation list and
side-by-side code diff are not tables; shared Base UI/ShadCN buttons, selects,
inputs, disclosures, and checkboxes provide their interactive controls. This
avoids fabricating a table component for a code comparison. ReUI's
[data grid](https://reui.io/docs/components/base/data-grid) and
[TanStack Virtual API](https://tanstack.com/virtual/latest/docs/api/virtualizer)
were checked before adapting this pattern.

The listbox has one stable keyboard focus target. Arrow keys, Home, and End
select and scroll to resources across unmounted rows; the selected option stays
mounted and carries position/set-size metadata. Diff viewport scrolling, search,
context expansion, layout controls, and previous/next change controls remain
keyboard accessible. Context defaults to three lines around changes. Equal-length
large manifests use linear comparison rather than an unbounded LCS matrix;
unequal-length very large middles remain conservatively highlighted. Expanded
context and long wrapped lines remain windowed and measured.

## Reproducible synthetic fixture

From `services/frontend`, install the repository lockfile with
`pnpm install --frozen-lockfile`, then run:

```sh
JUSTCD_PLAN_REVIEW_FIXTURE=1 pnpm dev --port 3128
```

Open `/plan-review-fixture` only in an isolated development/test environment.
The route returns 404 by default and should never be enabled for production.
All fixture actions stay in browser memory and issue no deployment mutations.
The fixture contains 10,000 changes, 1,000 excluded resources, five child labels,
and one manifest with 10,000 data fields and three changed-path selection
controls. It is not a benchmark for 10,000 simultaneously expanded field controls.
It includes deletion, takeover, cluster scope, ignored fields, and 60 known
immutable selector edits.

On 2026-10-11, T3's collaborative Chromium 152 browser on macOS, development
build with locked Next 16.4/React 19.3 dependencies, 1280×800 viewport, produced
these single-run observations. Latency is from button click to the next animation
frame after the navigator's DOM mutation; it excludes network/first compilation.
The repeatable review budget is under 200 ms per risk-filter action and fewer
than 60 mounted navigator/diff rows on this fixture. These observations are not
a device-independent performance guarantee.

| Risk filter | Matching resources | Observed latency | Mounted resource options |
| --- | ---: | ---: | ---: |
| Deletion | 1,000 | 29.7 ms | 11 |
| Ownership takeover | 233 | 31.5 ms | 10 |
| Cluster scope | 104 | 27.2 ms | 10 |
| Ignored fields | 1,323 | 19.6 ms | 17 |
| Ordinary change | 8,365 | 17.3 ms | 17 |

Browser checks covered:

- End selected resource 11,000 and retained listbox focus with only 10 options mounted.
- An approval resource link cleared a no-results search and conflicting deletion filter.
- Searching the large diff for `field-09990` rendered seven matching/context rows.
- Next change scrolled to the edit near line 5,000 with 32 rows mounted.
- A draft resource exclusion disabled approval/apply while saved risk counts stayed fixed; reset restored approval.
- The confirmation dialog recorded an in-memory approval and enabled apply.
- Group-child search showed 2,000 of 10,000 resources for `Child 3`.
- At 390×844, the group review had no page horizontal overflow (body width 390).

Frontend model tests cover immutable payload preservation, filter/count agreement,
child identity isolation, redacted immutable Secret evidence, and large sparse
diff context. Backend tests cover rule precedence/resource scope, legacy digest
compatibility, provenance changes invalidating modern digests, and PostgreSQL
SavePlan/PlanByID/ListPlans roundtrips through JSONB and upgraded schemas.
The browser checks exercise the actual ChangesTab review and shared group
navigator; authenticated policy edits, real approvals and live adoption remain
covered by backend authorization/integration tests rather than synthetic actions.
