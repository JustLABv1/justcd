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

The sidebar has no card, border or shadow; only the canvas is raised. There is no top bar: a
top-level page starts with its `PageHeading`, and nested pages show a breadcrumb above it. Search
lives in the sidebar (⌘K) only. The current navigation item is marked by its tint alone.

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
    where surfaces sit inside a container. Never a bare trigger line between Cards. With a single
    group there is nothing to tell apart: show its header on the canvas, no tray, not collapsible.
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
- **One record style.** Do not dim, dash or tint a Card to show state. A coloured Card edge
  (`border-warning/50`) is reserved for records waiting on a decision from a person, such as a
  deletion review. Paused, degraded or blocked are badges, not card styles.
- **Say a shared cause once.** When a group's records are all affected by one thing (an unreachable
  cluster), the group header states it. The records carry a neutral "Blocked by cluster" badge and
  no colour of their own, because their state is a consequence, not a separate problem.
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

**Wizard** (New application, Publish): `Steps`; each step is a section of `Field`s, not a Card;
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

| Application cards where every card carried two or three coloured badges, yellow text, and one of three card styles (dashed and dimmed, yellow edge, normal) | One card style, one coloured badge, coloured edge only for a pending decision, shared cluster outage stated in the group header. See `ApplicationCard` and `statusBadges`. |
| Truncated card titles and a tray around a single group | Titles clamp to two lines. `CollapsibleGroup` only when there are two or more groups; a single group keeps its header on the canvas. |

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

"No container" and "whitespace separates first" were over-applied during the migration. When in
doubt, a block gets a boundary.

## 11. Review checklist

- [ ] Each surface was chosen from the table in section 2.
- [ ] No surface inside a surface (Workbench and `CollapsibleGroup` excepted), and no Frame outside the digest rule.
- [ ] Every block has a visible boundary; neighbouring blocks use the same kind of container.
- [ ] One filled button per view, no text-link actions.
- [ ] Banner and badge tones agree.
- [ ] Labels, keyboard, focus and disabled states work; typecheck and lint pass; seen in the browser.
