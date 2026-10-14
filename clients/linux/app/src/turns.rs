//! A conversation's turns, kept in place across reads. Each turn has a key
//! that changes when what it shows changes; a read rebuilds only from the
//! first turn whose key differs, so what a reader opened, selected or
//! scrolled past stays as it was, and new content is appended at the end.
//! Turns of text run together into one surface, selectable as one; a turn
//! that is a widget, an interactive card, sits between surfaces.

use std::cell::RefCell;
use std::collections::hash_map::DefaultHasher;
use std::fmt::Debug;
use std::hash::{Hash, Hasher};

use adw::prelude::*;
use gtk::gio;

use crate::surface::{Rich, Surface};
use crate::ui;

pub struct Turn {
    key: String,
    /// What the turn says, as the markdown it was written in, for its menu's Copy.
    copy: Option<String>,
    build: Build,
}

enum Build {
    Widget(Box<dyn FnOnce() -> gtk::Widget>),
    Text(Box<dyn FnOnce() -> Rich>),
}

/// What a turn is named and shows, before it is built.
pub struct Key(String, Option<String>);

impl Key {
    /// The turn copies as `said`: as plain text, or as the markdown it is.
    pub fn copying(self, said: &str) -> Key {
        Key(self.0, Some(said.to_string()))
    }

    /// A turn that is a widget of its own: an interactive card.
    pub fn with(self, build: impl FnOnce() -> gtk::Widget + 'static) -> Turn {
        Turn { key: self.0, copy: self.1, build: Build::Widget(Box::new(build)) }
    }

    /// A turn of text, written into the surface it runs on in.
    pub fn text(self, build: impl FnOnce() -> Rich + 'static) -> Turn {
        Turn { key: self.0, copy: self.1, build: Build::Text(Box::new(build)) }
    }
}

impl Turn {
    /// The key of a turn named `id`, rebuilt whenever `shown` changes. Ids
    /// hold no `:`.
    pub fn key(id: impl std::fmt::Display, shown: &impl Debug) -> Key {
        let mut hasher = DefaultHasher::new();
        format!("{shown:?}").hash(&mut hasher);
        Key(format!("{id}:{:x}", hasher.finish()), None)
    }

    /// The turn's text, for a turn shown inside another; none for a widget.
    pub fn rich(self) -> Option<Rich> {
        match self.build {
            Build::Text(build) => Some(build()),
            Build::Widget(_) => None,
        }
    }

    fn id(&self) -> &str {
        self.key.split(':').next().unwrap_or("")
    }
}

/// Where a shown turn is: a widget of its own, or the n-th turn of a surface.
enum Placed {
    Widget(gtk::Widget),
    Text(Surface, usize),
}

pub struct Turns {
    pub widget: gtk::Box,
    shown: RefCell<Vec<(String, Placed)>>,
}

impl Turns {
    pub fn new(spacing: i32) -> Turns {
        Turns { widget: gtk::Box::new(gtk::Orientation::Vertical, spacing), shown: RefCell::new(Vec::new()) }
    }

    pub fn update(&self, turns: Vec<Turn>) {
        let mut shown = self.shown.borrow_mut();
        let keep = shown.iter().zip(&turns).take_while(|((key, _), turn)| *key == turn.key).count();
        let mut cut: Vec<Surface> = Vec::new();
        for (_, placed) in shown.drain(keep..) {
            match placed {
                Placed::Widget(widget) => self.widget.remove(&widget),
                Placed::Text(surface, n) => {
                    if cut.contains(&surface) {
                        continue;
                    }
                    if n == 0 {
                        self.widget.remove(&surface);
                    } else {
                        surface.truncate(n);
                    }
                    cut.push(surface);
                }
            }
        }
        let mut written: Vec<Surface> = Vec::new();
        for turn in turns.into_iter().skip(keep) {
            let id = turn.id().to_string();
            let key = turn.key.clone();
            match turn.build {
                Build::Widget(build) => {
                    let widget = build();
                    widget.set_widget_name(&id);
                    let menus = turn.copy.map(|said| copy_menus(&widget, said));
                    ui::copyable(&widget, menus.as_ref().map(|m| &m.1));
                    if let Some((menu, _)) = &menus {
                        ui::context_menu(&widget, menu);
                    }
                    self.widget.append(&widget);
                    shown.push((key, Placed::Widget(widget)));
                }
                Build::Text(build) => {
                    let last = self.widget.last_child().and_downcast::<Surface>();
                    let surface = last.unwrap_or_else(|| {
                        let surface = Surface::new();
                        self.widget.append(&surface);
                        surface
                    });
                    let n = surface.len();
                    surface.append(&id, build(), turn.copy);
                    if !written.contains(&surface) {
                        written.push(surface.clone());
                    }
                    shown.push((key, Placed::Text(surface, n)));
                }
            }
        }
        for surface in cut.iter().chain(&written) {
            surface.settle();
        }
    }

    /// The widget showing the turn named `id`, and how far down it the turn starts.
    pub fn find(&self, id: &str) -> Option<(gtk::Widget, f32)> {
        self.shown.borrow().iter().find(|(key, _)| key.split(':').next() == Some(id)).and_then(|(_, placed)| {
            match placed {
                Placed::Widget(w) => Some((w.clone(), 0.0)),
                Placed::Text(surface, _) => surface.locate(id).map(|y| (surface.clone().upcast(), y)),
            }
        })
    }
}

/// The turn's Copy and Copy as Markdown, acting on what it said: its own
/// menu, and the same beside a label's Copy of what is selected.
fn copy_menus(widget: &gtk::Widget, said: String) -> (gio::MenuModel, gio::MenuModel) {
    let actions = gio::SimpleActionGroup::new();
    let plain = kit::copy::plain(&said);
    for (name, text) in [("copy", plain), ("copy-markdown", said)] {
        let action = gio::SimpleAction::new(name, None);
        let target = widget.downgrade();
        action.connect_activate(move |_, _| {
            if let Some(target) = target.upgrade() {
                ui::copy_text(&target, &text);
            }
        });
        actions.add_action(&action);
    }
    widget.insert_action_group("turn", Some(&actions));
    let menu = |copy: &str, markdown: &str| {
        let menu = gio::Menu::new();
        menu.append(Some(copy), Some("turn.copy"));
        menu.append(Some(markdown), Some("turn.copy-markdown"));
        menu.upcast()
    };
    (menu("Copy", "Copy as Markdown"), menu("Copy Message", "Copy Message as Markdown"))
}
