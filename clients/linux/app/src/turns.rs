//! A conversation's turns, kept in place across reads. Each turn has a key
//! that changes when what it shows changes; a read rebuilds only from the
//! first turn whose key differs, so what a reader opened or scrolled past
//! stays as it was, and new content is appended at the end.

use std::cell::RefCell;
use std::collections::hash_map::DefaultHasher;
use std::fmt::Debug;
use std::hash::{Hash, Hasher};

use adw::prelude::*;
use gtk::gio;

use crate::ui;

pub struct Turn {
    key: String,
    /// What the turn says, as the markdown it was written in, for its menu's Copy.
    copy: Option<String>,
    build: Box<dyn FnOnce() -> gtk::Widget>,
}

/// What a turn is named and shows, before it is built.
pub struct Key(String, Option<String>);

impl Key {
    /// The turn copies as `said`: as plain text, or as the markdown it is.
    pub fn copying(self, said: &str) -> Key {
        Key(self.0, Some(said.to_string()))
    }

    pub fn with(self, build: impl FnOnce() -> gtk::Widget + 'static) -> Turn {
        Turn { key: self.0, copy: self.1, build: Box::new(build) }
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

    /// The turn's widget, for a turn shown inside another.
    pub fn build(self) -> gtk::Widget {
        (self.build)()
    }

    fn id(&self) -> &str {
        self.key.split(':').next().unwrap_or("")
    }
}

pub struct Turns {
    pub widget: gtk::Box,
    shown: RefCell<Vec<(String, gtk::Widget)>>,
}

impl Turns {
    pub fn new(spacing: i32) -> Turns {
        Turns { widget: gtk::Box::new(gtk::Orientation::Vertical, spacing), shown: RefCell::new(Vec::new()) }
    }

    pub fn update(&self, turns: Vec<Turn>) {
        let mut shown = self.shown.borrow_mut();
        let keep = shown.iter().zip(&turns).take_while(|((key, _), turn)| *key == turn.key).count();
        for (_, widget) in shown.drain(keep..) {
            self.widget.remove(&widget);
        }
        for turn in turns.into_iter().skip(keep) {
            let id = turn.id().to_string();
            let key = turn.key.clone();
            let widget = (turn.build)();
            widget.set_widget_name(&id);
            let menus = turn.copy.map(|said| copy_menus(&widget, said));
            ui::copyable(&widget, menus.as_ref().map(|m| &m.1));
            if let Some((menu, _)) = &menus {
                ui::context_menu(&widget, menu);
            }
            self.widget.append(&widget);
            shown.push((key, widget));
        }
    }

    /// The widget showing the turn named `id`.
    pub fn find(&self, id: &str) -> Option<gtk::Widget> {
        self.shown.borrow().iter().find(|(key, _)| key.split(':').next() == Some(id)).map(|(_, w)| w.clone())
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
