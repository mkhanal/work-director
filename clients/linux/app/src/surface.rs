//! A run of a conversation as one text surface: every turn between two
//! interactive cards written into one non-editable text view, so a drag or
//! Ctrl+A selects across all of it and Ctrl+C copies it as it reads. The kit's
//! `Doc` says what each character is; the surface styles it with tags, draws
//! the cards, bubbles and code blocks behind it, its avatars beside it, and
//! puts tables and small widgets where the doc holds their character.

use std::cell::RefCell;
use std::collections::HashMap;
use std::rc::Rc;

use adw::prelude::*;
use adw::subclass::prelude::*;
use gtk::{gdk, gio, glib, graphene, gsk, pango};

use kit::markdown::Align;
use kit::transcript::{Act, Content, Doc, Icon, Ink, Look, Show, Size, Style, Weight};
use kit::Tone;

use crate::ui;

/// A turn as text: its doc, and the actions and widgets its doc indexes.
#[derive(Default)]
pub struct Rich {
    pub doc: Doc,
    pub acts: Vec<Rc<dyn Fn()>>,
    pub embeds: Vec<Box<dyn Fn() -> gtk::Widget>>,
}

impl Rich {
    pub fn new(doc: Doc) -> Rich {
        Rich { doc, ..Rich::default() }
    }

    /// An action a click on a span runs.
    pub fn act(&mut self, run: impl Fn() + 'static) -> Act {
        self.acts.push(Rc::new(run));
        self.doc.act()
    }

    /// A widget in the line, copied as `copied`.
    pub fn embed(&mut self, copied: &str, make: impl Fn() -> gtk::Widget + 'static) {
        self.embeds.push(Box::new(make));
        self.doc.embed(copied);
    }

    /// Adds `other` after this, inside whatever frame or fold this has open.
    pub fn append(&mut self, other: Rich) {
        self.doc.append(other.doc);
        self.acts.extend(other.acts);
        self.embeds.extend(other.embeds);
    }
}

/// One turn's place in the surface.
struct Placed {
    id: String,
    /// Its first character, and the counts of what came before it, to cut back to.
    start: usize,
    paras: usize,
    frames: usize,
    folds: usize,
    acts: usize,
    embeds: usize,
    /// What it said, as markdown, for its menu's Copy Message.
    said: Option<String>,
}

/// A widget standing on a character, and when it shows.
struct Anchored {
    widget: gtk::Widget,
    at: usize,
    folds: Vec<usize>,
    show: Show,
    table: bool,
}

#[derive(Default)]
pub struct State {
    doc: Doc,
    starts: Vec<usize>,
    open: Vec<bool>,
    turns: Vec<Placed>,
    acts: Vec<Rc<dyn Fn()>>,
    embeds: Vec<Box<dyn Fn() -> gtk::Widget>>,
    anchored: Vec<Anchored>,
    /// Each pill's characters, by frame.
    pills: HashMap<usize, (usize, usize)>,
    /// The tag that places each bubble, by frame.
    bubbles: HashMap<usize, gtk::TextTag>,
    styles: HashMap<String, (gtk::TextTag, Style)>,
    paras: HashMap<String, gtk::TextTag>,
    folds: HashMap<String, gtk::TextTag>,
    /// The last paragraph's tags, for the newline that ends it when more follows.
    last_tags: Vec<gtk::TextTag>,
    /// Whether each turn's folds were open, by turn id, while it is rebuilt.
    remembered: HashMap<String, Vec<bool>>,
    swatches: HashMap<&'static str, gtk::Widget>,
    width: i32,
    /// The selection to put back once a refresh has rewritten the end.
    held: Option<(usize, usize)>,
    /// The turn a menu was opened on.
    pointed: Option<usize>,
    menu: gio::SimpleActionGroup,
    pressed: Option<(f64, f64)>,
}

mod imp {
    use super::*;

    #[derive(Default)]
    pub struct Surface {
        pub state: RefCell<State>,
    }

    #[glib::object_subclass]
    impl ObjectSubclass for Surface {
        const NAME: &'static str = "WdSurface";
        type Type = super::Surface;
        type ParentType = gtk::TextView;
    }

    impl ObjectImpl for Surface {}

    impl WidgetImpl for Surface {
        /// As tall as its lines: a text view's own height counts lines a
        /// fold has hidden.
        fn measure(&self, orientation: gtk::Orientation, for_size: i32) -> (i32, i32, i32, i32) {
            let measured = self.parent_measure(orientation, for_size);
            if orientation == gtk::Orientation::Horizontal {
                return measured;
            }
            let view = self.obj();
            let (y, h) = view.line_yrange(&view.buffer().end_iter());
            (y + h, y + h, -1, -1)
        }

        fn size_allocate(&self, width: i32, height: i32, baseline: i32) {
            self.parent_size_allocate(width, height, baseline);
            let changed = {
                let mut state = self.state.borrow_mut();
                let changed = state.width != width;
                state.width = width;
                changed
            };
            if changed {
                let weak = self.obj().downgrade();
                glib::idle_add_local_once(move || {
                    if let Some(surface) = weak.upgrade() {
                        surface.place_bubbles();
                    }
                });
            }
        }
    }

    impl TextViewImpl for Surface {
        fn snapshot_layer(&self, layer: gtk::TextViewLayer, snapshot: gtk::Snapshot) {
            if layer == gtk::TextViewLayer::BelowText {
                self.obj().draw_below(&snapshot);
            }
        }

        fn copy_clipboard(&self) {
            let surface = self.obj();
            let text = surface.selected();
            if !text.is_empty() {
                surface.clipboard().set_text(&text);
            }
        }
    }
}

glib::wrapper! {
    pub struct Surface(ObjectSubclass<imp::Surface>)
        @extends gtk::TextView, gtk::Widget,
        @implements gtk::Accessible, gtk::Buildable, gtk::ConstraintTarget, gtk::Scrollable;
}

/// Each ink's swatch: the CSS class whose colour it is.
const SWATCHES: &[&str] = &[
    "ink-text",
    "text2",
    "text3",
    "step-text",
    "ink-link",
    "failure",
    "settled-text",
    "attention-text",
    "accent-text",
    "swatch-hairline",
    "swatch-page",
    "swatch-tag",
    "swatch-bubble",
    "swatch-bubble-border",
    "swatch-answered",
    "swatch-answered-border",
    "swatch-proposed",
    "swatch-sunken",
    "swatch-chip",
    "swatch-code",
    "badge-attention",
    "badge-settled",
    "badge-live",
    "badge-waiting",
    "badge-decision",
    "swatch-badge-attention",
    "swatch-badge-settled",
    "swatch-badge-live",
    "swatch-badge-waiting",
    "swatch-badge-decision",
    "tint-gray",
    "tint-blue",
    "tint-indigo",
    "tint-orange",
    "tint-red",
    "tint-purple",
    "tint-teal",
    "tint-green",
    "tint-brown",
    "mark-done",
    "mark-needs-you",
    "mark-working",
    "mark-waiting",
    "mark-superseded",
    "mark-blocked",
];

impl Default for Surface {
    fn default() -> Self {
        Surface::new()
    }
}

impl Surface {
    pub fn new() -> Surface {
        let surface: Surface = glib::Object::builder()
            .property("editable", false)
            .property("cursor-visible", false)
            .property("wrap-mode", gtk::WrapMode::WordChar)
            .property("hexpand", true)
            .build();
        surface.add_css_class("surface");
        surface.update_property(&[gtk::accessible::Property::Label("Conversation")]);
        let swatches = gtk::Box::new(gtk::Orientation::Horizontal, 0);
        swatches.set_can_target(false);
        let mut map = HashMap::new();
        for class in SWATCHES {
            let swatch = gtk::Box::new(gtk::Orientation::Horizontal, 0);
            swatch.add_css_class(class);
            swatches.append(&swatch);
            map.insert(*class, swatch.upcast());
        }
        surface.add_overlay(&swatches, 0, 0);
        surface.imp().state.borrow_mut().swatches = map;
        surface.connect_actions();
        surface.connect_pointer();
        let weak = surface.downgrade();
        surface.buffer().connect_mark_set(move |_, _, mark| {
            if matches!(mark.name().as_deref(), Some("insert") | Some("selection_bound")) {
                if let Some(surface) = weak.upgrade() {
                    surface.mark_tables();
                }
            }
        });
        let weak = surface.downgrade();
        adw::StyleManager::default().connect_dark_notify(move |_| {
            let weak = weak.clone();
            glib::idle_add_local_once(move || {
                if let Some(surface) = weak.upgrade() {
                    surface.paint();
                }
            });
        });
        surface.connect_map(|surface| surface.paint());
        surface
    }

    /// How many turns it holds.
    pub fn len(&self) -> usize {
        self.imp().state.borrow().turns.len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Where the turn named `id` starts, in this widget's coordinates.
    pub fn locate(&self, id: &str) -> Option<f32> {
        let start = self.imp().state.borrow().turns.iter().find(|t| t.id == id)?.start;
        let iter = self.buffer().iter_at_offset(start as i32);
        let (y, _) = self.line_yrange(&iter);
        let (_, y) = self.buffer_to_window_coords(gtk::TextWindowType::Widget, 0, y);
        Some(y as f32)
    }

    /// Cuts the surface back to its first `keep` turns, holding the selection
    /// to put back once the rest is written again.
    pub fn truncate(&self, keep: usize) {
        let buffer = self.buffer();
        let mut state = self.imp().state.borrow_mut();
        if keep >= state.turns.len() {
            return;
        }
        if let Some((a, b)) = buffer.selection_bounds() {
            state.held = Some((a.offset() as usize, b.offset() as usize));
        }
        let removed: Vec<Placed> = state.turns.drain(keep..).collect();
        for (i, turn) in removed.iter().enumerate() {
            let end = removed.get(i + 1).map_or(state.open.len(), |next| next.folds);
            let open = state.open[turn.folds..end].to_vec();
            state.remembered.insert(turn.id.clone(), open);
        }
        let first = &removed[0];
        // The newline before the first turn removed goes with it.
        let from = first.start.saturating_sub(1);
        let mut start = buffer.iter_at_offset(from as i32);
        let mut end = buffer.end_iter();
        buffer.delete(&mut start, &mut end);
        state.doc.paras.truncate(first.paras);
        state.doc.frames.truncate(first.frames);
        state.doc.folds.truncate(first.folds);
        state.doc.acts = first.acts;
        state.doc.embeds = first.embeds;
        state.starts.truncate(first.paras);
        state.open.truncate(first.folds);
        state.acts.truncate(first.acts);
        state.embeds.truncate(first.embeds);
        state.anchored.retain(|a| a.at < from);
        let frames = first.frames;
        state.pills.retain(|f, _| *f < frames);
        state.bubbles.retain(|f, _| *f < frames);
        let last = state.doc.paras.len().checked_sub(1).map(|p| (state.doc.paras[p].clone(), p == 0));
        drop(state);
        let tags = last.map(|(para, first)| self.para_tags(&para, first)).unwrap_or_default();
        self.imp().state.borrow_mut().last_tags = tags;
    }

    /// Puts back a selection held over a refresh, as far as the text now reaches.
    pub fn settle(&self) {
        let Some((a, b)) = self.imp().state.borrow_mut().held.take() else { return };
        let buffer = self.buffer();
        let len = buffer.char_count() as usize;
        if a.min(b) >= len {
            return;
        }
        buffer.select_range(&buffer.iter_at_offset(a.min(len) as i32), &buffer.iter_at_offset(b.min(len) as i32));
    }

    /// Writes a turn at the end.
    pub fn append(&self, id: &str, rich: Rich, said: Option<String>) {
        let buffer = self.buffer();
        let (first_para, first_fold) = {
            let mut state = self.imp().state.borrow_mut();
            let start = if state.doc.paras.is_empty() { 0 } else { buffer.char_count() as usize + 1 };
            let placed = Placed {
                id: id.to_string(),
                start,
                paras: state.doc.paras.len(),
                frames: state.doc.frames.len(),
                folds: state.doc.folds.len(),
                acts: state.doc.acts,
                embeds: state.doc.embeds,
                said,
            };
            let (paras, folds) = (placed.paras, placed.folds);
            state.turns.push(placed);
            let Rich { doc, acts, embeds } = rich;
            let mut open = doc.folds.clone();
            if let Some(kept) = state.remembered.remove(id).filter(|k| k.len() == open.len()) {
                open = kept;
            }
            state.doc.append(doc);
            state.open.extend(open);
            state.acts.extend(acts);
            state.embeds.extend(embeds);
            (paras, folds)
        };
        // Folds are set before their text is written, so it never shows open first.
        let folds = self.imp().state.borrow().open.len();
        for f in first_fold..folds {
            self.show_fold(f);
        }
        let count = self.imp().state.borrow().doc.paras.len();
        for p in first_para..count {
            self.write_para(p);
        }
        for f in first_fold..folds {
            self.show_fold(f);
        }
        self.place_bubbles();
    }

    fn write_para(&self, p: usize) {
        let buffer = self.buffer();
        let (para, previous) = {
            let state = self.imp().state.borrow();
            (state.doc.paras[p].clone(), state.last_tags.clone())
        };
        let mut end = buffer.end_iter();
        if p > 0 {
            let refs: Vec<&gtk::TextTag> = previous.iter().collect();
            buffer.insert_with_tags(&mut end, "\n", &refs);
        }
        let start = end.offset() as usize;
        self.imp().state.borrow_mut().starts.push(start);
        let tags = self.para_tags(&para, p == 0);
        let mut at = start;
        for span in &para.spans {
            let mut span_tags = tags.clone();
            span_tags.push(self.style_tag(&span.style));
            match span.style.show {
                Show::Always => {}
                Show::Open(f) => span_tags.push(self.fold_tag(&format!("open-{f}"))),
                Show::Closed(f) => span_tags.push(self.fold_tag(&format!("closed-{f}"))),
            }
            let refs: Vec<&gtk::TextTag> = span_tags.iter().collect();
            let mut end = buffer.end_iter();
            match &span.content {
                Content::Text(text) | Content::Ornament(text) => buffer.insert_with_tags(&mut end, text, &refs),
                content => {
                    let anchor = buffer.create_child_anchor(&mut end);
                    let widget = self.anchored_widget(content);
                    self.add_child_at_anchor(&widget, &anchor);
                    let (a, b) = (buffer.iter_at_offset(at as i32), buffer.iter_at_offset(at as i32 + 1));
                    for tag in &refs {
                        buffer.apply_tag(*tag, &a, &b);
                    }
                    self.imp().state.borrow_mut().anchored.push(Anchored {
                        widget,
                        at,
                        folds: para.folds.clone(),
                        show: span.style.show,
                        table: matches!(content, Content::Table(_)),
                    });
                }
            }
            let len = span.content.len();
            if let Some(f) = span.style.frame {
                let mut state = self.imp().state.borrow_mut();
                let range = state.pills.entry(f).or_insert((at, at + len));
                range.1 = at + len;
            }
            at += len;
        }
        self.imp().state.borrow_mut().last_tags = tags;
    }

    fn anchored_widget(&self, content: &Content) -> gtk::Widget {
        match content {
            Content::Table(table) => ui::table(table),
            Content::Icon(icon) => icon_widget(*icon),
            Content::Embed(i, _) => {
                let state = self.imp().state.borrow();
                match state.embeds.get(*i) {
                    Some(make) => make(),
                    None => gtk::Box::new(gtk::Orientation::Horizontal, 0).upcast(),
                }
            }
            Content::Text(_) | Content::Ornament(_) => gtk::Box::new(gtk::Orientation::Horizontal, 0).upcast(),
        }
    }

    /// The tags a paragraph's characters carry: where it sits and the folds
    /// that hide it.
    fn para_tags(&self, para: &kit::transcript::Para, first: bool) -> Vec<gtk::TextTag> {
        let above = if first { 0 } else { para.above };
        let bubble = {
            let state = self.imp().state.borrow();
            para.frames
                .iter()
                .rev()
                .find(|f| state.doc.frames.get(**f).is_some_and(|fr| fr.look == Look::Bubble))
                .copied()
        };
        let indent = if bubble.is_some() { None } else { Some(para.indent + para.hang) };
        let key = format!("p:{indent:?}:{}:{}:{:?}:{above}:{}", para.right, para.hang, para.align, para.below);
        let mut tags = Vec::new();
        let existing = self.imp().state.borrow().paras.get(&key).cloned();
        tags.push(existing.unwrap_or_else(|| {
            let tag = gtk::TextTag::new(None);
            if let Some(indent) = indent {
                tag.set_left_margin(indent);
            }
            tag.set_right_margin(para.right);
            tag.set_indent(-para.hang);
            tag.set_pixels_above_lines(above);
            tag.set_pixels_below_lines(para.below);
            tag.set_justification(match para.align {
                Some(Align::Center) => gtk::Justification::Center,
                Some(Align::End) => gtk::Justification::Right,
                Some(Align::Start) | None => gtk::Justification::Left,
            });
            self.buffer().tag_table().add(&tag);
            self.imp().state.borrow_mut().paras.insert(key, tag.clone());
            tag
        }));
        if let Some(frame) = bubble {
            let existing = self.imp().state.borrow().bubbles.get(&frame).cloned();
            tags.push(existing.unwrap_or_else(|| {
                let tag = gtk::TextTag::new(None);
                self.buffer().tag_table().add(&tag);
                self.imp().state.borrow_mut().bubbles.insert(frame, tag.clone());
                tag
            }));
        }
        for f in &para.folds {
            tags.push(self.fold_tag(&format!("fold-{f}")));
        }
        tags
    }

    fn fold_tag(&self, name: &str) -> gtk::TextTag {
        if let Some(tag) = self.imp().state.borrow().folds.get(name) {
            return tag.clone();
        }
        let tag = gtk::TextTag::new(None);
        self.buffer().tag_table().add(&tag);
        self.imp().state.borrow_mut().folds.insert(name.to_string(), tag.clone());
        tag
    }

    fn style_tag(&self, style: &Style) -> gtk::TextTag {
        let look = Style { act: None, show: Show::Always, frame: None, ..style.clone() };
        let key = format!("{look:?}");
        if let Some((tag, _)) = self.imp().state.borrow().styles.get(&key) {
            return tag.clone();
        }
        let tag = gtk::TextTag::new(None);
        let (scale, height) = match style.size {
            Size::Title => (16.0 / 14.0, 1.3),
            Size::Body => (1.0, 1.45),
            Size::Callout => (13.0 / 14.0, 1.35),
            Size::Meta => (12.0 / 14.0, 1.3),
            Size::Small => (11.0 / 14.0, 1.3),
            Size::Code => (12.5 / 14.0, 1.35),
        };
        tag.set_scale(scale);
        tag.set_line_height(height);
        if style.size == Size::Code || style.code {
            tag.set_family(Some("monospace"));
        }
        tag.set_weight(match style.weight {
            Weight::Normal => 400,
            Weight::Medium => 500,
            Weight::Semibold => 600,
        });
        if style.italic {
            tag.set_style(pango::Style::Italic);
        }
        self.buffer().tag_table().add(&tag);
        self.color_tag(&tag, &look);
        self.imp().state.borrow_mut().styles.insert(key, (tag.clone(), look));
        tag
    }

    fn color_tag(&self, tag: &gtk::TextTag, style: &Style) {
        tag.set_foreground_rgba(Some(&self.ink(style.ink)));
        if style.code {
            tag.set_background_rgba(Some(&self.swatch("swatch-code")));
        }
    }

    /// A design token's colour now, read from its swatch.
    fn swatch(&self, class: &str) -> gdk::RGBA {
        self.imp().state.borrow().swatches.get(class).map(|w| w.color()).unwrap_or(gdk::RGBA::BLACK)
    }

    fn ink(&self, ink: Ink) -> gdk::RGBA {
        self.swatch(ink_class(ink))
    }

    /// Colours every style again, as the light or dark tokens now read.
    fn paint(&self) {
        let styles: Vec<(gtk::TextTag, Style)> = self.imp().state.borrow().styles.values().cloned().collect();
        for (tag, style) in styles {
            self.color_tag(&tag, &style);
        }
        self.queue_draw();
    }

    /// Shows or hides what fold `f` holds, as it is open or closed.
    fn show_fold(&self, f: usize) {
        let open = self.imp().state.borrow().open.get(f).copied().unwrap_or(true);
        let set = |tag: gtk::TextTag, hidden: bool| {
            if hidden {
                tag.set_invisible(true);
            } else {
                // Unset rather than visible, so a closed fold around it still hides it.
                tag.set_property("invisible-set", false);
            }
        };
        set(self.fold_tag(&format!("fold-{f}")), !open);
        set(self.fold_tag(&format!("open-{f}")), !open);
        set(self.fold_tag(&format!("closed-{f}")), open);
        let state = self.imp().state.borrow();
        for a in &state.anchored {
            let shows = a.folds.iter().all(|f| state.open[*f])
                && match a.show {
                    Show::Always => true,
                    Show::Open(f) => state.open[f],
                    Show::Closed(f) => !state.open[f],
                };
            a.widget.set_visible(shows);
        }
        drop(state);
        self.queue_draw();
    }

    fn toggle(&self, f: usize) {
        {
            let mut state = self.imp().state.borrow_mut();
            let Some(open) = state.open.get_mut(f) else { return };
            *open = !*open;
        }
        self.show_fold(f);
    }

    /// Sets each bubble's left edge so it sits at the right, as wide as its
    /// words up to the width a bubble may take.
    fn place_bubbles(&self) {
        let width = self.width();
        if width <= 0 {
            return;
        }
        let bubbles: Vec<(usize, gtk::TextTag)> =
            self.imp().state.borrow().bubbles.iter().map(|(f, t)| (*f, t.clone())).collect();
        for (frame, tag) in bubbles {
            let (texts, right) = {
                let state = self.imp().state.borrow();
                let Some(fr) = state.doc.frames.get(frame) else { continue };
                let (_, pad_right, _, pad_left) = fr.look.pad();
                let texts: Vec<String> = state
                    .doc
                    .paras
                    .iter()
                    .filter(|p| p.frames.contains(&frame))
                    .map(|p| p.spans.iter().map(|s| s.content.shown()).collect())
                    .collect();
                (texts, (fr.right + pad_right, pad_left))
            };
            let natural = texts.iter().map(|t| self.create_pango_layout(Some(t)).pixel_size().0).max().unwrap_or(0);
            let widest = width - right.0 - ui::SPACE_7 * 2 - right.1;
            let left = width - right.0 - natural.min(widest).max(0) - 2;
            tag.set_left_margin(left.max(0));
        }
        self.queue_draw();
    }

    /// Marks each table inside the selection as selected.
    fn mark_tables(&self) {
        let selected = self.buffer().selection_bounds().map(|(a, b)| (a.offset() as usize, b.offset() as usize));
        let state = self.imp().state.borrow();
        for a in state.anchored.iter().filter(|a| a.table) {
            if selected.is_some_and(|(s, e)| s <= a.at && a.at < e) {
                a.widget.add_css_class("selected");
            } else {
                a.widget.remove_css_class("selected");
            }
        }
    }

    /// The selection as copied: its words as shown, tables as rows.
    pub fn selected(&self) -> String {
        let Some((a, b)) = self.buffer().selection_bounds() else { return String::new() };
        let state = self.imp().state.borrow();
        let open = |f: usize| state.open.get(f).copied().unwrap_or(true);
        state.doc.copied(a.offset() as usize, b.offset() as usize, &open)
    }

    /// The character offset under a point in this widget.
    fn offset_at(&self, x: f64, y: f64) -> Option<usize> {
        let (bx, by) = self.window_to_buffer_coords(gtk::TextWindowType::Widget, x as i32, y as i32);
        let iter = self.iter_at_location(bx, by)?;
        // Past the end of a line is no span's.
        let past = iter.ends_line() && bx > self.iter_location(&iter).x() + 2;
        (!past).then_some(iter.offset() as usize)
    }

    /// A click that selects nothing runs what the span under it does; a
    /// right-click remembers the turn its menu acts on.
    fn connect_pointer(&self) {
        let events = gtk::EventControllerLegacy::new();
        events.set_propagation_phase(gtk::PropagationPhase::Capture);
        let weak = self.downgrade();
        events.connect_event(move |_, event| {
            let Some(surface) = weak.upgrade() else { return glib::Propagation::Proceed };
            let Some((x, y)) = event.position() else { return glib::Propagation::Proceed };
            let Some(button) = event.downcast_ref::<gdk::ButtonEvent>() else { return glib::Propagation::Proceed };
            let (x, y) = surface.within(x, y);
            match (event.event_type(), button.button()) {
                (gdk::EventType::ButtonPress, gdk::BUTTON_PRIMARY) => {
                    surface.imp().state.borrow_mut().pressed = Some((x, y));
                }
                (gdk::EventType::ButtonRelease, gdk::BUTTON_PRIMARY) => {
                    let pressed = surface.imp().state.borrow_mut().pressed.take();
                    let still = pressed.is_some_and(|(px, py)| (px - x).abs() < 4.0 && (py - y).abs() < 4.0);
                    if still {
                        let weak = surface.downgrade();
                        glib::idle_add_local_once(move || {
                            if let Some(surface) = weak.upgrade() {
                                if !surface.buffer().has_selection() {
                                    surface.click(x, y);
                                }
                            }
                        });
                    }
                }
                (gdk::EventType::ButtonPress, gdk::BUTTON_SECONDARY) => surface.point(x, y),
                _ => {}
            }
            glib::Propagation::Proceed
        });
        self.add_controller(events);
    }

    /// A point in the window as a point in this widget.
    fn within(&self, x: f64, y: f64) -> (f64, f64) {
        let Some(root) = self.root() else { return (x, y) };
        let native = root.upcast_ref::<gtk::Widget>();
        let (dx, dy) = root.surface_transform();
        let point = graphene::Point::new((x - dx) as f32, (y - dy) as f32);
        native.compute_point(self, &point).map_or((x, y), |p| (f64::from(p.x()), f64::from(p.y())))
    }

    fn click(&self, x: f64, y: f64) {
        let Some(at) = self.offset_at(x, y) else { return };
        let act = self.imp().state.borrow().doc.act_at(at);
        match act {
            Some(Act::Toggle(f)) => self.toggle(f),
            Some(Act::Link(url)) => {
                let window = self.root().and_downcast::<gtk::Window>();
                gtk::UriLauncher::new(&url).launch(window.as_ref(), gio::Cancellable::NONE, |_| {});
            }
            Some(Act::App(i)) => {
                let run = self.imp().state.borrow().acts.get(i).cloned();
                if let Some(run) = run {
                    run();
                }
            }
            None => {}
        }
    }

    fn point(&self, x: f64, y: f64) {
        let at = self.offset_at(x, y);
        let mut state = self.imp().state.borrow_mut();
        state.pointed = at.and_then(|at| state.turns.iter().rposition(|t| t.start <= at));
        let said = state.pointed.and_then(|t| state.turns[t].said.clone());
        let group = state.menu.clone();
        drop(state);
        for name in ["copy", "copy-markdown"] {
            if let Some(action) = group.lookup_action(name).and_downcast::<gio::SimpleAction>() {
                action.set_enabled(said.is_some());
            }
        }
    }

    /// The menu's Copy Message and Copy Message as Markdown, for the turn it was opened on.
    fn connect_actions(&self) {
        let actions = gio::SimpleActionGroup::new();
        for (name, markdown) in [("copy", false), ("copy-markdown", true)] {
            let action = gio::SimpleAction::new(name, None);
            let weak = self.downgrade();
            action.connect_activate(move |_, _| {
                let Some(surface) = weak.upgrade() else { return };
                let said = {
                    let state = surface.imp().state.borrow();
                    state.pointed.and_then(|t| state.turns.get(t)).and_then(|t| t.said.clone())
                };
                if let Some(said) = said {
                    surface.clipboard().set_text(&if markdown { said } else { kit::copy::plain(&said) });
                }
            });
            actions.add_action(&action);
        }
        self.insert_action_group("turn", Some(&actions));
        self.imp().state.borrow_mut().menu = actions;
        let menu = gio::Menu::new();
        for (label, action) in [("Copy Message", "turn.copy"), ("Copy Message as Markdown", "turn.copy-markdown")] {
            let item = gio::MenuItem::new(Some(label), Some(action));
            item.set_attribute_value("hidden-when", Some(&"action-disabled".to_variant()));
            menu.append_item(&item);
        }
        self.set_extra_menu(Some(&menu));
    }

    /// Draws behind the text: cards, bubbles, code, quotes, rules and pills;
    /// and each speaker's mark beside its line.
    fn draw_below(&self, snapshot: &gtk::Snapshot) {
        let state = self.imp().state.borrow();
        let width = self.width();
        let buffer = self.buffer();
        let open = |f: usize| state.open.get(f).copied().unwrap_or(true);
        let iter_at = |o: usize| buffer.iter_at_offset(o as i32);
        let shows = |p: usize| Doc::para_shows(&state.doc.paras[p], &open);
        for (id, frame) in state.doc.frames.iter().enumerate() {
            if let Some((a, b)) = state.pills.get(&id) {
                let first = state.doc.span_at(*a);
                let hidden = first.is_some_and(|s| match s.style.show {
                    Show::Always => false,
                    Show::Open(f) => !open(f),
                    Show::Closed(f) => open(f),
                });
                let para = state.starts.iter().rposition(|s| s <= a);
                if hidden || para.is_some_and(|p| !shows(p)) {
                    continue;
                }
                let (start, end) = (self.iter_location(&iter_at(*a)), self.iter_location(&iter_at(b - 1)));
                let rect = graphene::Rect::new(
                    start.x() as f32,
                    start.y() as f32 + 1.0,
                    (end.x() + end.width() - start.x()) as f32,
                    start.height() as f32 - 2.0,
                );
                let (fill, border) = match frame.look {
                    Look::Badge(tone) => (self.swatch(badge_fill(tone)), None),
                    _ => (self.swatch("swatch-tag"), Some(self.swatch("swatch-hairline"))),
                };
                fill_rounded(snapshot, &rect, 6.0, &fill, border.as_ref());
                continue;
            }
            let inside: Vec<usize> =
                (0..state.doc.paras.len()).filter(|p| state.doc.paras[*p].frames.contains(&id) && shows(*p)).collect();
            let (Some(&first), Some(&last)) = (inside.first(), inside.last()) else { continue };
            let (pad_top, _, pad_bottom, pad_left) = frame.look.pad();
            let (top_y, _) = self.line_yrange(&iter_at(state.starts[first]));
            let (last_y, last_h) = self.line_yrange(&iter_at(state.starts[last]));
            let top = top_y + if first == 0 { 0 } else { state.doc.paras[first].above } - pad_top;
            let bottom = last_y + last_h - state.doc.paras[last].below + pad_bottom;
            let mut x0 = frame.indent;
            let x1 = width - frame.right;
            if frame.look == Look::Bubble {
                if let Some(tag) = state.bubbles.get(&id) {
                    x0 = tag.left_margin() - pad_left;
                }
            }
            let rect = graphene::Rect::new(x0 as f32, top as f32, (x1 - x0) as f32, (bottom - top) as f32);
            match frame.look {
                Look::Card => fill_rounded(
                    snapshot,
                    &rect,
                    10.0,
                    &self.swatch("swatch-page"),
                    Some(&self.swatch("swatch-hairline")),
                ),
                Look::Plan => fill_rounded(
                    snapshot,
                    &rect,
                    10.0,
                    &self.swatch("swatch-proposed"),
                    Some(&self.swatch("swatch-hairline")),
                ),
                Look::Settled => fill_rounded(
                    snapshot,
                    &rect,
                    8.0,
                    &self.swatch("swatch-answered"),
                    Some(&self.swatch("swatch-answered-border")),
                ),
                Look::Chip => fill_rounded(snapshot, &rect, 8.0, &self.swatch("swatch-chip"), None),
                Look::Bubble => fill_rounded(
                    snapshot,
                    &rect,
                    14.0,
                    &self.swatch("swatch-bubble"),
                    Some(&self.swatch("swatch-bubble-border")),
                ),
                Look::Code => fill_rounded(snapshot, &rect, 10.0, &self.swatch("swatch-sunken"), None),
                Look::Quote => {
                    let bar = graphene::Rect::new(x0 as f32, top as f32, 3.0, (bottom - top) as f32);
                    snapshot.append_color(&self.swatch("swatch-hairline"), &bar);
                }
                Look::Rule => {
                    let p = &state.doc.paras[last];
                    let y = last_y + p.above + (last_h - p.above - p.below) / 2;
                    let line = graphene::Rect::new(x0 as f32, y as f32, (x1 - x0) as f32, 1.0);
                    snapshot.append_color(&self.swatch("swatch-hairline"), &line);
                }
                Look::Rules => {
                    let start = iter_at(state.starts[first]);
                    let mut end = start;
                    end.forward_to_line_end();
                    let (a, b) = (self.iter_location(&start), self.iter_location(&end));
                    let y = (a.y() + a.height() / 2) as f32;
                    let color = self.swatch("swatch-hairline");
                    if a.x() - 12 > x0 {
                        snapshot
                            .append_color(&color, &graphene::Rect::new(x0 as f32, y, (a.x() - 12 - x0) as f32, 1.0));
                    }
                    if b.x() + 12 < x1 {
                        snapshot.append_color(
                            &color,
                            &graphene::Rect::new((b.x() + 12) as f32, y, (x1 - b.x() - 12) as f32, 1.0),
                        );
                    }
                }
                Look::Tag | Look::Badge(_) => {}
            }
        }
        for (p, para) in state.doc.paras.iter().enumerate() {
            let Some(icon) = para.avatar else { continue };
            if !shows(p) {
                continue;
            }
            let loc = self.iter_location(&iter_at(state.starts[p]));
            let y = loc.y() as f32 + loc.height() as f32 / 2.0 - 12.0 + 3.0;
            self.draw_icon(snapshot, icon, 0.0, y, 24.0);
        }
    }

    /// An icon `size` pixels square at `x`, `y`.
    fn draw_icon(&self, snapshot: &gtk::Snapshot, icon: Icon, x: f32, y: f32, size: f32) {
        match icon {
            Icon::Mark(mark) => {
                let color = self.swatch(mark.css_class());
                let cr = snapshot.append_cairo(&graphene::Rect::new(x, y, size, size));
                cr.translate(f64::from(x), f64::from(y));
                ui::draw_mark(&cr, mark, f64::from(size), f64::from(size), &color);
            }
            Icon::State(state) => {
                let look = state.look();
                self.draw_named(snapshot, look.icon, &self.swatch(look.tint.css_class()), x, y, size);
            }
            Icon::Named(name, ink) => self.draw_named(snapshot, name, &self.ink(ink), x, y, size),
            Icon::Spinner => {}
        }
    }

    fn draw_named(&self, snapshot: &gtk::Snapshot, name: &str, color: &gdk::RGBA, x: f32, y: f32, size: f32) {
        let theme = gtk::IconTheme::for_display(&self.display());
        let icon = theme.lookup_icon(
            name,
            &[],
            16,
            self.scale_factor(),
            gtk::TextDirection::None,
            gtk::IconLookupFlags::empty(),
        );
        snapshot.save();
        snapshot.translate(&graphene::Point::new(x + (size - 16.0) / 2.0, y + (size - 16.0) / 2.0));
        icon.snapshot_symbolic(snapshot, 16.0, 16.0, &[*color]);
        snapshot.restore();
    }
}

/// A badge's fill swatch.
fn badge_fill(tone: Tone) -> &'static str {
    match tone {
        Tone::Attention => "swatch-badge-attention",
        Tone::Settled => "swatch-badge-settled",
        Tone::Live => "swatch-badge-live",
        Tone::Waiting => "swatch-badge-waiting",
        Tone::Decision => "swatch-badge-decision",
    }
}

fn fill_rounded(
    snapshot: &gtk::Snapshot,
    rect: &graphene::Rect,
    radius: f32,
    fill: &gdk::RGBA,
    border: Option<&gdk::RGBA>,
) {
    let rounded = gsk::RoundedRect::from_rect(*rect, radius);
    snapshot.push_rounded_clip(&rounded);
    snapshot.append_color(fill, rect);
    snapshot.pop();
    if let Some(border) = border {
        snapshot.append_border(&rounded, &[1.0; 4], &[*border; 4]);
    }
}

/// An icon in a line, as a widget standing on its character.
fn icon_widget(icon: Icon) -> gtk::Widget {
    match icon {
        Icon::State(state) => ui::state_icon(state).upcast(),
        Icon::Mark(mark) => ui::mark(mark, 16, mark.label()).upcast(),
        Icon::Named(name, ink) => ui::icon(name, &[ink_class(ink)]).upcast(),
        Icon::Spinner => {
            let spinner = gtk::Spinner::new();
            spinner.start();
            spinner.upcast()
        }
    }
}

/// The CSS class an ink's widget takes.
pub fn ink_class(ink: Ink) -> &'static str {
    match ink {
        Ink::Text => "ink-text",
        Ink::Text2 => "text2",
        Ink::Text3 => "text3",
        Ink::Step => "step-text",
        Ink::Link => "ink-link",
        Ink::Failure => "failure",
        Ink::Settled => "settled-text",
        Ink::Attention => "attention-text",
        Ink::Accent => "accent-text",
        Ink::Badge(tone) => tone.css_class(),
    }
}
