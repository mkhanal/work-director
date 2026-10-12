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
| `type.display` | 22 / 28 | semibold | page title (goal name) |
| `type.title` | 16 / 22 | semibold | section titles, markdown `#` |
| `type.body` | 14 / 22 | regular | everything said in the conversation |
| `type.callout` | 13 / 18 | regular / medium | rows, chips, buttons, tool rows |
| `type.meta` | 12 / 16 | medium | speaker, time, counts |
| `type.micro` | 11 / 14 | semibold, +0.4 tracking, uppercase | sidebar section headers |
| `type.code` | 12.5 / 19 | regular | code blocks, commands, ids |

### Shape

| Token | Value | Use |
|---|---|---|
| `radius.chip` | 6 | chips, tags, pills |
| `radius.card` | 10 | cards, question, answer, report |
| `radius.bubble` | 14 | your messages, the composer |
| `hairline` | 1 px, `color.separator` | borders, dividers |

### Colour (semantic; each app maps to its light and dark palettes)

| Token | Meaning |
|---|---|
| `color.page` | window background behind the conversation |
| `color.raised` | cards and the composer (one step above page) |
| `color.sunken` | code blocks, tool output (one step below page) |
| `color.separator` | hairlines |
| `color.text`, `.text2`, `.text3` | primary, secondary, tertiary text |
| `color.accent` | you: your bubbles (14% fill, 30% border), send, selection |
| `color.attention` | orange: waiting on you (8% fill, 45% border, 3 px leading bar) |
| `color.settled` | green: the director answered or a check passed (8% fill, 30% border) |
| `color.failure` | red: refused, failed, blocked |

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

- Top: product switcher (All products / each product) as a borderless menu
  button with the product's name in `type.callout` semibold; New Goal button
  beside it (⌘N).
- Search field, `space.2` below.
- Sections with `type.micro` headers in text3, `space.4` above each: **Needs
  you**, **Working**, **Ready**, then folded **Queued** and **Settled**, then
  **More** (Taste, Archived, Review).
- A thread row: 2 lines, 8 px vertical / 10 px horizontal padding, selection
  `radius.card`.
  - Line 1: state dot (8 px, coloured by band) + title in `type.callout`
    medium, one line.
  - Line 2 (`type.meta` text3), indented under the title: product · `3/5` ·
    `2m`.
  - Waiting on you: the dot becomes an orange ring and line 2 says what it
    asks.
- Context menu: Open, Rename…, Pause/Resume, Archive.

### Goal header

At the top of the conversation, `space.7` above:

- Name in `type.display`. Double-click or Rename… makes it an inline field;
  Return saves (`wd rename <id> "<title>"`), Escape cancels.
- Under it, `space.2`: state pill, product, `3 of 5 tasks`, delivery status,
  in `type.meta` text2, separated by `·`.
- Toolbar (right): the next gate as one prominent button, Open in editor,
  Your messages, Agents.

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
| Colours | system semantic colours + accent | libadwaita named colours (`@accent_bg_color`, `@warning_color`, `@success_color`, `@error_color`, `@card_bg_color`, `@view_bg_color`) |
