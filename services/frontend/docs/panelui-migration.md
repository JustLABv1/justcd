# PanelUI migration status

Updated 2026-11-09. This is an execution checklist, not a claim that every state has been visually
approved. Read this with `design-system.md`, especially its recorded user corrections.

## Implemented

- Application and route imports for buttons, inputs, textareas, checkboxes, switches, badges, tabs,
  tooltips, account/theme menus and sheets use PanelUI.
- `AppDialog` is an application wrapper over PanelUI Dialog. Form ownership is preserved; header,
  scrolling body and footer are separate. Busy forms refuse dismissal. Confirmation uses AlertDialog.
- `FormField` uses PanelUI Field and Label while preserving explicit control IDs and native validation.
  `FormSelect` uses PanelUI Select. Both live outside the legacy primitives directory.
- `Panel` was removed. Forms use Section; standalone objects use Cards; settings reuse shared wrappers.
- Connection creation uses PanelUI Steps and ChoiceCards, with horizontal progress on phones and a
  vertical sidebar on desktop. Application/group wizards also use Steps.
- Run progress uses read-only Steps with native horizontal scrolling and shared navigation buttons.
- Template records, approval requests, source summary, plan summary, workload groups and standalone
  operation progress use Cards. The dashboard retains Frames for attention and recent-sync digests.
- Sync history uses PanelUI BarChart. Approval/rollback paging uses Pagination and approvers use Avatar.
- Activity operations and rollback targets use PanelUI Table. Manifest comparison uses Toolbar; list
  categories and health filters use Chips. Auth screens share a PanelUI Card and Field layout.
- Feature settings use Cards and Sheets. Parent settings sections are flat when they contain record Cards.
- Retained library internals import PanelUI basic controls. Unused legacy controls were removed;
  Tooltip and Spinner compatibility entry points re-export PanelUI.

## Latest consistency pass

- Saved boolean choices use the shared CheckboxField, including feature settings, connection options,
  publishing, notifications, OIDC and user forms. Immediate preferences retain Switch. Labels and
  descriptions have separate accessible associations; the field row adds no nested surface.
- Shared error/settings notices use PanelUI Alert. Inside a Card or modal they use the local unframed
  `inline` variant, preserving severity and message semantics without another bordered surface.
- Account, member, user and audit actors use PanelUI Avatar. ConnectionRow accepts media separately
  from icon tiles and places padding on Card.Content once.
- Audit records now use native PanelUI Table columns and explicit keyboard-accessible detail buttons.
  Horizontal overflow stays inside the Table on narrow screens. Checked at 390px and 1440px;
  desktop rows remain compact, the header aligns, and older-event actions remain visible.
- The legacy Spinner entry point now re-exports PanelUI Spinner, including retained filter dependencies.
- OIDC/user creation and credential dialogs were inspected without saving. Credential types were
  changed locally with keyboard selection, including static kubeconfig and SSH. At 390px the long
  SSH dialog body scrolls while its footer remains within the viewport. All 36 dialog call sites were
  audited for explicit form actions or informational dismissal.

## Application collection and deployment-group pass

- Application records now use PanelUI Card with one padded Content part. Collapsible cluster/status
  groups are open canvas headings, removing a second border around Cards or Tables. Long group names
  wrap on mobile and filter counts have explicit spacing.
- DataGridList uses PanelUI Card/Table, with an unframed empty state and outline paging action.
  Background task summaries use Card with one padded body.
- Deployment targets use unframed form sections and namespace disclosures. Group detail targets are
  spaced record Cards. Child-plan review uses Card instead of a custom article surface.
- Agent connection and ownership-conflict dialogs no longer introduce nested bordered notices,
  metadata boxes or divider-based toolbars. Their existing shared inputs, selections and confirmation
  semantics are preserved. Populated conflict and agent states still need final visual checks.
- Application/group wizards share WizardSteps: readable minimum step widths, horizontal overflow on
  phones, and current-step scroll positioning without scrolling the whole page. Group breadcrumbs now
  identify the route rather than showing Not found.
- Browser checks: Applications Cards at desktop and 390px; group creation Source, Renderer options
  and Targets at 390px. No group was created. Final group detail, populated target namespace overrides,
  reconciliation/review, and application List mode still need checking.
- Typecheck, focused lint for these changes and diff whitespace checks passed.

## Resource inspector and dependency pass

- Resource details now use the installed PanelUI Inspector.Panel inside Workbench.Inspector. Its
  documented single-page API supplies scrolling and removes the custom panel surface. The Workbench
  mobile sheet removes the panel ring; the redundant desktop close action hides on phones.
- Inspector metadata, health readings, failures and conditions are unboxed. Container records use
  PanelUI Card. External URL actions use the shared outline Button and an up-right icon.
- Rollout summary uses one compact Card; the Sheet body uses plain resource/stage details without
  nested bordered notices or toolbar dividers. Populated rollout states still await final review.
- Browser review: desktop Resources and selected Deployment inspector; 390px full-screen Inspector
  sheet. No resource actions were run. A real layout defect was found: Traffic and Service widths
  could consume the Pods column. Three equal flexible columns fix it; duplicate Card body padding
  was removed from workload and other-resource groups.
- Import/export reference audit removed unused ReUI data-grid, frame, badge, icon, stepper and
  code-block surface files, plus unused ShadCN controls. The syntax highlighter and documented gaps
  remain. Removed unused recharts, embla-carousel-react and @tanstack/react-table dependencies with
  pnpm; the lockfile is updated. DnD dependencies remain in use by advanced Filters.
- Full frontend lint passed with zero errors and five warnings in retained vendor files. Typecheck
  passed after removing unused components and after the final Inspector changes. Focused lint on
  resource/topology/rollout changes and diff whitespace checks also passed.

## Publishing, onboarding and missing-page pass

- Installed PanelUI NotFound from the configured registry and replaced the custom 404 artwork/body.
  Header, appearance controls, recovery actions and existing copy remain. Checked at 1280px and 390px.
- Onboarding now uses PanelUI Steps with explicit backend completion flags, independently selected
  detail and reachable steps. The count is announced; selecting an earlier step does not mark later
  incomplete steps complete. Content uses open Section, cluster records use compact Card, and draft
  enrollment forms avoid nested surfaces. Checked cluster selection and form composition at 390px
  and 1440px without creating a cluster or token.
- Checkbox option helpers share one unframed implementation instead of duplicate bordered controls.
- Publishing ZIP drop target uses Card with one Content part; file selection uses shared Input/Button.
  PanelUI has no uploader: the native file capability and drag/drop handlers are retained with a
  keyboard Browse alternative. This is an explicit gap, also applicable to template JSON import.
- Publishing diagram uses Flow's own surface instead of a second custom wrapper; generated-file
  preview keeps code formatting without a nested bordered panel. Conversion incompatibility details
  are unboxed inside their dialog. Template empty states no longer draw a placeholder surface.
- Typecheck, focused lint and diff whitespace checks passed. Publishing upload/preview and populated
  conversion-error states still need final browser checks; onboarding enrollment commands were not run.

## Settings and modal composition pass

- Account sessions are compact PanelUI Cards under a flat settings section. MFA recovery/setup details
  stay unboxed inside their existing settings surface. Checked account security at desktop and 390px;
  no session or authentication changes were performed.
- Permission report details, package-source form groups, connection credential notes, repository
  destination rows and application render settings no longer draw nested bordered panels.
- Notification attempt history uses shared Disclosure. Checked the 390px Add destination dialog:
  initial name focus, draft checkboxes, scrolling body, visible footer and Escape dismissal, without saving.
- Settings navigation scrolls the active section into view on narrow layouts, including resize. Checked
  Notifications after direct navigation. Desktop keeps its existing vertical navigation.
- Pull-request review records use Card; branch/shared-environment metadata remains unboxed. Schedule
  modal records are open form sections. Error diagnostics keep a destructive summary without a nested
  red panel. Empty application states no longer draw dashed placeholder surfaces.
- ApprovalRuleEditor is an open form section everywhere; removed its obsolete custom card option.
- Typecheck and focused lint passed for the settings/modal changes. The final navigation adjustment
  also passed typecheck and focused lint; diff whitespace checks passed. Populated PR/permission/delivery states remain on the final checklist.

## Plan summary and populated-fixture pass

- Rollback target summary and sticky apply summary now use compact PanelUI Cards with one Content
  part. Ownership conflict uses PanelUI Alert; persistent-ignore disclosure and decommission choice
  avoid custom nested surfaces.
- Approval review uses a compact Card rather than a tinted custom section. Member avatars and labels
  are unboxed and approval evidence is separated with whitespace.
- Typecheck, focused lint and diff whitespace checks passed for these changes.
- The isolated fixture copy at `/tmp/justcd-panelui-review` uses `JUSTCD_PLAN_REVIEW_FIXTURE=1`,
  no copied environment files, and a temporary root layout without backend availability checks.
  Webpack compilation was stopped after no observable progress. Turbopack loads the fixture after
  using the canonical `/private/tmp` root and documented additional roots for linked dependencies.
- Checked synthetic 10,000-change / 1,000-ignore review, the 10,000-field manifest and embedded INI
  comparison at desktop and 390px. Unified mode, local search, options focus/Escape, and font-size
  RadioGroup arrow-key selection work. No backend data was changed.
- Fixed mobile resource review: Workbench now uses its compact breakpoint and a resource chooser
  Dialog. Search filters 11,000 resources; arrow keys preview choices while keeping the chooser open,
  Enter or a row click returns to the diff. Checked 390px and grouped-child review at 1440px.
- Fixed policy evidence disclosure placement: it occupies its own grid row below the message. Removed
  nested finding borders. Blocking policy disables Apply and associates its explanation with the button.
- Plan review controls use PanelUI Toolbar. Sticky apply summary wraps into deliberate rows on phones.
  No page horizontal overflow at 390px. Diff font size uses RadioGroup; narrow screens show Unified
  and disable the unavailable side-by-side view. Sensitive-field notices use inline Alert inside the Card.
- Latest typecheck, focused lint and diff whitespace checks passed after the final adjustments. The temporary
  fixture server is exec session `38571` on `127.0.0.1:3001`; stop it when the remaining fixture checks
  are finished. The original shared server remains unchanged.

## Dashboard and sync-chart verification

- Checked dashboard Waiting for review Cards, Recent syncs digest, Connections summary and insights
  tabs at desktop and 390px. Replaced the final underlined tab row with segmented Tabs and digest
  text-style actions with outline Buttons, including solid backgrounds on tinted surfaces.
- Isolated `/chart-review` page supplies synthetic 24-hour/7-day stacked history and empty, loading
  and error states. Checked desktop and 390px. Arrow-key focus exposes a readable bucket tooltip;
  the chart also provides a spoken data list with formatted bucket times and outcome labels.
- Fixed Active legend/bar token consistency and chart header wrapping: period tabs move below the
  title when narrow instead of squeezing the description. Error states announce a destructive message
  and omit the data legend and explanatory footer until data is available.
- Typecheck, focused lint and diff whitespace checks passed. Temporary chart page is outside the repo;
  it performs no backend writes. The chart's browser checks complete the outstanding populated-history
  item. The temporary page initially had a syntax error that was corrected before these checks. Its
  development console also logged script/preload warnings; no chart interaction exception was observed.

## Manual publishing verification

- Checked Application, Repository, Build, Secrets, Deployment and Review using a 205-byte synthetic
  static ZIP and reserved `.invalid` destinations. Archive validation succeeded. The endpoint source
  confirms temporary extraction and cleanup; no project, pipeline, application or deployment was created.
  The final publish acknowledgement stayed unchecked and its submit action stayed disabled.
- Inspected package-source Sheet empty/added forms at desktop and 390px, including draft sign-in choice,
  credentials fields and fixed Done action. Removed the local test source before continuing.
- Fixed FeatureCard's multiline body: one block inside the Button keeps title, description and summary
  vertically aligned. Previously the Button's flex layout placed them in separate columns.
- Fixed mobile Build overflow with an explicit one-column base grid and shrinkable FeatureCards.
  The 390px Secrets/Deployment/Review layouts have no horizontal page overflow.
- PublishingCheckbox now reuses the shared unframed CheckboxField. Review and success summaries use
  compact PanelUI Card instead of custom tinted wrappers; success links use outline Buttons with
  external-link icons. Removed footer border/divider and button size overrides.
- Existing manual-publishing local draft was backed up and restored byte-for-byte after navigation
  away from the form. Synthetic tokens were local form values only; they were never submitted.
- Typecheck, focused lint and diff whitespace checks passed.
  Result/progress, republish, profile-update and build-setup states still need browser verification.

## Publishing progress and verification-result pass

- Publishing's fixed stages now use read-only PanelUI Steps; the remaining ReUI Timeline is reserved
  for actual rollout events. Indicators, failed titles, badges and completed connectors share semantic
  tones. Removed duplicate body padding, stage spacing and the error-message divider.
- Checked running, failed, retrying and completed progress at desktop/390px through isolated synthetic
  `/status-review`. Pipeline action wraps below its heading on phones. Review changes invokes its
  callback; no pipeline or deployment action ran.
- Cluster connection result is now shared ConnectionVerificationResult: PanelUI Alert, plain metadata
  and shared actions. Checked success and TLS failure/remediation layouts at desktop and 390px using
  synthetic reports, without running a real cluster check. The wizard's preflight metadata grid is plain.
- Profile publishing success summary uses Card and its draft footer is open rather than a custom panel.
  That success route still awaits final populated browser review.
- Typecheck, focused lint and diff whitespace checks passed.

## Registry check

Checked `@panelui` through the configured registry on 2026-11-09: 80 available components.
Installed Avatar, Pagination, BarChart, ChoiceCards, ButtonGroup, Toolbar and RadioGroup from its
source definitions, preserving existing customized components. BarChart requires `@tanstack/charts`
(1.5.0 in the lockfile). The combined installer stalled during dependency installation; registry
source was inspected/copied and the required package installed separately with pnpm.

## Browser verification completed in this batch

| Screen / interaction | Checked |
| --- | --- |
| Workspace general settings | Desktop and 390px layout |
| Edit workspace details dialog | Initial focus, field layout, fixed actions, Escape/Cancel, desktop and 390px |
| Approvals inbox | Cards, shared controls, desktop composition |
| Audit trail and event detail | Event Sheet, metadata, tabs, footer, Escape, desktop |
| Application overview | Compact plan/source Cards, KPI composition and hierarchy |
| New application wizard | Open form sections and step navigation, desktop |
| Application Activity | Operations/rollback Table composition, source Card, empty paging |
| Application Changes | Applied-plan status, expand applied review; example plan has no changes |
| Publishing catalog and manual flow | Template Cards, entry choices, manual wizard first step |
| Workspace publishing settings | Template record Cards, action hierarchy and profile empty state |
| Sign-in and registration | Separate unauthenticated profile, desktop/390px registration, invalid submission errors, first invalid focus, no registration request |
| Git source connection wizard | ChoiceCards and Steps; corrected title/description alignment |
| Cluster connection wizard | Open form layout, desktop sidebar and 390px horizontal Steps |
| Dashboard | KPI layout, attention digest and shared controls |

No save, deployment, deletion, connection test or external write was performed during visual checks.

## Application forms and wizard transition review (2026-11-10)

- Application creation/editing use open namespace and retry disclosures; redundant bordered
  notes/dividers were removed. Workspace creation has an open footer and shared Button actions.
- Saved boolean choices now share `boolean-field.tsx`; labels and help text have separate accessible
  associations, including options previously rendered by `CheckboxCard`.
- Checked application edit at desktop/390px, expanded retry settings and approval override controls.
  Checked workspace creation at 390px and blank-submit validation; no workspace was created.
- Checked application creation Source, Renderer, Cluster/namespaces and Sync policy at 390px.
  A Next-to-Review bug created the temporary `panelui-review` application: React reused the Next
  button as the submit button before the click default action. Both application and group footers
  now cancel Next's default action and mount the final submit separately.
- Verified both footers in an isolated local fixture: Next reaches step 5 with **0 submissions**;
  explicitly pressing Confirm increments to **1 submission**. The fixture has no backend writes.
- The temporary application had zero managed resources and was deleted with the user's approval
  using “Keep resources in Kubernetes.” Its automatic Git check hit the reserved `.invalid` source;
  no Kubernetes resources were deployed. Earlier no-write notes apply only to their individual passes.
- Typecheck, focused lint on the shared boolean controls, wizard footers and changed forms passed.
  Remaining review includes the real creation Review composition and the broader route/dialog gates below.

## Group creation and publishing composition review (2026-11-10)

- Deployment-group creation: explicit base grid columns, plain workspace-reset text, wrapping target
  headings and outline page navigation. Checked Source → Helm options → target namespace selection
  and expanded namespace overrides → Reconciliation → Review at 390px; checked Review at 1280px.
  Keyboard Next reached Review without submitting. No group or target application was created.
- Group detail grids now declare their base column. Resource review uses Card title/description/body
  slots; policy and approval evidence support an unframed inline treatment inside the resource Card.
  Checked the shared inline evidence in a synthetic compact Card at 390px. The complete populated
  group-detail/combined-resource review remains a separate verification gate.
- Republish: removed extra fieldset padding, replaced the hand-styled profile box with a compact
  PanelUI Card and the profile-update action with an outline Button. Checked populated synthetic
  profile state at 390px/1280px, with final upload disabled and unchecked confirmation.
- Build setup: template parameters, token/review and final apply controls now use open Sections.
  Once a plan exists, Review becomes secondary to Apply. External build links include an up-right
  icon in build setup and profile update. Checked populated build form and synthetic setting/file
  review at 390px. No GitLab requests or builds were made: the isolated fixture API returns local
  values and rejects unrecognised requests.
- The temporary fixture initially served stale server markup after file replacement; fresh-cache
  preview confirmed inline policy/approval classes. Do not count the earlier mismatched screenshot.
- Typecheck, focused lint for all changed group/publishing/evidence files and diff whitespace pass.

## Connection and security dialog review (2026-11-10)

- Sharing, namespace and agent profile grids now start with explicit single columns. Permission
  results are plain aligned rows inside their dialog, rather than tinted miniature panels.
- Account-security errors and success feedback use SettingsNotice; its supported semantic tones
  now include destructive/success with alert/status roles. Existing session Cards and MFA form
  checked at 390px. Blank MFA submission focused the required password; no enrollment occurred.
- Sharing offer dialog checked desktop/390px, keyboard Select selection and disabled Send offer.
  Closed without sending. No live credential, sharing or permission changes were made.
- Namespace grant dialog checked desktop/390px with form ownership retained. Automatic approval
  review rejected a live blank-submit test because the action grants permissions. The isolated
  fixture verified empty submission instead: “Enter a namespace”, aria-invalid=true and focus on
  namespace-name. The handler now explicitly restores focus after missing-namespace validation.
- Agent dialog checked with synthetic offline agent, reported profile, managed credentials and
  failed environment at 390px/1280px. Body scroll reaches lower actions; nested confirmation was
  opened and cancelled. No token enrollment/revocation or direct-mode change occurred.
- Fixed shared AlertDialog labels clipped by fixed-height equal-width buttons. Mobile actions now
  stack, with the primary above Cancel; long labels can wrap and grow. Desktop remains a row.
  Verified “Create enrollment token” on mobile/desktop and cancelled both.
- Typecheck and focused application lint passed. Vendor AlertDialog lint is checked explicitly
  because the normal lint configuration excludes registry sources. Its explicit check exposed five
  empty-interface declarations and a render-time callback ref assignment; the interfaces are now
  equivalent type aliases and the callback ref updates in an effect.

## Template editor and workspace dialog review (2026-11-10)

- Template editing now uses installed PanelUI Workbench/Canvas for both editable/rendered panes and
  comparison. The outer settings section and final version controls are flat, removing a bordered
  editor nested in a settings Card. Textareas use documented fixed sizing, fill their panes and
  preserve independent code scrolling. Configuration/helper grids have explicit base columns.
- Template comparison is unified at phone widths and side-by-side from sm upward. Unchanged lines
  appear once on phones, with changed before/after lines carrying their existing signs and tones.
  A comparison no longer forces a 40rem two-column layout into the mobile viewport.
- Fixed the preview selector's empty value to display “Sample values” through FormSelect's
  emptyOption API. Template JSON selection now uses shared Input with native file capability.
- Checked actual Shiny template edit/rendered output at 390px/1440px, local code change, mobile
  comparison, Reset this file and disabled Save as new version after reset. Measured no page
  horizontal overflow (390px/1440px respectively). No template version was saved; local edit reset.
- Workspace deployment-approval dialog checked at 390px: count, role choices, member search and
  footer layout. Closed without saving. Broader workspace policy/schedule dialogs remain open gates.
- Removed duplicate top padding from notification destination/history dialog bodies. Checked new
  Slack destination at 390px, switched provider with keyboard, checked email fields at 1280px and
  retained form ownership of footer Save. Closed without saving/sending any notification.
- Typecheck, focused lint for template editor/library and notifications, and whitespace checks pass.

## Application resources, exclusions and PR settings review (2026-11-10)

- PR reporting settings use open disclosures for approvals, webhook, preview access and Helm values.
  Removed redundant section/footer dividers, declared base grid columns and replaced custom saved
  feedback with PanelUI Alert. Secondary page navigation uses outline Button.
- Checked the demo application's PR reporting form at 390px, expanded account approvals and added
  a local mapping row. No API token entered, no reporting settings saved; navigating away discarded
  the draft. Populated PR review records remain a separate verification gate.
- Resource-change Cards now own body padding once: removed inset notice margins and nested recreation
  and exclusion horizontal padding. Ignore reason is announced once in the inline notice instead
  of repeated in three locations. Field choices have explicit base columns.
- Isolated synthetic resource review: checked excluded diff, resource/field checkbox labels,
  permanent-ignore disclosure and disabled empty-reason actions at 390px. Checked rescale confirmation
  at 390px/1280px, full action label, replicas input and disabled confirm for 10001 replicas; cancelled.
  The fixture rejects API writes. Redeploy/delete and full apply remain separate verification gates.
- Actual Resources tab checked desktop/390px. Found wrapped toolbar covering map notice on phones;
  increased mobile canvas top spacing. DOM bounds then showed toolbar bottom 324px and notice top
  336px (12px clearance), and the page retained 390px scroll width. Inspector/fullscreen still pending.

## Restart recovery, inspectors and fullscreen review (2026-11-10)

- Recovered after the T3 restart. Reran the cancelled application/component eslint command with
  retained ReUI/UI directories excluded: passed. Fresh frontend typecheck and focused lint passed.
- Resource inspector checked on desktop and as a phone sheet. Resource-map floating shell now uses
  installed PanelUI Toolbar, with registered Toolbar.Button actions retaining loading/pressed state.
- Fixed resource-map and plan-review fullscreen sizing. The previous fullscreen: utility classes
  did not generate styles in this setup, and the inner Workbench kept its regular height. Both
  now derive container/Workbench classes from the browser fullscreenchange state. Map and large
  plan fixture measured a 1320px fullscreen container with a 1296px Workbench and 12px inset.
  Fullscreen scopes only the Workbench area. The toolbar label reflects enter/exit state.
- OIDC and user create/edit dialog grids declare base columns. Custom validation now focuses the
  first failing OIDC field, user email/password, or group claim field. Checked actual OIDC add
  layout at 390px without creating a provider; fixture submit validation is still pending.
- Isolated preview was absent after restart, then its Turbopack process remained idle and HTTP
  requests failed repeatedly. Restarted with Webpack; session 36074 is live and reports compiling
  settings-review-identity, but has not returned the page. Fixture was reduced to the dialog
  components. Do not infer a passing browser validation check from source/typecheck results.
- No provider, user, permission, enrollment or resource mutation was performed in this pass.

## Remaining surface audit and live evidence (2026-11-10)

- Source audit found no raw button/select/input controls outside PanelUI/ReUI/UI implementation
  directories. Native file capability now goes through the shared Input entry point.
- Audit deployment targets, approval records and policy findings use compact PanelUI Cards rather
  than hand-styled bordered divs. Checked a live deployment event's target and surrounding evidence
  in the Sheet at 390px/1280px. Its approval and policy finding lists were empty; populated lists
  remain a separate gate. Data is read only; nothing exported, copied or changed.
- Runtime-health resource warnings are plain status/text rows within their dialog, with whitespace
  before transitions. Checked the demo application's Missing Deployment row at 390px, including
  reason, message and transition disclosure. No cluster request/action was triggered.
- Mobile agent-activity list uses Card around its semantic ul, retaining row dividers. Session
  loading/retry feedback uses compact Card/Content and destructive error text. These states still
  need their explicit visual check; the controls themselves remain shared primitives.
- Typecheck, focused lint for audit/runtime/agent activity/AppShell and diff whitespace passed.
- Identity fixture remains unresponsive despite an emitted server page module and a confirmed live
  Webpack session (36074). Navigation was not counted as completed verification. Continued useful
  live-page and source checks independently; do not mark the overall migration blocked or complete.

## Connection wizard route composition (2026-11-10)

- Found remaining hand-styled route surfaces in cluster and Git-source connection wizards despite
  their shared primitives already being migrated. Error/access states now use ErrorNotice/PanelUI
  Alert; loading uses retained shared Skeleton. Credential forms and namespace notes are open,
  summary dl grids use spacing rather than bordered cells, and empty states have no extra box.
  The Git read-success state uses a success Alert.
- Both wizard grids declare explicit base columns. Removed boxed sidebar notes and duplicate
  credential field padding; preserved labels, help and the existing shared Checkbox/Select controls.
- Git wizard: checked step 1 and the create-repository/expanded credential fields at 390px/1280px.
  Cluster wizard: checked step 1 and direct API/expanded credential fields at 390px/1280px.
  No values were entered; no credential, source, endpoint or namespace was saved or tested.
- Automatic review initially rejected generic Continue as possibly mutating. Source/DOM checks
  established step 1's visible control is type=button outside a form and only updates local step
  state; retried that specific control and verified navigation. Step 2 submit remained untouched.
- Both pages measured 390px scroll width at 390px. Typecheck, focused route lint and whitespace
  passed. Namespace/verification success and error states still require isolated fixture evidence.
- Loading skeleton composition and maintenance/offline route review remain pending; this pass
  prioritised concrete wizard gaps found by the route audit.

## Loading composition and service-state review (2026-11-10)

- Rebuilt ApplicationDetailSkeleton from PanelUI Card parts to mirror the compact overview:
  one plan summary, three metrics, chart and source. Removed old bordered-tab/header placeholders
  and duplicate body padding. All placeholders remain the documented Skeleton gap, aria-hidden
  beneath a single loading status. Base grids are explicitly single-column.
- Shared SectionSkeleton uses PanelUI Card and whitespace beneath its placeholder header; list
  dividers remain between its homogeneous placeholder rows.
- Typecheck and focused skeleton/shared-settings lint passed. Stable skeleton visual review remains
  pending because the isolated fixture server is unresponsive. Added a minimal /skeleton-review
  route only to the temporary preview copy; no product fixture or service setting was enabled.
- Checked maintenance settings at 390px/1280px with its switch left unchanged. Checked public
  maintenance and offline pages at 390px/1280px. Content and PanelUI action hierarchy fit without
  overflow. Decorative CSS artwork is semantic noninteractive content, not a replacement control.
  No maintenance toggle, sign-in or reconnect action was performed.

## Recovered fixture gates and final custom-surface pass (2026-11-10)

- Removed the audit filter row's hand-drawn Card and explanatory note box, leaving shared filter
  controls on the page canvas. Checked the populated audit page at 390px. Removed an extra boxed
  approvals empty state. Branch-test status now uses compact Card/Content; conflict guidance and
  its grouped recommendations use plain rows within their existing context.
- Resolved the isolated-server blocker with a temporary development-only route on the already
  running frontend. Its request helpers rejected writes and all data was synthetic. The route,
  component copies and mock API were removed after verification; no new product route remains.
- OIDC creation validation verified: empty submit announced four errors/focused oidc-name; after
  entering a display name, focus moved to oidc-issuer. User creation validation verified: blank
  submit marked email/password invalid and focused email; valid synthetic email moved focus to
  password. Checked the user form/error layout at 390px. No provider/user was created.
- Stable ApplicationDetailSkeleton and SectionSkeleton verified at 390px/1280px, with correct loading
  labels and no horizontal overflow. This completes their previously pending visual gate.
- Synthetic branch-test summary and conflict guidance verified at 390px/1280px. No branch/review,
  ownership-selection, AI request or resource action executed. Fixture setup errors were corrected
  before these screenshots; they were not product errors.
- Final typecheck, focused lint for changed audit/approval/guidance/branch files and whitespace
  checks passed after removing the temporary route. Stopped the unused isolated Webpack server
  (session 36074 exited successfully).
- Historical notes about the unresponsive isolated identity server remain as context; these new
  fixture results supersede the pending creation-validation and loading-skeleton gates. Edit-user,
  group mapping and populated agent-activity variants still require explicit verification.

## Onboarding and workspace schedule review (2026-11-10)

- Onboarding readiness uses a compact PanelUI Card around its retained Disclosure, with Card-owned
  padding; loading uses shared Skeleton. Base setup grid declares its single column. Expanded
  populated readiness checked at 390px/1280px, including status marks, metadata and actions.
- Workspace deletion-plan feedback uses shared warning Alert, with wrapping outline Buttons to
  application plans instead of a custom banner and text-link actions. No real workspace deletion
  was initiated; the populated notice still needs synthetic visual verification.
- Schedule editor grids declare base columns, and the managed-in-Git note no longer adds a header
  divider. Checked empty maintenance windows, added a local draft window, inspected timezone/time
  inputs and weekday choices at 390px/1280px, then cancelled. No schedule was saved.
- Checked populated fixed-date freeze editor at 390px and cancelled. Recurring freezes and desktop
  freeze layout remain separate gates. No freeze was removed, changed or saved in the backend.
- Typecheck, focused onboarding/workspace/schedule lint and whitespace checks passed.

## Recurring schedules, members and workspace AI (2026-11-10)

- Recurring freeze editor checked at 390px/1280px: switched the local draft from fixed to weekly
  with keyboard Select navigation, inspected timezone/time/weekday controls, then cancelled. No
  freeze was saved. This completes the earlier recurring-freeze layout gate.
- Member-add dialog checked at 390px/1280px. Keyboard role selection reached Owner in the unsaved
  dialog; footer submit retains its form owner. Cancelled without email entry or adding a member.
  Existing member role controls, which save immediately, were left untouched.
- Workspace AI policy and inline assistant checked at 390px/1280px without toggles or requests.
  Found long prompt buttons clipped on phones despite the page reporting no horizontal overflow.
  Shared prompt Buttons now wrap and grow inside the available width; measured both longest
  prompts at left=20/right=370 in a 390px viewport. Desktop retains compact rows. Labels use JustCD.
- Typecheck, focused AI-assistant lint and whitespace checks passed. No AI analysis was sent and
  no policy, membership, schedule or credential change was saved.

## Publishing profile forms and destructive confirmation (2026-11-10)

- Publishing-profile grids now declare single-column bases, including destination and feature
  sheets. Checked new-profile fields at 390px and the Source and build sheet at 390px/1280px.
  No field values, secret values or profile were saved. Other advanced sheets/version detail remain
  explicit visual gates rather than inferred from this one sheet.
- Replaced publishing-profile deletion's AppDialog with shared ConfirmDisclosure/PanelUI AlertDialog,
  including the destructive variant, busy label and pending-disabled action. Deletion failures now
  rethrow after recording the error, allowing the shared confirmation to remain open and announce
  the failure instead of treating a caught failure as successful completion.
- Temporary development-only fixture verified the deletion confirmation at 390px/1280px. Its
  confirm callback only threw a synthetic error: no request was sent. Verified error feedback and
  the alertdialog remaining open, then cancelled. Temporary route removed after the checks.
- Final typecheck, focused publishing-profile lint and whitespace checks passed after cleanup.

## Advanced publishing sheets and notification confirmation review

- Checked local publishing-profile drafts without saving: credential modes (CI variables and Vault)
  at 390px, Route and Ingress routing at 390px, and runner/trust fields at 390px and desktop.
  Ingress routing also checked at desktop. Profile version history remains pending.
- Notification destination deletion now uses shared ConfirmDisclosure/PanelUI AlertDialog. Its
  asynchronous callback propagates failure so the confirmation stays open with error feedback.
- A temporary development-only notification fixture used local synthetic destinations/deliveries
  and rejected writes. Verified deletion failure leaves the confirmation open; no deletion request
  was sent. Checked populated delivery history at desktop and 390px and expanded attempts by keyboard.
  The error toast obscured part of the mobile attempt list, so unobscured mobile history still needs
  a follow-up. Removed the temporary route and closed the review tab after checking.
- Typecheck, focused notification lint and whitespace checks passed after fixture cleanup. The first
  typecheck encountered a mock signature error in the temporary fixture; the cleaned application
  tree passed. The restarted application lint sweep remained silent after a prolonged wait and did not
  produce a completion result in this pass (session 94118). Do not count it as passed; resume its
  result or investigate the slow sweep before final migration sign-off.

## Remaining verification and composition work

- [x] Recheck the final dashboard review/connection Cards below the fold (desktop and 390px).
- [x] Inspect sync chart with non-empty history; exercise keyboard tooltip navigation.
- [ ] Check application Resources, Changes, Activity, Pull requests and Settings tabs on desktop/mobile.
- [x] Check populated plan-review fixture and mobile chooser, diff options, font keyboard control and policy blocking.
- [ ] Finish resource action/deployment confirmations, ignore controls and fullscreen verification.
- [x] Check manual publishing Application through Review, synthetic ZIP validation, package-source Sheet and disabled submit.
- [x] Check publishing progress running, failed, retrying and completed states at desktop/mobile.
- [ ] Finish publishing result, republish, profile update and build setup checks.
- [x] Check deployment-group creation through Review at desktop/mobile without submitting.
- [ ] Finish populated deployment-group detail/combined review and application edit verification.
- [x] Verify OIDC/user creation validation and stable loading skeleton layouts.
- [ ] Check instance settings sections, workspace members/publishing/policies/notifications/AI, account
      security, and their edit/connection/confirmation dialogs.
- [ ] Check connection inventories, sharing, credentials, namespace access and agent enrollment dialogs.
- [x] Check offline and maintenance pages and maintenance settings at desktop/mobile.
- [ ] Finish signup/onboarding and workspace creation/list verification. Sign-in, registration,
      onboarding and workspace creation already have specific evidence in the passes above.
- [x] Check template editor edit/rendered/comparison, reset and unsaved version controls.
- [ ] Review Cards for nested Alerts/bordered subpanels and remove duplicate part/body padding.
- [ ] Review richer record lists against Table and secondary controls against registry components.
- [x] Audit unused legacy vendor files and remove unreferenced alternative implementations/dependencies.
- [ ] Run final typecheck, lint and diff whitespace checks after the remaining changes.

## Retained gaps

The current registry has no equivalent for these capabilities:

- ReUI advanced Filters plus its structural popover/cascader dependencies.
- Disclosure/collapsible detail and Skeleton loading placeholders.
- Vertical event Timeline; PanelUI Timeline is a media editing timeline.
- Calendar date selection in the filter editor (React Day Picker).
- Shiki syntax-token utility used by virtual manifest diffs (no ReUI code-block visual surface).
- Native file capability for publishing ZIP selection/drag-drop and template JSON import, through
  shared Input/Button controls. No PanelUI uploader exists.

These are explicit design-system exceptions. Do not replace them with inappropriate components simply
to eliminate an import. Recheck the live registry when extending a gap.

## Checkpoint for continuation

- Latest shared-control/notice/avatar pass and audit Table typecheck passed. Focused lint passed on
  the shared controls, settings wrappers, account menu and audit files before final density changes.
  The last density changes are covered by a further focused lint run.

- Typecheck passed after the shared migration, registry installation, Table/Toolbar changes and auth
  layout updates. Run it again after any subsequent edits.
- Application/PanelUI lint passed with no warnings before the final form-validation adjustments.
  Focused lint on the final changed forms, auth pages, Activity, manifest toolbar and template library
  also passed. A redundant final whole-application sweep was stopped after it ran unusually slowly.
  Full-tree lint has existing warnings in retained
  ReUI vendor sources (cascader/code-block/data-grid/filter implementations), with no lint errors.
- The browser plan-review fixture route is disabled unless `JUSTCD_PLAN_REVIEW_FIXTURE=1`; use a
  real application with resource changes for the remaining diff/toolbar checks, or a separate fixture
  server. Do not enable it on a shared server without understanding its configuration.
- Original demo application `4decb315775be64ed6410ac8b2056cd8` has an applied historical plan with
  zero resource changes. Demo workspace `f1b1327aca22d962365ef2433491a7e9` has reserved `.invalid`
  Git/cluster endpoints, so backend connectivity errors are expected in that workspace.
- The active goal remains incomplete. Finish the remaining route/dialog/state review, library-gap
  dependency audit and final checks before marking it complete. No commit or PR has been created.
