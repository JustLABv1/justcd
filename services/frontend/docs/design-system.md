# JustCD design system

Every screen is built from PanelUI (`@/components/panelui`, registry `@panelui`). This file decides
**which** PanelUI component to use. It is binding for new UI and for any UI a change touches.

The one idea: **choose the component from the shape of the content.** Frame is one option among
many, and usually not the right one.

## 1. Surfaces

The page has three levels and never more:

| Level | What it is | Drawn with |
| --- | --- | --- |
| 0. Shell | Sidebar and the rounded content canvas | `AppShell` |
| 1. Section | A heading with content under it. Open on the canvas, or one Card (`Section surface`) | `Section` (section 8) |
| 2. Surface | One bordered thing inside a section | `Card`, `Frame`, `Table`, `Kpi`, `Alert`, `Workbench` |

### Surface colours

Dark surfaces are one ladder, defined in `app/globals.css`. Each step is lighter than the one it
sits on; a surface is never darker than its parent.

| Token | Utility | Used for |
| --- | --- | --- |
| `--shell` | `bg-shell` | Behind the sidebar and the canvas. The sidebar sits directly on it. |
| `--background` | `bg-background` | The content canvas |
| `--tray` | `bg-tray` | A container that holds Cards: Frame trays, `CollapsibleGroup` |
| `--card` | `bg-card` | Cards, Frame panels, tables in a surface |
| `--popover` | `bg-popover` | Menus, popovers, dialogs |
| `--muted` | `bg-muted` | Small fills and hover states only: tab lists, chips, skeletons, icon tiles |

- Never use `bg-muted` for a container of Cards; it is lighter than `bg-card`, so the Cards would
  read as holes. Use `bg-tray`.
- Where the fill step already separates two surfaces, do not add a second border for emphasis.
- Yellow (`warning`) means "needs attention" and nothing else. Favourites, decorative icons and
  neutral group headings are not yellow.
- In the light theme `--shell` and `--tray` equal `--muted`.

### Shell

The sidebar is the PanelUI `NavRail` used as it ships: a floating, rounded panel inset 8px from the
window, filled one step above the canvas. The content sits directly on the canvas (`bg-background`);
there is no second container. There is no top bar: a top-level page starts with its `PageHeading`,
and nested pages show a breadcrumb above it. Search lives in the sidebar (⌘K) only. The current
navigation item is marked by its tint alone.

In the dark theme raised surfaces (Card, Frame, Kpi, `CollapsibleGroup`) have no border: the fill
step separates them from what they sit on. A Card inside a Card keeps its border, and a Card that
colours its own edge must set the `dark:` colour too, otherwise the dark rule wins.

Rules:

- **Choose grouping in the context of the whole page.** Open sections suit forms and page-level
  groups. Compact standalone summaries beside a chart or other Cards usually need a Card of their
  own. Do not remove a container solely to reduce the number of Frames.
- **Never put a surface in a surface.** No Alert, bordered box or tinted panel inside a Card or Frame.
  Inside a surface use type, whitespace, Badge and icons. Workbench is the exception: its canvas holds
  surfaces, because that is what a canvas is for.
- **Size surfaces to their content.** Short status summaries use one compact Card body with a small
  label, readable status and aligned action. Do not add a separate header or repeat padding inside
  `Card.Content`; use the component’s size and part padding. Reserve large type for key metrics.
- Form wrappers must preserve validation semantics: errors are announced, invalid submission focuses
  the first failing control, required selects validate an empty value, and dialog actions retain their
  form owner when the body scrolls.
- **Every block has a visible boundary.** A reader must be able to tell where one block ends and the
  next begins without reading the headings. Whitespace alone is enough only between surfaces that
  draw their own edge. Concretely:
  - Data that would float on the canvas (a toolbar with a table, a filtered list, a paged list) is a
    `Section surface`: one Card, heading inside, controls and table in its content.
  - Open sections stacked in one column are ruled off by `Section` itself. Do not add or remove that
    rule by hand; pass `divided={false}` only when each section already ends in its own Card.
  - Blocks that sit side by side or in one column of a dashboard use the **same kind of container**.
    A canvas heading over loose Cards next to Frames reads as unfinished; make it a Frame or a Card
    with the heading inside.
  - A collapsible group of Cards or a Table is a `CollapsibleGroup` (`components/ui/collapsible`):
    a bordered tray whose header is the trigger. It is the second sanctioned place, after Workbench,
    where surfaces sit inside a container. Never a bare trigger line between Cards. A single group
    is a tray too, so the page keeps one shape whatever the data (it starts open).
- No divider under a tab row, header or toolbar; hairlines belong between rows of one list and
  between stacked open sections.
- **Headings sit above the surface**, in the section, unless the component owns a header slot and the
  surface stands alone (`Card.Header`).

## 2. Content shape to component

| The content is | Use | Example |
| --- | --- | --- |
| One object with a title, details and actions | `Card` | A Git source, an approval request, the deployment plan |
| Several comparable objects | Stack or grid of `Card` | Connections, templates, application cards |
| Records with the same columns | `Table` + `Pagination` | Audit trail, operations, rollback targets |
| A short digest of things that live elsewhere | `Frame` with `Frame.Row` | Needs attention, recent syncs |
| Label and value pairs | Plain `<dl>` in a section or Card | Source details, render settings |
| A number that matters | `Kpi` (with trend or progress), `Stat` over a chart | Health counts, last sync |
| A chart | Card holding `Stat` and the chart | Sync activity |
| Exploring or editing something large | `Workbench` (+ `Canvas`, `Flow`) | Resources, plan review |
| Something wrong or needing a decision | `Alert`, or the attention digest (section 4) | Plan expired |
| Short feedback after an action | `toast` | Saved |
| A focused task | `Dialog`, or `Sheet` for side detail | Edit routing |
| A destructive confirmation | `AlertDialog` | Delete application |
| A multi-step task | `Steps` with `Field` | New application |

### Frame is for digests only

Use Frame only when **all** are true:

1. The rows are short, one or two lines, and the same kind of thing.
2. It summarises or links to something that lives elsewhere.
3. There is at most **one Frame per screen**. A dashboard may hold several, one per digest, so its
   columns share one container style.

Otherwise pick Card, Table or a plain section. A Frame holding a single row, a form, a chart or
key-value pairs is always wrong.

## 3. Choosing a control

| Need | Use | Not |
| --- | --- | --- |
| Switch between views of one thing | `Tabs` (`segmented`) | Chips |
| Narrow a list | `Chip.Group` | Tabs |
| Pick from more than about 5 options | `Select` / `FormSelect` | Chips |
| Complex query filters | ReUI `Filters` (see section 6) | |
| Setting applied at once | `Switch` | Checkbox |
| Setting applied on save | `Checkbox` | Switch |
| Several row actions | `Menu` (`ActionMenu`) | A row of buttons |
| A static state or label | `Badge` | Chip |

## 4. Status and errors

- Tone follows severity everywhere: `destructive` failed or being deleted, `warning` blocked or
  drifting, `info` neutral notes, `success` done. A banner and the status badge for the same thing
  must share a tone.
- **At most one stack of problems per screen.** Two or more problems go in one digest Frame named
  "Needs attention", each row laid out the same: tinted icon, title, muted detail, actions on the
  right. The Frame itself stays neutral: severity is the toned icon and count badge in its header.
  Do not tint the tray or its border. One problem is a single `Alert`.
- `Alert` is one surface. Its actions sit at the trailing side of the message (under it on phones),
  never in a separate tinted strip. Do not rebuild that strip with custom classes.
- Several notes with no separate actions: one Alert with `Alert.Details` and `Alert.Item`.
- Inside a surface, state is a tinted icon and text, not a nested Alert. The local
  `Alert variant="inline"` provides accessible shared message parts without a border, tray or background.
  Shared notice wrappers expose `inline` for this case.
- **One signal per record.** A Card or row that shows several states (delivery, runtime, paused)
  colours only the worst badge; the others are neutral `secondary` badges. Do not repeat the state
  as coloured text under the badges; the reason line is plain text led by an alert icon in the
  signal's tone.
- **Attention is never neutral.** A record counted as "needs attention" shows at least one coloured
  signal. A state such as runtime "Unknown" is `warning` when it is the reason the record is on
  the attention list, not a grey badge. For applications this is one function,
  `applicationCardStatus` (`lib/application-card-status.ts`), with a test over every combination of
  delivery state, runtime state and flags. Change tones there, not in a component, and never by
  matching substrings of a status string.
- **One record style.** Do not dim, dash or shrink a Card to show state. Its edge may carry the
  card's signal colour: `destructive` for a record that is down, `warning` for one waiting on a
  decision from a person (a deletion review). Everything else (paused, unknown, informational) is a
  badge, not a card style.
- **A shared cause explains, it never hides.** When a group's records are all affected by one thing
  (an unreachable cluster), the group header states it in detail and each record names it in its
  reason line. The records keep their own state colour: they are down, and the page must read that way.
- An error must read as an error: red tone on the icon and title or the Alert edge, not only on a
  small badge. A large tinted background is not required and usually looks muddy on dark themes.

## 5. Actions

- One `default` (filled) button per view, for the most likely next step.
- `outline` for secondary, `ghost` only inside toolbars and menus, `destructive` for deletion.
- No text links as actions. Inline links are for running prose only.
- External links carry an up-right icon, not a typed arrow.
- Actions for a whole surface go in its header slot. If they do not fit, collapse into a `Menu`.
- On a tinted strip, an outline button needs a solid `bg-background`. Do not use a filled primary there.

## 6. Where PanelUI has no component

Keep these, and restyle only through tokens:

| Need | Keep |
| --- | --- |
| Advanced filter builder | ReUI `Filters` |
| Collapsible detail | `Disclosure` (`components/ui/collapsible`) |
| Loading placeholders | `Skeleton` (`components/ui/skeleton`) |
| Vertical event timeline | ReUI `Timeline`. PanelUI's `timeline` is a media editor. |
| Calendar date selection inside advanced filters | `Calendar` (`components/ui/calendar`, React Day Picker); PanelUI has no date picker. |
| File selection and drag/drop | Shared Input/Button with native file capability and Card drop target; PanelUI has no uploader. |
| Manifest syntax tokens | The existing Shiki highlighter utility; PanelUI has no code highlighter. This is data, not a ReUI visual surface. |

Sync activity uses PanelUI `BarChart` with `@tanstack/charts`. The retained filter builder may keep
its structural popover/cascader dependencies; its buttons, inputs and badges use PanelUI. The remaining Tooltip and Spinner compatibility
entry points in `components/ui` re-export PanelUI primitives; unused legacy controls were removed.

Anything else missing
is a gap to raise, not a reason to hand-build a control.

## 7. Page blueprints

**List** (Applications, Approvals, Audit): `PageHeading`; one filter row with search, then Chips or
Filters; Cards or a Table; Pagination; empty state with no container.

**Detail** (Application): `PageHeading` with status; attention digest if needed; `Tabs`; each tab is
sections on the canvas.

**Dashboard** (Home): a Kpi row; attention digest; further digests (waiting for review, recent
syncs) as Frames in the side column; one chart.

**Settings** (Instance, Workspace, Connections, Application settings): `SettingsLayout`; sections
with the heading on the canvas; rows in one Card; records as a stack of Cards;
`SettingsDangerZone` last.

**Workbench** (Resources, Plan review): toolbar in `Workbench.Bar`, sidebar for navigation, notices
on the canvas, inspector for detail. Fullscreen targets the Workbench only. On phones, resource navigation opens in a Dialog;
do not force a desktop sidebar to remain beside the canvas.

**Wizard** (New application, Publish): `Steps`; each step is a `Section surface` (one Card: title and
description in its header, `Field`s in its content), so the step is a visible block. Selectable option
cards inside it (package sources, optional services) are allowed;
Back and Next at the bottom. Use `WizardSteps` for application/group flows so labels retain readable
widths on phones and the current step scrolls into view within the step rail.

## 8. Shared wrappers

Screens use these so the rules live in one place:

| Wrapper | Becomes |
| --- | --- |
| `Section` (`components/section.tsx`) | Open: heading on the canvas, ruled off from a preceding open section. `surface`: one Card with the heading in its header |
| `CollapsibleGroup` (`components/ui/collapsible.tsx`) | Bordered tray with a trigger header, holding a group's Cards or Table |
| `Panel` | Removed. Choose `Section`, `Card`, `Table` or `Workbench` explicitly |
| `SettingsSection`, `SettingsPanel`, `SettingsGroup` | `Section` + `Card`. Not Frame |
| `SettingsDangerZone` | `Section` + Card with a destructive border |
| `ConnectionRow` | A `Card` per record |
| `AttentionRow` | Frame rows of the attention digest, the reference Frame use |

## 9. Migration

Shared controls, form fields, dialogs, confirmations, sheets, tooltips, menus, badges and wizard
navigation use PanelUI. New registry additions include Avatar, Pagination, BarChart, ChoiceCards,
ButtonGroup, Toolbar, RadioGroup, Inspector and NotFound. The generic `Panel` wrapper has been removed.

The migration remains under visual review. See [panelui-migration.md](panelui-migration.md) for
verified screens, remaining checks and retained library gaps. An import audit is not a visual sign-off.

## 10. Lessons from the application overview review (2026-11-09)

These user corrections guide the rest of the migration. Reuse the corrected patterns in
`components/application/overview-tab.tsx`; review the entire composition, not just the component.

| Rejected result | Required pattern | Reason |
| --- | --- | --- |
| Frame replaced by Card while keeping a separate large header, oversized status and padded inner wrapper | Compact status Card: `size="sm"`, one `Card.Content`, small label, normal-sized status, action aligned alongside | A library swap alone preserves poor spacing and hierarchy. Padding belongs to the Card parts, once. |
| Floating Source heading, action on a separate line, loose metadata beside the sync chart | Compact Source Card: title and action together in `Card.Header`, aligned `<dl>` in `Card.Content` | A standalone object needs clear grouping and alignment with neighbouring surfaces. An open section looked unfinished here. |
| “Source details” action that navigated to Activity | “Configure” action that opens Settings | Action labels must describe their destination. |
| “Source: yaml” | “Renderer: YAML”, with Git ownership shown separately when applicable | Metadata labels must match the data. |

Apply these lessons on every screen:

- Select the surface, density, type scale and action placement together. Do not perform mechanical
  Frame-to-Card or Card-to-Section replacements.
- Use open Sections for page structure; use Cards to group standalone objects. Both are valid.
- Match neighbouring header alignment and spacing. Do not stretch short summaries merely to fill space.
- Keep a surface action beside its heading when room permits; wrap deliberately on narrow screens.
- Wrap multiline Button content in one block (as FeatureCard does); separate title/description spans
  otherwise become flex items and break the vertical hierarchy. Responsive form grids start with an
  explicit `grid-cols-1` so long content cannot set an oversized implicit column.
- Inspect the composed page in the collaborative browser after migration, including a narrow layout.
  Passing typecheck and lint does not establish visual quality.
- Long action labels must wrap within their container and grow vertically, including suggested
  prompts. A page with hidden horizontal overflow can still clip controls; inspect their bounds.
- Workbench fullscreen must fill its fullscreen parent, including the inner Workbench. Follow
  browser fullscreenchange state for sizing and the enter/exit label; verify actual bounds.
- Code editors use Workbench with fixed-size Textareas that fill their panes. Keep configuration
  fields outside the editor surface. Comparisons use a unified layout on phones and side-by-side
  columns only where readable; do not require desktop-width columns in a narrow viewport.
- Confirmation actions keep their complete labels. AlertDialog stacks actions on phones and lets
  longer labels wrap; never clip a destructive action to preserve a fixed button height.
- Keep wizard navigation and final submission as distinct mounted controls. Cancel the Next click
  default action before changing steps; a button changing to `type="submit"` during that click can
  submit before the user sees Review. Verify navigation with a local submission counter.
- Record further user design corrections here and update conflicting earlier rules immediately.

### Separation review (2026-11-10)

The user rejected screens where blocks were not clearly bounded. These apply frontend-wide:

| Rejected result | Required pattern |
| --- | --- |
| Alert with its button in a tinted strip under the message | Actions inside the Alert, trailing side. Fixed in `Alert`; do not reintroduce the strip. |
| "Needs attention" Frame with a warning- or destructive-tinted tray | Neutral Frame; toned icon and count badge in the header. |
| "Waiting for review" as a canvas heading over loose Cards, beside Frames | Same container as its neighbours: a Frame with link rows. |
| Collapsible cluster group as a bare heading line above application Cards | `CollapsibleGroup` tray, so several groups read as separate containers. |
| "Recent operations" and "Rollback targets" as open sections whose tables ran into each other | `Section surface` for each. |

| Application cards where every card carried two or three coloured badges, yellow text, and one of three card styles (dashed and dimmed, yellow edge, normal) | One card style, one coloured badge, a coloured edge for the card signal. See `applicationCardStatus`. |
| Cards of applications blocked by an unreachable cluster all turned grey "because the cluster is the shared cause", so a page of down applications looked healthy | Blocked applications keep their state colour (red when down) and a reason line naming the cluster. |
| Truncated card titles | Titles clamp to two lines. |
| One group on the canvas, several in trays: the page changed shape with the data | Every group is a `CollapsibleGroup` tray, a single one included. |
| "Needs attention 9" beside "All apps 9" | Status chips use the group headers' split: Down, Waiting for review, To check, In sync, Paused. "Needs attention" stays reachable by link and saved view. |
| An outage said four times (alert, header, card reason, card button) | One alert above the groups, in every grouping, carries "Check cluster"; blocked cards keep the reason line and lose the button. |
| Status chips taking three rows on a phone | Below `sm`, search plus one "Filters" button that opens a bottom `Sheet` with the chips, favourites and advanced filter. |
| List rows only openable one at a time | Rows have checkboxes; a floating bar offers Refresh plans, Add label and Auto-sync (resume asks first). Rows the reader may not change are skipped and counted in the result. |

### Application detail review (2026-11-10)

| Found | Now |
| --- | --- |
| Deployment schedule as loose rows at the bottom of Overview | Configuration lives in the Settings tab, as a settings section. Overview shows state, not settings. |
| Right column of Overview ended after one Card while the left ran on | Balance the columns: Source and Recent activity right, the chart left. |
| Plan status ("applied", "stale") as an icon and text floating on the canvas | `Alert` with its actions. A status message with actions is always an Alert. |
| "Refresh plan" in the page header and again inside the tab | An action appears once per view. |
| Empty state with its only action in the far corner of the section header | The action sits in the empty state; the section is a `surface`. |
| Header action filled on every tab, beside a filled action in the tab | The page-header action is filled only on tabs where it is the next step (Overview, Activity). |
| Cards on one page in two sizes (18px and 16px titles) | Cards inside a tab use `size="sm"`. |
| A server component reading `Card.Content` from the client module crashed the page | A file that uses compound PanelUI parts (`Card.Content`, `Frame.Row`) is a client component. |

### Connections and settings review (2026-11-10)

| Found | Now |
| --- | --- |
| A Git source the dashboard reports as unreachable looked neutral on the page where it is fixed | A record shows the state other screens report about it: destructive badge and the reason. Derive it from the same function (`gitSourceHealth`), never a second rule. |
| "Discovery paused" as a grey outline badge | Paused is `warning`, failed is `destructive`, on is `success`, each with a dot. |
| Workspace name in the breadcrumb, the sidebar switcher and a scope badge | `ScopeBadge` only where the scope is not already on screen (instance settings). |
| Settings navigation group named "Delivery", like the main sidebar group | Group labels are unique across both navigations. |
| Content column narrower than the page heading, so the header action overhung the cards | With a settings navigation the content fills to the heading's right edge. |
| Count badge alone in the far corner of a section heading | A count sits directly after the title. |
| Properties ("Private to this workspace", "No credential") as badges beside the name | Badges are for state. Properties go in the muted line under the name. |

### Manual publishing wizard review (2026-11-10)

| Found | Now |
| --- | --- |
| Filled red "Discard draft" beside the filled blue "Next" | Discarding a draft is not a deletion: outline, small. One filled button per view. |
| "Publishing in workspace X. Switch it with the workspace menu…" as a sentence on the canvas | `WorkspaceNote` is an outline badge in the page header; the hint is its tooltip. |
| Back far left, Next far right, Back disabled and almost invisible on step 1 | Both on the right, Back only from step 2. |
| Wizard steps as bare fields on the canvas, "just input and text on a background" | Each step is a `Section surface`. The earlier rule "a step is a section, not a Card" contradicted "every block has a boundary" and is replaced. |
| Draft notice floating between the step and the buttons | `PublishingDraftNotice` renders as a compact Card. |
| Notes between fields indented 4 px and one size above the field hints | Aligned with the fields. A note that states a security or storage fact is an `Alert variant="plain"`. |

### Application detail second review (2026-11-13)

| Found | Now |
| --- | --- |
| Runtime "Degraded" only as a small header badge while plan, KPIs and chart all read healthy | A "Runtime degraded" row in Needs attention (`runtimeProblem`), and a Runtime health Kpi whose bar takes the worst workload signal (`summarizeRuntime`). Observation coverage ("4 of 4 observed") is drawn neutral, never green. |
| Green "Synced" beside red "Runtime Degraded" | Sync badge turns neutral when the runtime is worse. One coloured signal. |
| Sync policy as a large KPI | Settings are not metrics: sync policy is a row in the Source card. |
| "Refresh plan" filled even when nothing is outdated, and again in the plan card | The header action is filled only when the plan is missing, stale or expired; the plan card only leads to the plan. |
| Profile update Alert with its own border inside the Source Card | Alerts inside a surface use `variant="inline"` with actions in `Alert.Actions`. |
| History rows that repeat "Sync completed successfully" | Rows carry revision, who or what started the run, and duration (`operationContext`). |
| Status and type filters as Selects with 3–5 options | Single-choice `Chip.Group`; pressing the active chip clears it. |
| Application Settings tab as one long column | `SettingsNav` with one section per `?section=`; adding an exclusion is a Dialog, the rules a Table. |
| Background plan completion switched the tab | A toast with a "Review" action; only user-started work changes the tab. |

New PanelUI components in use: `Breadcrumb` (via `PageBreadcrumb`, every nested page), `ScrollHeader`
(application detail: trail and actions in the bar, title and status in the large block; actions move
under the title below `sm`), `Combobox` (plan picker, custom rollback revision), `RollingNumber`
(live Kpi counts). A heatmap was considered for sync history and rejected: a one-colour ramp cannot
show failures.

### Overview dashboard review (2026-11-13)

| Found | Now |
| --- | --- |
| Medium severity purple, low severity green | Critical and high red, medium and low yellow. Attention is never neutral or green. |
| "Healthy" and "Needs attention" as two KPIs of the same total | One Applications KPI with a split bar (healthy, needs attention, other). |
| Syncs KPI with a sparkline repeating the chart below | "Failed syncs", the number a person acts on. |
| Workspace name as the page title, beside the sidebar switcher | Title "Overview"; the workspace name leads the description. |
| Connections as a Card that only said "All 5 reachable" | A status badge in the page heading; problems keep the Alert. |
| Attention rows that only linked to the application | `AttentionRow` (shared with the application page) with an action that opens the tab holding the fix. |
| Chart and digest behind tabs, chart 400px tall with one bar | Side by side; the chart is shorter and defaults to 7 days. |

The Delivery section shows deploy frequency, failure rate and time to restore against the previous
period (`/workspaces/{id}/delivery-metrics`). Lead time is not shown: it needs commit timestamps.

### Applications list review (2026-11-13)

| Found | Now |
| --- | --- |
| Two to four bare badges per card ("Degraded", "Unknown") with nothing saying which is which | One headline badge (`CardStatus.primary`), self-describing via `badgeLabel` ("Runtime degraded", "Delivery out of sync"); the rest behind "+N" with a tooltip. |
| Title Case badges from CSS `capitalize` | Sentence case, as everywhere else. |
| Cluster name truncated to "DEMO ONLY - u…" beside the namespace | The cluster has a line of its own; hidden when the list is grouped by cluster. |
| Group bar yellow "to check" over red cards | The header takes its worst card's colour and says "N down, N waiting for review, N to check". |
| Every card in a group carrying the same red edge for the same unreachable cluster | The group header names the shared cause once; when it covers the whole group, cards drop the edge. |
| The grouping label repeated on every card of its group | Hidden. |
| A favourite star on every card | Shown on hover or focus, or when already a favourite (always on touch). |
| "Checked 33 min ago" as the only time | Recent runs as dots and "Deployed …", from the workspace sync history; the fix is one button away (`attentionTab`). |
| "Cluster unreachable" repeated on every blocked card | The card says "Blocked by cluster" (plus the state, such as "· agent offline", when the cluster is not the group) and links to "Check cluster", not to the application's activity. |
| "Checked 23 d ago" in grey on a paused application | Paused applications are not polled, so the footer says "Not checked while paused · last 23 d ago"; the paused badge carries the colour. Only an application that should be polled turns yellow ("Checks stalled"). |
| Stretched cards with a gap above a floating footer | The footer sits behind a divider at the bottom of the card. |

"No container" and "whitespace separates first" were over-applied during the migration. When in
doubt, a block gets a boundary.

## 11. Review checklist

- [ ] Each surface was chosen from the table in section 2.
- [ ] No surface inside a surface (Workbench and `CollapsibleGroup` excepted), and no Frame outside the digest rule.
- [ ] Every block has a visible boundary; neighbouring blocks use the same kind of container.
- [ ] One filled button per view, no text-link actions.
- [ ] Banner and badge tones agree.
- [ ] Labels, keyboard, focus and disabled states work; typecheck and lint pass; seen in the browser.
