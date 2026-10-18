//! Find in page: Ctrl+F opens a search bar over the page; every match in the
//! words the page's surfaces show is highlighted, the current one stronger
//! and scrolled into view. Closing it leaves the current match selected, to
//! copy.

use std::cell::{Cell, RefCell};
use std::rc::{Rc, Weak};

use adw::prelude::*;
use gtk::{gdk, glib, pango};

use crate::surface::Surface;
use crate::ui;

/// A match: the words it is in, their place on the page, and its characters there.
struct Match {
    text: Text,
    place: usize,
    chars: (usize, usize),
}

pub struct FindBar {
    pub widget: gtk::SearchBar,
    entry: gtk::SearchEntry,
    count: gtk::Label,
    /// What the page scrolls, and the column of turns in it.
    scroller: gtk::ScrolledWindow,
    column: gtk::Box,
    /// Every match, in page order.
    found: RefCell<Vec<Match>>,
    current: Cell<Option<usize>>,
    /// The labels highlighted, each with the attributes it had of its own.
    marked: RefCell<Vec<(gtk::Label, Option<pango::AttrList>)>>,
    me: Weak<FindBar>,
}

impl FindBar {
    pub fn new(scroller: &gtk::ScrolledWindow, column: &gtk::Box) -> Rc<FindBar> {
        let entry = gtk::SearchEntry::builder().placeholder_text("Find on this page").width_chars(28).build();
        ui::announce(&entry, "Find on this page");
        let count = gtk::Label::new(None);
        count.add_css_class("dim-label");
        count.add_css_class("numeric");
        let previous = gtk::Button::from_icon_name("go-up-symbolic");
        previous.set_tooltip_text(Some("Previous match (Shift+Enter)"));
        ui::announce(&previous, "Previous match");
        let next = gtk::Button::from_icon_name("go-down-symbolic");
        next.set_tooltip_text(Some("Next match (Enter)"));
        ui::announce(&next, "Next match");
        let steps = gtk::Box::new(gtk::Orientation::Horizontal, 0);
        steps.add_css_class("linked");
        steps.append(&previous);
        steps.append(&next);
        let row = ui::hbox(8);
        row.append(&entry);
        row.append(&count);
        row.append(&steps);
        let widget = gtk::SearchBar::builder().child(&row).show_close_button(true).build();
        widget.connect_entry(&entry);
        let find = Rc::new_cyclic(|me| FindBar {
            widget,
            entry,
            count,
            scroller: scroller.clone(),
            column: column.clone(),
            found: RefCell::new(Vec::new()),
            current: Cell::new(None),
            marked: RefCell::new(Vec::new()),
            me: me.clone(),
        });
        find.wire(&previous, &next);
        find
    }

    fn wire(&self, previous: &gtk::Button, next: &gtk::Button) {
        let weak = self.me.clone();
        self.entry.connect_search_changed(move |_| {
            if let Some(find) = weak.upgrade() {
                find.refresh(true);
            }
        });
        let weak = self.me.clone();
        self.entry.connect_activate(move |_| {
            if let Some(find) = weak.upgrade() {
                find.step(true);
            }
        });
        let weak = self.me.clone();
        self.entry.connect_stop_search(move |_| {
            if let Some(find) = weak.upgrade() {
                find.widget.set_search_mode(false);
            }
        });
        let keys = gtk::EventControllerKey::new();
        keys.set_propagation_phase(gtk::PropagationPhase::Capture);
        let weak = self.me.clone();
        keys.connect_key_pressed(move |_, key, _, mods| {
            let back =
                matches!(key, gdk::Key::Return | gdk::Key::KP_Enter) && mods.contains(gdk::ModifierType::SHIFT_MASK);
            match weak.upgrade() {
                Some(find) if back => {
                    find.step(false);
                    glib::Propagation::Stop
                }
                _ => glib::Propagation::Proceed,
            }
        });
        self.entry.add_controller(keys);
        for (button, forward) in [(previous, false), (next, true)] {
            let weak = self.me.clone();
            button.connect_clicked(move |_| {
                if let Some(find) = weak.upgrade() {
                    find.step(forward);
                }
            });
        }
        let weak = self.me.clone();
        self.widget.connect_search_mode_enabled_notify(move |bar| {
            if let Some(find) = weak.upgrade() {
                if !bar.is_search_mode() {
                    find.closed();
                }
            }
        });
    }

    /// Opens the bar, or takes the person back to its field, its words selected.
    pub fn open(&self) {
        self.widget.set_search_mode(true);
        self.entry.grab_focus();
        self.entry.select_region(0, -1);
        self.refresh(true);
    }

    /// Makes the next or previous match current, opening the bar if it is closed.
    pub fn step(&self, forward: bool) {
        if !self.widget.is_search_mode() {
            self.open();
            return;
        }
        let count = self.found.borrow().len();
        self.current.set(kit::find::step(self.current.get(), count, forward));
        self.show(true);
    }

    /// Finds again in what the page now shows, keeping the current match
    /// where it was; `scroll` brings it into view.
    pub fn refresh(&self, scroll: bool) {
        if !self.widget.is_search_mode() {
            return;
        }
        let query = self.entry.text();
        let was = self.current.get().and_then(|i| self.found.borrow().get(i).map(|m| (m.place, m.chars.0)));
        self.clear();
        let mut found = Vec::new();
        let mut shown = Vec::new();
        texts(&self.column, &mut shown);
        for (place, text) in shown.into_iter().enumerate() {
            if let Text::Surface(surface) = &text {
                let weak = self.me.clone();
                surface.on_fold(Rc::new(move || {
                    if let Some(find) = weak.upgrade() {
                        find.refresh(false);
                    }
                }));
            }
            found.extend(text.find(&query).into_iter().map(|chars| Match { text: text.clone(), place, chars }));
        }
        let places: Vec<(usize, usize)> = found.iter().map(|m| (m.place, m.chars.0)).collect();
        self.current.set(kit::find::keep(was.as_ref(), &places));
        *self.found.borrow_mut() = found;
        self.show(scroll);
    }

    /// Highlights the matches and says how many; `scroll` brings the current into view.
    fn show(&self, scroll: bool) {
        let found = self.found.borrow();
        let current = self.current.get().and_then(|i| found.get(i));
        let mut texts: Vec<&Text> = Vec::new();
        for m in found.iter() {
            if !texts.contains(&&m.text) {
                texts.push(&m.text);
            }
        }
        for text in texts {
            let own: Vec<(usize, usize)> = found.iter().filter(|m| m.text == *text).map(|m| m.chars).collect();
            let strong = current.filter(|m| m.text == *text).map(|m| m.chars);
            match text {
                Text::Surface(surface) => surface.mark_found(&own, strong),
                Text::Label(label) => self.mark_label(label, &own, strong),
            }
        }
        let query = !self.entry.text().is_empty();
        self.count.set_label(&if query { kit::find::label(self.current.get(), found.len()) } else { String::new() });
        if let (true, Some(m)) = (scroll, current) {
            self.reveal(m);
        }
    }

    /// Highlights matches in a label over the attributes it had of its own.
    fn mark_label(&self, label: &gtk::Label, found: &[(usize, usize)], current: Option<(usize, usize)>) {
        let own = {
            let mut marked = self.marked.borrow_mut();
            match marked.iter().find(|(l, _)| l == label) {
                Some((_, own)) => own.clone(),
                None => {
                    let own = label.attributes();
                    marked.push((label.clone(), own.clone()));
                    own
                }
            }
        };
        let list = own.and_then(|l| l.copy()).unwrap_or_default();
        let text = label.text();
        let byte = |c: usize| text.char_indices().nth(c).map_or(text.len(), |(b, _)| b) as u32;
        let add = |mut attr: pango::Attribute, (a, b): (usize, usize)| {
            attr.set_start_index(byte(a));
            attr.set_end_index(byte(b));
            list.insert(attr);
        };
        for m in found {
            add(pango::AttrColor::new_background(0xffff, 0xcccc, 0).into(), *m);
            add(pango::AttrInt::new_background_alpha(0x6000).into(), *m);
        }
        if let Some(m) = current {
            add(pango::AttrColor::new_background(0xffff, 0xc8c8, 0).into(), m);
            add(pango::AttrInt::new_background_alpha(0xffff).into(), m);
            add(pango::AttrColor::new_foreground(0x1c1c, 0x1c1c, 0x1c1c).into(), m);
        }
        label.set_attributes(Some(&list));
    }

    /// Takes every highlight off the page.
    fn clear(&self) {
        for m in self.found.borrow().iter() {
            if let Text::Surface(surface) = &m.text {
                surface.mark_found(&[], None);
            }
        }
        for (label, own) in self.marked.take() {
            label.set_attributes(own.as_ref());
        }
    }

    /// Scrolls the page so the line holding a match shows, a third of the way
    /// down if it was out of view.
    fn reveal(&self, m: &Match) {
        let (widget, (top, bottom)): (gtk::Widget, (f32, f32)) = match &m.text {
            Text::Surface(surface) => (surface.clone().upcast(), surface.line_at(m.chars.0)),
            Text::Label(label) => (label.clone().upcast(), (0.0, label.height() as f32)),
        };
        let point = |y: f32| widget.compute_point(&self.column, &gtk::graphene::Point::new(0.0, y)).map(|p| p.y());
        let (Some(top), Some(bottom)) = (point(top), point(bottom)) else { return };
        let a = self.scroller.vadjustment();
        let (top, bottom) = (f64::from(top), f64::from(bottom));
        if top < a.value() || bottom > a.value() + a.page_size() {
            a.set_value(top - a.page_size() / 3.0);
        }
    }

    /// The bar closed: highlights go, and the current match is selected.
    fn closed(&self) {
        self.clear();
        let found = std::mem::take(&mut *self.found.borrow_mut());
        if let Some(m) = self.current.take().and_then(|i| found.get(i)) {
            match &m.text {
                Text::Surface(surface) => {
                    surface.select(m.chars);
                    surface.grab_focus();
                }
                Text::Label(label) if label.is_selectable() => {
                    label.select_region(m.chars.0 as i32, m.chars.1 as i32);
                    label.grab_focus();
                }
                Text::Label(_) => {}
            }
        }
        self.count.set_label("");
    }
}

/// Words the page shows: a run of its conversation, or a label.
#[derive(Clone, PartialEq)]
enum Text {
    Surface(Surface),
    Label(gtk::Label),
}

impl Text {
    fn find(&self, query: &str) -> Vec<(usize, usize)> {
        match self {
            Text::Surface(surface) => surface.find(query),
            Text::Label(label) => {
                let mut hay = kit::find::Haystack::default();
                hay.words(&label.text(), 0);
                hay.find(&kit::find::needle(query))
            }
        }
    }
}

/// The words shown under `root`, in page order; a control's label is no
/// words of the page.
fn texts(root: &impl IsA<gtk::Widget>, out: &mut Vec<Text>) {
    let mut child = root.first_child();
    while let Some(widget) = child {
        child = widget.next_sibling();
        if !widget.is_visible() || !widget.is_child_visible() {
            continue;
        }
        if let Some(surface) = widget.downcast_ref::<Surface>() {
            out.push(Text::Surface(surface.clone()));
        } else if let Some(label) = widget.downcast_ref::<gtk::Label>() {
            out.push(Text::Label(label.clone()));
            continue;
        } else if widget.is::<gtk::Button>()
            || widget.is::<gtk::MenuButton>()
            || widget.is::<gtk::DropDown>()
            || widget.is::<gtk::Editable>()
            || widget.is::<gtk::TextView>()
        {
            continue;
        }
        // A table or card set in a run of the conversation is searched after its run.
        texts(&widget, out);
    }
}
