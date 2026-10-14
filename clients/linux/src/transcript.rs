//! A conversation as one stretch of text: paragraphs of styled spans, the
//! frames drawn around them (cards, bubbles, code), the folds that hide
//! them, and what a click on them does. The app puts a `Doc` into one text
//! surface character for character, so a selection there is a range here,
//! and `copied` is what the person gets for it.

use crate::conversation::{Step, StepKind, ToolCall};
use crate::copy;
use crate::ledger::WorkState;
use crate::look::{Mark, Tone};
use crate::markdown::{self, Align, Block, Table};
use crate::time;

/// Space between turns, under a turn's head, and between blocks, in pixels.
pub const TURN_GAP: i32 = 20;
pub const HEAD_GAP: i32 = 8;
pub const BLOCK_GAP: i32 = 12;
/// Where a turn's body starts: past the 24 px avatar column and its 12 px gap.
pub const BODY: i32 = 36;
/// How far a fold's content sits in from its line.
const FOLD_INDENT: i32 = 18;

#[derive(Debug, Clone, PartialEq, Default)]
pub struct Doc {
    pub paras: Vec<Para>,
    pub frames: Vec<Frame>,
    /// Whether each fold shows its content at first.
    pub folds: Vec<bool>,
    /// How many `Act::App` and `Content::Embed` indices the app supplies.
    pub acts: usize,
    pub embeds: usize,
    ctx: Context,
}

/// Where the next paragraph goes while a doc is written.
#[derive(Debug, Clone, PartialEq, Default)]
struct Context {
    indent: i32,
    right: i32,
    frames: Vec<usize>,
    folds: Vec<usize>,
    /// Frames opened with no paragraph in them yet: the next one takes their top padding.
    fresh: Vec<usize>,
}

/// One line of the surface, as it wraps.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct Para {
    pub spans: Vec<Span>,
    /// Pixels from the left edge, and from the right.
    pub indent: i32,
    pub right: i32,
    /// Lines after the first sit this much further in, so a bullet's words align.
    pub hang: i32,
    pub align: Option<Align>,
    pub above: i32,
    pub below: i32,
    /// The frames it sits in, outermost first.
    pub frames: Vec<usize>,
    /// The folds that hide it while any is closed.
    pub folds: Vec<usize>,
    /// The speaker's mark, drawn in the avatar column beside it.
    pub avatar: Option<Icon>,
    /// It starts a block: copied after a blank line.
    pub starts: bool,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Span {
    pub content: Content,
    pub style: Style,
}

#[derive(Debug, Clone, PartialEq)]
pub enum Content {
    /// Words, copied as they are. Never holds a newline.
    Text(String),
    /// Shown, never copied: a chevron, a control's label, a pill's padding.
    Ornament(String),
    /// An icon in the line; one character, copied as nothing.
    Icon(Icon),
    /// A table drawn as a grid; one character, copied as its rows.
    Table(Table),
    /// A widget the app makes, by its index; one character, copied as the text given.
    Embed(usize, String),
}

impl Content {
    /// Its characters in the surface.
    pub fn len(&self) -> usize {
        match self {
            Content::Text(t) | Content::Ornament(t) => t.chars().count(),
            Content::Icon(_) | Content::Table(_) | Content::Embed(..) => 1,
        }
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// What it puts in the surface: a picture, table or widget stands on the
    /// object replacement character.
    pub fn shown(&self) -> String {
        match self {
            Content::Text(t) | Content::Ornament(t) => t.clone(),
            Content::Icon(_) | Content::Table(_) | Content::Embed(..) => '\u{FFFC}'.to_string(),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Icon {
    State(WorkState),
    Mark(Mark),
    /// A symbolic icon from the theme, in an ink.
    Named(&'static str, Ink),
    /// Something still running.
    Spinner,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct Style {
    pub size: Size,
    pub weight: Weight,
    pub italic: bool,
    pub ink: Ink,
    /// Inline code: monospace on a faint fill.
    pub code: bool,
    pub act: Option<Act>,
    pub show: Show,
    /// The inline frame it is drawn in, a pill.
    pub frame: Option<usize>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Size {
    Title,
    Body,
    Callout,
    Meta,
    Small,
    Code,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Weight {
    Normal,
    Medium,
    Semibold,
}

/// A text colour, by the design token it reads.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Ink {
    Text,
    Text2,
    Text3,
    Step,
    Link,
    Failure,
    Settled,
    Attention,
    Accent,
    Badge(Tone),
}

/// What a click on a span does.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub enum Act {
    /// Opens or folds a fold.
    Toggle(usize),
    /// Opens a web address.
    Link(String),
    /// The app's own action, by its index.
    App(usize),
}

/// When a span shows: always, or only while a fold is open or closed.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Show {
    Always,
    Open(usize),
    Closed(usize),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct Frame {
    pub look: Look,
    /// Its left edge, and its right edge's distance from the right.
    pub indent: i32,
    pub right: i32,
}

/// How a frame is drawn.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Look {
    /// A bordered card.
    Card,
    /// A card on the proposed fill, as the plan shows.
    Plan,
    /// The director's answer: green fill and border.
    Settled,
    /// An agent chip: a faint fill.
    Chip,
    /// Your words: a bubble at the right, as wide as its words.
    Bubble,
    /// Code on a sunken fill.
    Code,
    /// A bar down the left.
    Quote,
    /// A hairline across, on an empty paragraph.
    Rule,
    /// Hairlines either side of a centred line.
    Rules,
    /// An inline sub-goal tag.
    Tag,
    /// An inline badge in a tone.
    Badge(Tone),
}

impl Look {
    /// Padding inside it: top, right, bottom, left.
    pub fn pad(self) -> (i32, i32, i32, i32) {
        match self {
            Look::Card => (14, 16, 14, 16),
            Look::Plan => (12, 14, 12, 14),
            Look::Settled => (8, 10, 8, 10),
            Look::Chip => (10, 10, 10, 10),
            Look::Bubble => (10, 14, 10, 14),
            Look::Code => (12, 12, 12, 12),
            Look::Quote => (0, 0, 0, 15),
            Look::Rule | Look::Rules | Look::Tag | Look::Badge(_) => (0, 0, 0, 0),
        }
    }
}

impl Style {
    pub const fn of(size: Size) -> Style {
        Style {
            size,
            weight: Weight::Normal,
            italic: false,
            ink: Ink::Text,
            code: false,
            act: None,
            show: Show::Always,
            frame: None,
        }
    }

    pub const BODY: Style = Style::of(Size::Body);
    pub const CALLOUT: Style = Style::of(Size::Callout);
    pub const META: Style = Style::of(Size::Meta);
    /// Who spoke: 12 px, medium, the second text colour.
    pub const SPEAKER: Style = Style {
        size: Size::Meta,
        weight: Weight::Medium,
        italic: false,
        ink: Ink::Text2,
        code: false,
        act: None,
        show: Show::Always,
        frame: None,
    };
    /// A card's label: 12 px, medium, the third text colour.
    pub const LABEL: Style = Style {
        size: Size::Meta,
        weight: Weight::Medium,
        italic: false,
        ink: Ink::Text3,
        code: false,
        act: None,
        show: Show::Always,
        frame: None,
    };

    pub fn ink(self, ink: Ink) -> Style {
        Style { ink, ..self }
    }

    pub fn weight(self, weight: Weight) -> Style {
        Style { weight, ..self }
    }

    pub fn act(self, act: Act) -> Style {
        Style { act: Some(act), ..self }
    }

    pub fn show(self, show: Show) -> Style {
        Style { show, ..self }
    }
}

impl Doc {
    pub fn new() -> Doc {
        Doc::default()
    }

    /// Starts a paragraph `above` pixels under the one before, in the open
    /// frames and folds; `starts` when it begins a block.
    pub fn para(&mut self, above: i32, starts: bool) -> &mut Doc {
        let padding: i32 = self.ctx.fresh.drain(..).map(|f| self.frames[f].look.pad().0).sum();
        self.paras.push(Para {
            indent: self.ctx.indent,
            right: self.ctx.right,
            above: above + padding,
            frames: self.ctx.frames.clone(),
            folds: self.ctx.folds.clone(),
            starts,
            ..Para::default()
        });
        self
    }

    fn last(&mut self) -> &mut Para {
        if self.paras.is_empty() || !self.ctx.fresh.is_empty() {
            self.para(0, false);
        }
        let last = self.paras.len() - 1;
        &mut self.paras[last]
    }

    pub fn align(&mut self, align: Align) -> &mut Doc {
        self.last().align = Some(align);
        self
    }

    pub fn avatar(&mut self, icon: Icon) -> &mut Doc {
        self.last().avatar = Some(icon);
        self
    }

    pub fn hang(&mut self, px: i32) -> &mut Doc {
        self.last().hang = px;
        self
    }

    pub fn push(&mut self, content: Content, style: Style) -> &mut Doc {
        self.last().spans.push(Span { content, style });
        self
    }

    /// Words in a style; each newline in them starts a paragraph.
    pub fn text(&mut self, text: &str, style: Style) -> &mut Doc {
        for (i, line) in text.split('\n').enumerate() {
            if i > 0 {
                self.para(0, false);
            }
            if !line.is_empty() {
                self.push(Content::Text(line.to_string()), style.clone());
            }
        }
        self
    }

    pub fn ornament(&mut self, text: &str, style: Style) -> &mut Doc {
        self.push(Content::Ornament(text.to_string()), style)
    }

    pub fn icon(&mut self, icon: Icon) -> &mut Doc {
        self.push(Content::Icon(icon), Style::CALLOUT)
    }

    /// Words in an inline frame, a pill, padded by a space either side.
    pub fn pill(&mut self, look: Look, text: &str, style: Style) -> &mut Doc {
        let (indent, right) = (self.ctx.indent, self.ctx.right);
        self.frames.push(Frame { look, indent, right });
        let style = Style { frame: Some(self.frames.len() - 1), ..style };
        self.ornament(" ", style.clone()).push(Content::Text(text.to_string()), style.clone()).ornament(" ", style)
    }

    /// A widget the app makes for this doc's next embed index, copied as `copied`.
    pub fn embed(&mut self, copied: &str) -> usize {
        let index = self.embeds;
        self.embeds += 1;
        self.push(Content::Embed(index, copied.to_string()), Style::CALLOUT);
        index
    }

    /// An app action for this doc's next index.
    pub fn act(&mut self) -> Act {
        self.acts += 1;
        Act::App(self.acts - 1)
    }

    /// Opens a frame: what follows is drawn inside it, padded.
    pub fn open_frame(&mut self, look: Look) -> usize {
        self.frames.push(Frame { look, indent: self.ctx.indent, right: self.ctx.right });
        let id = self.frames.len() - 1;
        let (_, right, _, left) = look.pad();
        self.ctx.indent += left;
        self.ctx.right += right;
        self.ctx.frames.push(id);
        self.ctx.fresh.push(id);
        id
    }

    pub fn close_frame(&mut self) {
        let Some(id) = self.ctx.frames.pop() else { return };
        let (_, right, bottom, left) = self.frames[id].look.pad();
        self.ctx.indent -= left;
        self.ctx.right -= right;
        self.ctx.fresh.retain(|f| *f != id);
        if let Some(last) = self.paras.last_mut().filter(|p| p.frames.contains(&id)) {
            last.below += bottom;
        }
    }

    /// Opens a fold: what follows hides while it is closed.
    pub fn open_fold(&mut self, open: bool) -> usize {
        self.folds.push(open);
        let id = self.folds.len() - 1;
        self.ctx.folds.push(id);
        id
    }

    pub fn close_fold(&mut self) {
        self.ctx.folds.pop();
    }

    pub fn indent(&mut self, px: i32) {
        self.ctx.indent += px;
    }

    /// Adds `other` after this doc's last paragraph, in the open frames and
    /// folds, its own frames, folds, actions and embeds numbered after these.
    pub fn append(&mut self, other: Doc) {
        let (frames, folds, acts, embeds) = (self.frames.len(), self.folds.len(), self.acts, self.embeds);
        let ctx = self.ctx.clone();
        self.frames.extend(other.frames.into_iter().map(|f| Frame {
            indent: f.indent + ctx.indent,
            right: f.right + ctx.right,
            ..f
        }));
        self.folds.extend(other.folds);
        self.acts += other.acts;
        self.embeds += other.embeds;
        let fold = |f: usize| f + folds;
        for (i, mut para) in other.paras.into_iter().enumerate() {
            para.indent += ctx.indent;
            para.right += ctx.right;
            para.frames = ctx.frames.iter().copied().chain(para.frames.iter().map(|f| f + frames)).collect();
            para.folds = ctx.folds.iter().copied().chain(para.folds.iter().map(|f| fold(*f))).collect();
            if i == 0 {
                para.above += self.ctx.fresh.drain(..).map(|f| self.frames[f].look.pad().0).sum::<i32>();
            }
            for span in &mut para.spans {
                span.style.frame = span.style.frame.map(|f| f + frames);
                span.style.show = match span.style.show {
                    Show::Always => Show::Always,
                    Show::Open(f) => Show::Open(fold(f)),
                    Show::Closed(f) => Show::Closed(fold(f)),
                };
                span.style.act = span.style.act.take().map(|a| match a {
                    Act::Toggle(f) => Act::Toggle(fold(f)),
                    Act::App(a) => Act::App(a + acts),
                    link => link,
                });
                if let Content::Embed(e, _) = &mut span.content {
                    *e += embeds;
                }
            }
            self.paras.push(para);
        }
    }

    /// Its characters in the surface: each paragraph's, a newline between.
    pub fn len(&self) -> usize {
        self.paras.iter().map(Para::len).sum::<usize>() + self.paras.len().saturating_sub(1)
    }

    pub fn is_empty(&self) -> bool {
        self.paras.is_empty()
    }

    /// What the surface holds, character for character.
    pub fn shown(&self) -> String {
        self.paras
            .iter()
            .map(|p| p.spans.iter().map(|s| s.content.shown()).collect::<String>())
            .collect::<Vec<_>>()
            .join("\n")
    }

    /// Where each paragraph starts in the surface.
    pub fn starts(&self) -> Vec<usize> {
        let mut at = 0;
        self.paras
            .iter()
            .map(|p| {
                let start = at;
                at += p.len() + 1;
                start
            })
            .collect()
    }

    /// What a click at character `at` does.
    pub fn act_at(&self, at: usize) -> Option<Act> {
        self.span_at(at).and_then(|s| s.style.act.clone())
    }

    /// The span holding character `at`.
    pub fn span_at(&self, at: usize) -> Option<&Span> {
        let mut start = 0;
        for para in &self.paras {
            let end = start + para.len();
            if at < end {
                let mut from = start;
                for span in &para.spans {
                    if at < from + span.content.len() {
                        return Some(span);
                    }
                    from += span.content.len();
                }
            }
            if at <= end {
                return None;
            }
            start = end + 1;
        }
        None
    }

    /// Whether a paragraph shows, given which folds are open.
    pub fn para_shows(para: &Para, open: &dyn Fn(usize) -> bool) -> bool {
        para.folds.iter().all(|f| open(*f))
    }

    /// What copying characters `from..to` gives, with the folds `open` says
    /// are open: the words shown, without ornaments; a table as its rows,
    /// tab-separated; a blank line before each block.
    pub fn copied(&self, from: usize, to: usize, open: &dyn Fn(usize) -> bool) -> String {
        let mut out = String::new();
        let mut any = false;
        let mut start = 0;
        for para in &self.paras {
            let end = start + para.len();
            if end >= from && start < to.max(from + 1) && Doc::para_shows(para, open) {
                let mut line = String::new();
                let mut words = false;
                let mut at = start;
                for span in &para.spans {
                    let len = span.content.len();
                    let (a, b) = (from.max(at), to.min(at + len));
                    let shows = match span.style.show {
                        Show::Always => true,
                        Show::Open(f) => open(f),
                        Show::Closed(f) => !open(f),
                    };
                    if a < b && shows {
                        match &span.content {
                            Content::Text(t) => {
                                line.extend(t.chars().skip(a - at).take(b - a));
                                words = true;
                            }
                            Content::Table(table) => {
                                line.push_str(&copy::table_rows(table));
                                words = true;
                            }
                            Content::Embed(_, copied) if !copied.is_empty() => {
                                line.push_str(copied);
                                words = true;
                            }
                            Content::Ornament(_) | Content::Icon(_) | Content::Embed(..) => {}
                        }
                    }
                    at += len;
                }
                let empty = para.spans.is_empty() && from <= start && start < to;
                if words || empty {
                    if any {
                        out.push_str(if para.starts { "\n\n" } else { "\n" });
                    }
                    out.push_str(&line);
                    any = true;
                }
            }
            start = end + 1;
        }
        out
    }
}

impl Para {
    pub fn len(&self) -> usize {
        self.spans.iter().map(|s| s.content.len()).sum()
    }

    pub fn is_empty(&self) -> bool {
        self.spans.is_empty()
    }
}

/// Markdown as paragraphs: headings, bullets, code, tables, quotes and rules,
/// a block's words styled as its inline marks say.
pub fn markdown(source: &str) -> Doc {
    markdown_in(source, Style::BODY)
}

/// Markdown in a base style: its size and colour for words not otherwise marked.
pub fn markdown_in(source: &str, base: Style) -> Doc {
    let mut doc = Doc::new();
    blocks(&mut doc, markdown::parse(source), &base);
    doc
}

/// Markdown that folds, whole blocks at a time, once more than `lines`
/// lines have shown; Show all opens the rest.
pub fn markdown_folded(source: &str, lines: usize) -> Doc {
    let parsed = markdown::parse(source);
    let mut seen = 0;
    let shown = parsed
        .iter()
        .take_while(|block| {
            let fits = seen < lines;
            seen += block_lines(block);
            fits
        })
        .count()
        .max(1);
    let mut doc = Doc::new();
    let rest: Vec<Block> = parsed.iter().skip(shown).cloned().collect();
    blocks(&mut doc, parsed.into_iter().take(shown).collect(), &Style::BODY);
    if rest.is_empty() {
        return doc;
    }
    let fold = doc.open_fold(false);
    let mut more = Doc::new();
    blocks(&mut more, rest, &Style::BODY);
    if let Some(first) = more.paras.first_mut() {
        first.above += BLOCK_GAP;
        first.starts = true;
    }
    doc.append(more);
    doc.close_fold();
    doc.para(4, false);
    let link = Style::META.ink(Ink::Link).act(Act::Toggle(fold));
    doc.ornament("Show all", link.clone().show(Show::Closed(fold)));
    doc.ornament("Show less", link.show(Show::Open(fold)));
    doc
}

/// About how many lines a block takes.
fn block_lines(block: &Block) -> usize {
    match block {
        Block::Paragraph(text) | Block::Code(text) => text.lines().count().max(1),
        Block::Table(table) => table.rows.len() + 1,
        Block::Quote(inner) => inner.iter().map(block_lines).sum(),
        Block::Heading(..) | Block::Bullet { .. } | Block::Rule => 1,
    }
}

fn blocks(doc: &mut Doc, parsed: Vec<Block>, base: &Style) {
    let mut after_bullet = false;
    for (i, block) in parsed.into_iter().enumerate() {
        let above = if i == 0 { 0 } else { BLOCK_GAP };
        let bullet = matches!(block, Block::Bullet { .. });
        match block {
            Block::Heading(_, title) => {
                doc.para(above + 4, true);
                inline(doc, &title, &Style { size: Size::Title, weight: Weight::Semibold, ..base.clone() });
            }
            Block::Paragraph(text) => {
                for (n, line) in text.split('\n').enumerate() {
                    doc.para(if n == 0 { above } else { 0 }, n == 0);
                    inline(doc, line.trim_end(), base);
                }
            }
            Block::Bullet { text, number, depth } => {
                // A task item's box stands in for its bullet.
                let task = [("[x] ", "☑ "), ("[X] ", "☑ "), ("[ ] ", "☐ ")]
                    .iter()
                    .find_map(|(mark, shown)| text.strip_prefix(mark).map(|rest| (rest.to_string(), *shown)));
                let (text, marker) = match (task, &number) {
                    (Some((rest, shown)), None) => (rest, shown.to_string()),
                    (_, Some(n)) => (text.clone(), format!("{n} ")),
                    (None, None) if depth == 0 => (text.clone(), "• ".to_string()),
                    (None, None) => (text.clone(), "◦ ".to_string()),
                };
                // Items of one list follow each other line by line.
                let follows = i > 0 && after_bullet;
                doc.para(if follows { 4 } else { above }, !follows);
                let hang = if number.is_some() { 20 } else { 14 };
                let last = doc.paras.len() - 1;
                doc.paras[last].indent += depth as i32 * 16;
                doc.hang(hang);
                doc.push(Content::Text(marker), base.clone().ink(Ink::Text2));
                inline(doc, &text, base);
            }
            Block::Code(code) => {
                doc.open_frame(Look::Code);
                let style = Style { size: Size::Code, ..base.clone() };
                for (n, line) in code.split('\n').enumerate() {
                    if n == 0 {
                        doc.para(above, true);
                    } else {
                        doc.para(0, false);
                    }
                    if !line.is_empty() {
                        doc.push(Content::Text(line.to_string()), style.clone());
                    }
                }
                doc.close_frame();
            }
            Block::Table(table) => {
                doc.para(above, true);
                doc.push(Content::Table(table), base.clone());
            }
            Block::Quote(inner) => {
                doc.open_frame(Look::Quote);
                let mut quoted = Doc::new();
                blocks(&mut quoted, inner, &base.clone().ink(Ink::Text2));
                if let Some(first) = quoted.paras.first_mut() {
                    first.above += above;
                    first.starts = true;
                }
                doc.append(quoted);
                doc.close_frame();
            }
            Block::Rule => {
                // A rule copies as nothing: its line holds only a space.
                doc.para(above, true);
                let (indent, right) = (doc.ctx.indent, doc.ctx.right);
                doc.frames.push(Frame { look: Look::Rule, indent, right });
                let id = doc.frames.len() - 1;
                let last = doc.paras.len() - 1;
                doc.paras[last].frames.push(id);
                doc.ornament(" ", base.clone());
            }
        }
        after_bullet = bullet;
    }
}

/// Inline markdown into the last paragraph.
fn inline(doc: &mut Doc, text: &str, base: &Style) {
    for run in markdown::runs(text) {
        let mut style = base.clone();
        if run.bold {
            style.weight = Weight::Semibold;
        }
        style.italic |= run.italic;
        style.code |= run.code;
        if let Some(url) = run.link {
            style.ink = Ink::Link;
            style.act = Some(Act::Link(url));
        }
        doc.push(Content::Text(run.text), style);
    }
}

/// A turn: its head, if it has one, then its body indented under it. A turn
/// without a head continues the one before.
pub fn turn(head: Option<Doc>, body: Doc) -> Doc {
    let mut doc = Doc::new();
    let has_head = head.is_some();
    if let Some(mut head) = head {
        if let Some(first) = head.paras.first_mut() {
            first.above += TURN_GAP;
            first.starts = true;
        }
        doc.append(head);
    }
    doc.indent(BODY);
    let mut body = body;
    if let Some(first) = body.paras.first_mut() {
        first.above += if has_head { HEAD_GAP } else { BLOCK_GAP };
        if has_head {
            first.starts = false;
        }
    }
    doc.append(body);
    doc
}

/// A turn's head: the speaker's mark, who spoke, when, and what about.
pub fn head(avatar: Option<Icon>, who: &str, at: Option<&str>, detail: Option<&str>) -> Doc {
    let mut doc = Doc::new();
    doc.para(0, true);
    doc.paras[0].indent = BODY;
    if let Some(icon) = avatar {
        doc.avatar(icon);
    }
    doc.push(Content::Text(who.to_string()), Style::SPEAKER);
    meta_tail(&mut doc, at, detail);
    doc
}

/// " · when · detail" after a head's speaker.
pub fn meta_tail(doc: &mut Doc, at: Option<&str>, detail: Option<&str>) {
    for part in [at.map(time::when), detail.map(str::to_string)].into_iter().flatten().filter(|p| !p.is_empty()) {
        doc.push(Content::Text(format!(" · {part}")), Style::META.ink(Ink::Text3));
    }
}

/// An agent's head: its state's mark, its name and when it spoke.
pub fn agent_head(title: &str, state: WorkState, at: Option<&str>) -> Doc {
    head(Some(Icon::State(state)), title, at, None)
}

/// What the person said, as a bubble at the right under who and when; past
/// eight lines the rest folds behind Show all.
pub fn told(label: &str, at: Option<&str>, said: &str) -> Doc {
    let mut doc = Doc::new();
    doc.para(TURN_GAP, true).align(Align::End);
    doc.push(Content::Text(label.to_string()), Style::SPEAKER);
    meta_tail(&mut doc, at, None);
    if !said.trim().is_empty() {
        doc.open_frame(Look::Bubble);
        let first = doc.paras.len();
        folded(&mut doc, said, 8, Style::BODY);
        if let Some(p) = doc.paras.get_mut(first) {
            p.above += HEAD_GAP;
        }
        doc.close_frame();
    }
    doc
}

/// What a message the person sent says while the director is still busy with
/// the one before it.
pub const WAITING_FOR_DIRECTOR: &str = "Waiting for the director to finish the last one";

/// Says under the person's words that the director has not been given them yet.
pub fn waiting_for_director(doc: &mut Doc) {
    doc.para(HEAD_GAP, false).align(Align::End);
    doc.text(WAITING_FOR_DIRECTOR, Style::META.ink(Ink::Text3));
}

/// Words that fold after `lines` lines, with Show all and Show less.
pub fn folded(doc: &mut Doc, text: &str, lines: usize, style: Style) {
    let all: Vec<&str> = text.split('\n').collect();
    let mut shown = 0;
    let mut chars = 0;
    for line in &all {
        chars += line.chars().count();
        if shown >= lines || chars > lines * 110 {
            break;
        }
        shown += 1;
    }
    if shown == 0 {
        shown = 1;
    }
    doc.para(0, false);
    doc.text(&all[..shown].join("\n"), style.clone());
    if shown == all.len() {
        return;
    }
    let fold = doc.open_fold(false);
    doc.para(0, false);
    doc.text(&all[shown..].join("\n"), style.clone());
    doc.close_fold();
    doc.para(2, false);
    let link = Style::META.ink(Ink::Link).act(Act::Toggle(fold));
    doc.ornament("Show all", link.clone().show(Show::Closed(fold)));
    doc.ornament("Show less", link.show(Show::Open(fold)));
}

/// A line that opens and folds what follows it: a chevron, then its words.
fn toggle_line(doc: &mut Doc, fold: usize, above: i32) {
    doc.para(above, false);
    let act = Act::Toggle(fold);
    let chevron = Style::CALLOUT.ink(Ink::Text3).act(act);
    doc.ornament("▸ ", chevron.clone().show(Show::Closed(fold)));
    doc.ornament("▾ ", chevron.show(Show::Open(fold)));
}

/// A session's step: a thinking that folds to one line, a tool call as its
/// line, or what the agent said.
pub fn step(step: &Step, open: bool) -> Doc {
    match step.kind {
        StepKind::Thinking => {
            let mut doc = Doc::new();
            let fold = doc.folds.len();
            toggle_line(&mut doc, fold, 0);
            doc.push(Content::Text("Thinking".into()), Style::CALLOUT.ink(Ink::Text3).act(Act::Toggle(fold)));
            doc.open_fold(open);
            doc.indent(FOLD_INDENT);
            doc.para(4, false);
            doc.text(step.text(), Style::CALLOUT.ink(Ink::Text2));
            doc.close_fold();
            doc
        }
        StepKind::Tool => match &step.tool {
            Some(call) => tool_row(call),
            None => {
                let mut doc = Doc::new();
                doc.text("a tool call", Style::CALLOUT.ink(Ink::Text2));
                doc
            }
        },
        _ => markdown(step.text()),
    }
}

/// One tool call in a line, its input and result folded under it.
pub fn tool_row(call: &ToolCall) -> Doc {
    let mut doc = Doc::new();
    let fold = doc.folds.len();
    toggle_line(&mut doc, fold, 0);
    let act = Act::Toggle(fold);
    doc.push(
        Content::Text(call.name.clone()),
        Style::CALLOUT.weight(Weight::Semibold).ink(Ink::Text2).act(act.clone()),
    );
    if !call.summary.is_empty() {
        doc.push(Content::Text(format!(" {}", call.summary)), Style::CALLOUT.ink(Ink::Text2).act(act));
    }
    if call.failed {
        doc.ornament(" ", Style::CALLOUT).icon(Icon::Named("dialog-error-symbolic", Ink::Failure));
    } else if call.result.is_none() {
        doc.ornament(" ", Style::CALLOUT).icon(Icon::Spinner);
    }
    doc.open_fold(false);
    doc.indent(FOLD_INDENT);
    for block in [Some(call.input.as_str()), call.result.as_deref()].into_iter().flatten().filter(|b| !b.is_empty()) {
        doc.open_frame(Look::Code);
        doc.para(6, false);
        doc.text(block.trim_end_matches('\n'), Style::of(Size::Code));
        doc.close_frame();
    }
    doc.close_fold();
    doc
}

/// A run of tool calls: one line each, more than three folded into "N tool calls".
pub fn tools(calls: &[ToolCall]) -> Doc {
    let mut rows = Doc::new();
    for (i, call) in calls.iter().enumerate() {
        let mut row = tool_row(call);
        if let Some(first) = row.paras.first_mut() {
            first.above += if i == 0 { 0 } else { 4 };
        }
        rows.append(row);
    }
    if calls.len() <= 3 {
        return rows;
    }
    let mut doc = Doc::new();
    let fold = doc.folds.len();
    toggle_line(&mut doc, fold, 0);
    let words = format!("{} tool calls", calls.len());
    doc.push(Content::Text(words), Style::CALLOUT.ink(Ink::Text2).act(Act::Toggle(fold)));
    doc.open_fold(false);
    doc.indent(FOLD_INDENT);
    if let Some(first) = rows.paras.first_mut() {
        first.above += 4;
    }
    doc.append(rows);
    doc.close_fold();
    doc
}

/// A centred line between two hairlines: a day, a look, a state change.
pub fn event_line(text: &str, ink: Ink, strong: bool) -> Doc {
    let mut doc = Doc::new();
    doc.para(TURN_GAP, true).align(Align::Center);
    let (indent, right) = (doc.ctx.indent, doc.ctx.right);
    doc.frames.push(Frame { look: Look::Rules, indent, right });
    let last = doc.paras.len() - 1;
    doc.paras[last].frames.push(doc.frames.len() - 1);
    let weight = if strong { Weight::Semibold } else { Weight::Normal };
    doc.push(Content::Text(text.to_string()), Style::META.ink(ink).weight(weight));
    doc
}
