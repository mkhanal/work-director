//! A conversation's turns, kept in place across reads. Each turn has a key
//! that changes when what it shows changes; a read rebuilds only from the
//! first turn whose key differs, so what a reader opened or scrolled past
//! stays as it was, and new content is appended at the end.

use std::cell::RefCell;
use std::collections::hash_map::DefaultHasher;
use std::fmt::Debug;
use std::hash::{Hash, Hasher};

use adw::prelude::*;

pub struct Turn {
    key: String,
    build: Box<dyn FnOnce() -> gtk::Widget>,
}

/// What a turn is named and shows, before it is built.
pub struct Key(String);

impl Key {
    pub fn with(self, build: impl FnOnce() -> gtk::Widget + 'static) -> Turn {
        Turn { key: self.0, build: Box::new(build) }
    }
}

impl Turn {
    /// The key of a turn named `id`, rebuilt whenever `shown` changes. Ids
    /// hold no `:`.
    pub fn key(id: impl std::fmt::Display, shown: &impl Debug) -> Key {
        let mut hasher = DefaultHasher::new();
        format!("{shown:?}").hash(&mut hasher);
        Key(format!("{id}:{:x}", hasher.finish()))
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
            self.widget.append(&widget);
            shown.push((key, widget));
        }
    }

    /// The widget showing the turn named `id`.
    pub fn find(&self, id: &str) -> Option<gtk::Widget> {
        self.shown.borrow().iter().find(|(key, _)| key.split(':').next() == Some(id)).map(|(_, w)| w.clone())
    }
}
