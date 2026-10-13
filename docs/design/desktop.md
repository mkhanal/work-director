# Work Director desktop design

One design for every desktop app: SwiftUI on macOS, GTK4 with libadwaita on
Linux, WinUI 3 later. Each app renders it with its platform's own controls and
fonts; the measures, the hierarchy and the conversation model are the same
everywhere. Views are computed in Go (`wd serve --stdio`); an app only renders
and confirms writes.

## Principles

1. **One conversation.** A goal reads top to bottom like a chat: what you asked,
   the plan, what the supervisor and each agent said and did, what waits on you.
   Everything else (record, claims, events) is one step away, never in the way.
2. **Hierarchy by type and space, not boxes.** Agents' words sit flush on the
   page. A box means something: your words, a question waiting on you, an
   answer the director gave, a report. Nothing else is boxed.
3. **Who said it is always visible.** Every turn opens with its speaker: You,
   Supervisor, a task, Director. A glance up the page finds your own messages.
4. **Calm by default, loud when it needs you.** One accent colour. Orange only
   for what waits on you; green only for what the director settled; red only
   for failure.
5. **Nothing moves under the reader.** The conversation follows new content
   while you are at the bottom and holds still while you read.

## Tokens

### Space (4-point grid)

| Token | Value | Use |
|---|---|---|
| `space.1` | 4 | icon to label |
| `space.2` | 8 | inside a row; between a meta line and its body |
| `space.3` | 12 | inside a card; between rows in a list |
| `space.4` | 16 | card padding; between paragraphs of a turn |
| `space.5` | 24 | between turns |
| `space.6` | 32 | page gutters; between a turn and a section change |
| `space.7` | 48 | above the first turn; around empty states |

### Type

macOS uses SF Pro; Linux uses the system UI font (Cantarell or Inter); code is
SF Mono or the system monospace.

| Token | Size / line | Weight | Use |
|---|---|---|---|
| `type.hero` | 26 / 32 | semibold | the new-goal page's question |
| `type.display` | 22 / 28 | semibold | page title (goal name) |
| `type.title` | 16 / 22 | semibold | section titles, markdown `#` |
| `type.headline` | 14 / 20 | semibold | a card's title, an emphasised row |
| `type.body` | 14 / 22 | regular / medium | everything said in the conversation; a question's text in medium |
| `type.callout` | 13 / 18 | regular / medium | rows, chips, buttons, tool rows |
| `type.callout` strong | 13 / 18 | semibold | sidebar product, agent names, the selected tab |
| `type.caption` | 12 / 16 | regular | secondary lines, notes |
| `type.meta` | 12 / 16 | medium | speaker, time, counts |
| `type.small` | 11 / 14 | regular | hints under the composer, sidebar row details |
| `type.micro` | 11 / 14 | semibold, +0.4 tracking, uppercase | sidebar section headers |
| `type.tag` | 10.5 / 13 | semibold | pills and tags: verdicts, kinds, Recommended |
| `type.code` | 12.5 / 19 | regular | code blocks; 12 for tool output and file chips; 11 for command hints |

Every text style in an app comes from this table; nothing sets a size of its
own. Symbols have their own sizes — 9 (disclosure chevrons), 11.5 (tab
icons), 13 (composer controls), 14 (toolbar), 15 (prompt marks), 17 (turn
avatars) — and never borrow a text size.

Toolbar actions are grouped by what they do, with space between groups:
the next gate; Pause or Resume with Actions; Your messages (in the
conversation) with Open in Editor; then the icon-only Command and Agents
toggles last. New Goal lives at the top of the sidebar, not in the toolbar.

### Shape

| Token | Value | Use |
|---|---|---|
| `radius.chip` | 6 | chips, tags, pills |
| `radius.card` | 10 | cards, question, answer, report |
| `radius.bubble` | 14 | your messages, the composer |
| `hairline` | 1 px, `color.separator` | borders, dividers |

### Colour and components (from `docs/design/goal-tree/`)

The canvas fixes every colour and component measure. Light values are the
canvas's; dark keeps the structure and takes the platform's dark equivalent,
badges as the same hue at 18 % over the dark page with the foreground
lightened to 4.5:1. macOS: `clients/macos/Sources/WorkDirector/Design.swift`.

| Token | Light | Dark |
|---|---|---|
| page | `#ffffff` | `#1e1e1e` |
| sidebar | `#f2f2f6`, width 268, hairline right, padding 14 / 10 | `#26262a` |
| sidebar selected row | `#dcdce3` | `#3a3a40` |
| search field | `#e6e6ec` | `#323237` |
| hairline | `#e3e3e8` | `#38383d` |
| inner separator, selected tab, progress track | `#ececf1` | `#2e2e33` |
| record row separator | `#f0f0f3` | `#2a2a2f` |
| button border | `#d6d6dc` | `#48484e` |
| text / text2 / text3 | `#1d1d1f` / `#55555b` / `#6c6c72` | `#f2f2f7` / `#b4b4ba` / `#8e8e95` |
| step text | `#3a3a3f` | `#d0d0d6` |
| accent / links | `#0a64d6` / `#0a5cc7` | `#3d8bf0` / `#5aa0ff` |
| green (done) / orange (needs you) / grey (waiting, superseded) | `#1f9d55` / `#e8820c` / `#9a9aa0` | `#34c46e` / `#f0943a` / `#7c7c82` |
| badge attention | `#fff1e0` / `#9a3b00` | `#42301b` / `#ffb067` |
| badge settled | `#e6f6ec` / `#11692f` | `#1e3528` / `#6fdc95` |
| badge live | `#e8effc` / `#0a4fb0` | `#1a2b3f` / `#8cb8ff` |
| badge waiting | `#efeff3` / `#55555b` | `#343435` / `#c2c2c8` |
| badge decision | `#f1ebfb` / `#5b2aa8` | `#292037` / `#c4a6ff` |
| question card border / fill / bar | `#f3c99a` / `#fff8f0` / `#e8820c` | `#6b4a22` / `#2b231a` / `#f0943a` |
| sub-goal row waiting on you | `#fffaf4` | `#2a2219` |
| your bubble fill / border | `#e5effc` / `#bcd3f3` | `#1c2d45` / `#2f4a70` |
| sub-goal tag fill | `#f2f2f6` | `#2c2c31` |
| director answered fill / border | `#f0faf3` / `#cdebd7` | `#1b2a20` / `#2b4a35` |
| filter chip selected fill / text | `#1d1d1f` / `#ffffff` | `#f2f2f7` / `#1d1d1f` |
| failure | `#d70015` | `#ff6961` |

| Component | Measure |
|---|---|
| micro (section header) | 11/14, semibold, +0.4 tracking, uppercase, text3 |
| sidebar row | padding 8 / 10, radius 10; dot 8; title 13/18 medium; meta 12/16 text3 |
| toolbar | 52 high, hairline below; buttons 13/18, padding 5 / 12, radius 7, button border; primary filled accent |
| header | padding 20 / 32; breadcrumb 12/16 text3; title 22/28 semibold; tabs 13, padding 6 / 10, radius 7, selected fill semibold |
| body | padding 20 / 32, 20 between sections, hairline above |
| card | hairline border, radius 10, padding 14 / 16; label 12/16 medium text3 |
| badge | 10.5/13 semibold, padding 2 / 6, radius 6 |
| question card | 3 px leading bar, radius 10; option rows 7 / 10, radius 8, 13/18 medium; chosen row page-filled with the question border |
| state icon (20) | done: filled green circle, white check · needs you: orange ring + centre dot · working: accent ring + quarter fill · waiting: grey dashed ring · superseded: grey ring + bar |
| sub-goal row | padding 14 / 16, title 14 medium, meta 12 text3, badge trailing |
| steps | 13/20; ✓ green done, ● accent semibold doing, ○ text3 todo; dropped struck through |
| progress bar | 120 × 4, radius 2, track inner, fill green |
| filter chip | 12/16, padding 4 / 10, radius 14, hairline |
| your bubble | radius 14, padding 10 / 14 |
| sub-goal tag | 11/14 medium, padding 2 / 7, radius 6, hairline |
| event line | 12 text3 centred between hairlines |
| record row | columns 52 · 96 · flexible · 220 · 90, gap 12, padding 9 / 0 |
| record summary | five cards, number 20/24 semibold, label 12 text3 |

## The conversation

The centre pane is one scroll view with a `space.6` gutter on each side and
`space.7` above the first turn, filling the window's width.

### A turn

```
[avatar 24]  Speaker · 10:42 · detail                          (type.meta, text2)
             Body…                                               (type.body)
```

- Avatar column 24 px with `space.3` to the body; the body never sits under
  the avatar.
- Speakers and their marks: **You** (filled person, accent), **Supervisor**
  (shield, text2), a **task** (its state symbol, coloured by state, the task
  title as the name, a link to its conversation), **Director** (checkmark seal,
  green).
- Consecutive turns by the same speaker within two minutes merge: the second
  drops its avatar and meta line and keeps `space.4` above.
- `space.5` between turns.

### What each kind of turn looks like

| Turn | Rendering |
|---|---|
| Your ask, message, decision, answer | right-aligned bubble, at most 80% width, `radius.bubble`, accent fill; meta line above it, right-aligned; long text folds after 8 lines with Show all |
| Sending / queued / refused | your bubble with its status in the meta line; refused adds Retry and Discard links in red under it |
| Supervisor reply | flush turn; its words as markdown; then each action as an agent chip (task state, title, what it was told, chevron), `space.2` apart; refused lines in red `type.callout` |
| Agent text | flush turn, markdown |
| Thinking | one-line disclosure "Thinking" in text3; open while it is the newest step |
| Tool calls | grouped: one line per call, `type.callout` text2, tool name in medium weight, summary truncated; results on disclosure in `color.sunken` code blocks; a run of more than 3 collapses to "N tool calls" |
| Report | card in `color.raised`, title row "Report" + verdict pill, markdown body folded after 12 lines |
| Question waiting on you | attention card (see Question) placed last, just above the composer |
| A question with its answer | the asker's turn holds the question and its choices as read-only chips; the answer follows as a Director turn (settled card) or your bubble |
| Look, state change, stopped | event line: centred `type.meta` text3 between two hairlines, e.g. `── Collector moved to review · 10:42 ──` |

### Markdown

Paragraphs `space.3` apart; headings `type.title` with `space.4` above;
bullets with an 8 px marker column, hanging indent; code blocks `type.code`
in `color.sunken`, `radius.card`, 12 px padding, horizontally scrollable; inline
code in `color.sunken` with 3 px horizontal padding.

### Question (waiting on you)

```
┃ The supervisor asks you                                    (meta, attention)
┃ 1 of 3 · Tag v1.0.0 on e328cf57?                           (type.body, medium)
┃  ◉ Yes, e328cf57          Recommended                      (option rows)
┃  ○ No, 55b857f2
┃  ○ Something else… [text field]
┃                                          [Skip]  [Answer]  (buttons)
```

- 3 px attention bar on the leading edge, attention fill, `radius.card`,
  `space.4` padding.
- One item at a time when there are several, with "1 of 3" and Back/Next; a
  single item shows alone. The answer button sends all items at once.
- Options are full-width rows (radio for single, checkbox for multi), 36 px
  tall, label in `type.callout` medium, description in text2 below. A
  "(recommended)" suffix renders as a trailing Recommended tag instead.
- The last row is always free text.

### Composer

- Sits below the scroll view (never over it), `space.6` gutters, `space.3`
  above, `space.5` below.
- `radius.bubble` card in `color.raised`, hairline border, soft shadow
  (y 2, blur 10, 7% black). Padding 12 / 14.
- Field: `type.body`, 1 to 10 lines, then scrolls.
- Bottom row inside the card, `space.2` below the field: context chips
  (runner, model, folder) in `type.meta` text2 on the left; on the right the
  remember toggle, Stop (while something works) and Send: a 28 px accent circle
  with an up arrow, dimmed while empty.
- Under the card, right-aligned `type.micro`-sized hint: the send chord and
  "Esc Esc stops".

### Following

At the bottom (within 48 px) new content keeps the newest in view; scrolled
up, the view holds still. A 34 px round "jump to latest" button floats
bottom-right, `space.6` from the edge, while away from the bottom. Sending
jumps to the bottom.

## Screens

### Window

Three panes: sidebar (280, 240–380), conversation, agents inspector (340,
260–480, toggled). Title bar is unified with the toolbar; no title text in the
toolbar; the conversation's own header carries the name.

### Sidebar

As the canvas (`goal-tree/main`, `subgoal`): 268 wide, `sidebar` fill, a
hairline on its right, padding 14 / 10.

- Top: the product's name in 13 semibold (a menu: All products, each
  product, Add Product…) and a "+ New Goal" button beside it (⌘N); under it a
  rounded "Search goals" field.
- Bands with micro headers: **Needs you**, **Working**, **Completed**; then a
  folded **Let go** (dropped, abandoned, superseded) and **More** (Taste,
  Archived, Review). Top goals and standalone work only; a goal with a
  question anywhere below it is in Needs you.
- A row: 8 / 10 padding, radius 10, selected `sidebar selected`; an 8 px dot
  (orange needs you, accent working, green completed, grey waiting or let go);
  title 13/18 medium; meta 12/16 text3: `1 of 4 · [1 needs you]`, or where a
  done goal finished (`Complete PR #512`).
- With a sub-goal open, its path from the top goal shows under the top goal's
  row, behind a 2 px rule, each level a way to its page.
- Context menu: Open, Rename…, Pause/Resume, Archive.

### Goal header

At the top of the conversation, `space.7` above:

- Name in `type.display`. Double-click or Rename… makes it an inline field;
  Return saves (`wd rename <id> "<title>"`), Escape cancels.
- Under it, `space.2`: state pill, product, `3 of 5 tasks`, delivery status,
  in `type.meta` text2, separated by `·`.
- Toolbar (right): the next gate as one prominent button, Open in editor,
  Your messages, Agents.

### Goal page

A fixed top (name, meta line, tabs) over one scrolling view, so the name and
the way between views never scroll away. Tabs: **Overview**, **Conversation**,
**Tasks** (with count), **Record** (with count); Overview carries an orange dot
while something waits on you. The last tab chosen is remembered.

- **Overview** answers "where does this stand" without scrolling, in this
  order, each under a `type.micro` section title with `space.6` between:
  Needs you (every question waiting on you, answered in place: the goal's,
  each task's session question, and each task in needs-input with what it
  asked or reported); Next step (the next gate as one prominent button with
  why); Progress (done of total in `type.title`, one 8 px bar segmented by
  state — done green, working accent, review purple, blocked red, waiting
  grey, dropped faint — and a legend with counts); Working now (an agent chip
  per running session); Latest from the supervisor; What you decided; What you
  asked.
- **Conversation** opens with filter chips — Everything, You, Supervisor,
  Reports, Questions — each with its count. Everything folds each run of two
  or more updates (looks, state changes, checks, landings, messages told to a
  task) into one "N updates" line that opens; a day line ("Today",
  "Yesterday", "Monday, Oct 12") starts each day.
- **Tasks** is a table: state and title, what its agent is doing now, its
  last report's verdict, when it last moved, tokens; sortable by plan order,
  state (needs input and blocked first) or recency; a row opens the task.
- **Record** is the raw event log, newest first.

### Reports

A report reads in parts, parsed by `wd`: a header row (Report, verdict pill,
what it said with the verdict), then labelled fields in a 64 px name column:
Files as monospaced path chips that wrap, with any note under them; then each
named section (Verify, PR, Notes…) as markdown. The first two sections show;
the rest open with "Show N more".

### Toolbar

Primary actions carry their names, not just icons: the next gate, Your
messages, Pause or Resume, Actions (every write the work's state allows), Open
in Editor, Command (⌘K). Only the inspector toggle is an icon.

### Agents inspector

- Header: `type.micro` "AGENTS" + settled count and total tokens.
- One card per agent, `space.2` apart, 10 px padding, `radius.card`; the
  focused one selected.
  - Line 1: phase mark (asking orange ring, working spinner-dot, resting
    hollow, settled check) + title `type.callout` semibold + time on the right
    (`type.meta` monospaced digits).
  - Line 2: what it is doing now, one line, text2 (orange while asking).
  - Line 3: model · tokens · tools in `type.meta` text3.
  - Subagents indented 16 px under their parent.

### New goal

Centred column, at most 640 wide, `space.7` from the top: "What should get
done?" in `type.display`, product menu under it, then the composer (taller:
4 to 12 lines). Below it, goals that may be continued as a list of thread rows,
once found.

### Empty and error states

Centred, `space.7` padding, symbol at 32 px in text3, one line of `type.title`,
one of `type.callout` text2, at most one button.

## Platform mapping

| Design | macOS (SwiftUI) | Linux (GTK4 + libadwaita) |
|---|---|---|
| Window, three panes | `NavigationSplitView` + `.inspector` | `AdwOverlaySplitView` (sidebar) inside `AdwOverlaySplitView` (inspector), `AdwToolbarView` |
| Sidebar list | `List(.sidebar)` | `GtkListBox` with `.navigation-sidebar` |
| Conversation | `ScrollView` + `LazyVStack` | `GtkScrolledWindow` + `GtkListView` (factory per turn kind) |
| Bubble, cards | shapes with fills | `GtkBox` with CSS classes (`.bubble-you`, `.card`, `.attention`, `.settled`) |
| Composer | `NSTextView` field | `GtkTextView` in a `.card` |
| Question options | buttons with radio/check marks | `AdwActionRow` with `GtkCheckButton` (grouped for radio) |
| Toasts / undo | banner | `AdwToastOverlay` with Undo |
| Colours | the token table's light and dark values as dynamic `NSColor`s | libadwaita named colours (`@accent_bg_color`, `@warning_color`, `@success_color`, `@error_color`, `@card_bg_color`, `@view_bg_color`) |
