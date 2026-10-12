//! The one place a person types to Work Director: a new goal and a running
//! one read the same. The key that sends is the person's choice.

use std::cell::RefCell;
use std::rc::Rc;

use adw::prelude::*;
use gtk::{gdk, glib};

use kit::{Chord, ReturnPress};

use crate::settings::Settings;
use crate::ui::{self, SPACE_2};

const LINE: i32 = 22;

type Sender = Rc<dyn Fn(String)>;
type Stopper = Rc<dyn Fn()>;

pub struct Composer {
    pub widget: gtk::Box,
    view: gtk::TextView,
    placeholder: gtk::Label,
    send: gtk::Button,
    stop: gtk::Button,
    remember: gtk::ToggleButton,
    chips: gtk::Box,
    note: gtk::Label,
    on_send: Rc<RefCell<Option<Sender>>>,
    on_stop: Rc<RefCell<Option<Stopper>>>,
}

impl Composer {
    /// A composer whose field grows from `min` to `max` lines, then scrolls.
    pub fn new(settings: Rc<Settings>, min: i32, max: i32) -> Rc<Composer> {
        let view = gtk::TextView::builder()
            .wrap_mode(gtk::WrapMode::WordChar)
            .accepts_tab(false)
            .top_margin(2)
            .bottom_margin(2)
            .build();
        view.add_css_class("composer-field");
        let placeholder = ui::text("", &["body", "text3"]);
        placeholder.set_valign(gtk::Align::Start);
        placeholder.set_can_target(false);
        let field = gtk::Overlay::new();
        field.set_child(Some(&view));
        field.add_overlay(&placeholder);
        let scroller = gtk::ScrolledWindow::builder()
            .hscrollbar_policy(gtk::PolicyType::Never)
            .propagate_natural_height(true)
            .min_content_height(min * LINE)
            .max_content_height(max * LINE)
            .child(&field)
            .build();

        let chips = ui::hbox(SPACE_2);
        chips.set_hexpand(true);
        chips.set_valign(gtk::Align::Center);
        let remember = gtk::ToggleButton::builder()
            .icon_name("bookmark-new-symbolic")
            .tooltip_text("Remember: also keep what you say as feedback, to learn from")
            .build();
        remember.add_css_class("flat");
        let stop =
            gtk::Button::builder().icon_name("media-playback-stop-symbolic").tooltip_text("Stop (Esc Esc)").build();
        stop.add_css_class("flat");
        stop.set_visible(false);
        let send = gtk::Button::builder().icon_name("go-up-symbolic").tooltip_text("Send").sensitive(false).build();
        send.add_css_class("send");
        send.add_css_class("circular");
        send.add_css_class("suggested-action");
        let bar = ui::hbox(SPACE_2);
        bar.append(&chips);
        bar.append(&remember);
        bar.append(&stop);
        bar.append(&send);

        let card = ui::vbox(SPACE_2);
        card.add_css_class("composer");
        card.append(&scroller);
        card.append(&bar);

        let note = ui::text("", &["meta"]);
        note.set_visible(false);
        let hint = ui::line("", &["micro", "text3"]);
        hint.set_halign(gtk::Align::End);
        hint.set_xalign(1.0);
        let widget = ui::vbox(SPACE_2);
        widget.append(&card);
        widget.append(&note);
        widget.append(&hint);

        let composer = Rc::new(Composer {
            widget,
            view,
            placeholder,
            send,
            stop,
            remember,
            chips,
            note,
            on_send: Rc::new(RefCell::new(None)),
            on_stop: Rc::new(RefCell::new(None)),
        });

        let hinted = settings.clone();
        let hint_now = move || hint.set_label(&format!("{} · Esc Esc stops", hinted.send_key().hint()));
        hint_now();

        let buffer = composer.view.buffer();
        let (placeholder, send) = (composer.placeholder.clone(), composer.send.clone());
        buffer.connect_changed(move |b| {
            let empty = b.text(&b.start_iter(), &b.end_iter(), false).trim().is_empty();
            placeholder.set_visible(b.char_count() == 0);
            send.set_sensitive(!empty);
        });

        let keys = gtk::EventControllerKey::new();
        keys.set_propagation_phase(gtk::PropagationPhase::Capture);
        let weak = Rc::downgrade(&composer);
        keys.connect_key_pressed(move |_, key, _, state| {
            if key != gdk::Key::Return && key != gdk::Key::KP_Enter {
                return glib::Propagation::Proceed;
            }
            hint_now();
            let held = Chord {
                shift: state.contains(gdk::ModifierType::SHIFT_MASK),
                control: state.contains(gdk::ModifierType::CONTROL_MASK),
                alt: state.contains(gdk::ModifierType::ALT_MASK),
                super_key: state.contains(gdk::ModifierType::SUPER_MASK),
            };
            match (settings.send_key().press(held), weak.upgrade()) {
                (ReturnPress::Send, Some(composer)) => {
                    composer.submit();
                    glib::Propagation::Stop
                }
                (ReturnPress::Send, None) => glib::Propagation::Stop,
                (ReturnPress::Newline, _) => {
                    // A chord the text view would not take as a newline still makes one.
                    if held != Chord::NONE && held != Chord::SHIFT {
                        if let Some(composer) = weak.upgrade() {
                            composer.view.buffer().insert_at_cursor("\n");
                        }
                        return glib::Propagation::Stop;
                    }
                    glib::Propagation::Proceed
                }
            }
        });
        composer.view.add_controller(keys);

        let weak = Rc::downgrade(&composer);
        composer.send.connect_clicked(move |_| {
            if let Some(composer) = weak.upgrade() {
                composer.submit();
            }
        });
        let stopper = Rc::clone(&composer.on_stop);
        composer.stop.connect_clicked(move |_| {
            let stop = stopper.borrow().clone();
            if let Some(stop) = stop {
                stop();
            }
        });
        composer
    }

    pub fn connect_send(&self, f: impl Fn(String) + 'static) {
        *self.on_send.borrow_mut() = Some(Rc::new(f));
    }

    pub fn connect_stop(&self, f: impl Fn() + 'static) {
        *self.on_stop.borrow_mut() = Some(Rc::new(f));
    }

    fn submit(&self) {
        let text = self.text();
        if text.trim().is_empty() {
            return;
        }
        let send = self.on_send.borrow().clone();
        if let Some(send) = send {
            send(text);
        }
    }

    pub fn text(&self) -> String {
        let buffer = self.view.buffer();
        buffer.text(&buffer.start_iter(), &buffer.end_iter(), false).to_string()
    }

    pub fn set_text(&self, text: &str) {
        self.view.buffer().set_text(text);
    }

    pub fn clear(&self) {
        self.set_text("");
    }

    pub fn focus(&self) {
        self.view.grab_focus();
    }

    pub fn set_placeholder(&self, text: &str) {
        self.placeholder.set_label(text);
    }

    /// Whether something the composer speaks to is working, and so can be stopped.
    pub fn set_working(&self, working: bool) {
        self.stop.set_visible(working);
    }

    pub fn set_busy(&self, busy: bool) {
        self.view.set_sensitive(!busy);
        self.send.set_sensitive(!busy && !self.text().trim().is_empty());
    }

    /// Context chips: runner, model, the folder it works in.
    pub fn set_chips(&self, chips: &[String]) {
        ui::clear(&self.chips);
        for chip in chips {
            self.chips.append(&ui::tag(chip, &["meta", "text2"]));
        }
    }

    pub fn set_remember_offered(&self, offered: bool) {
        self.remember.set_visible(offered);
        if !offered {
            self.remember.set_active(false);
        }
    }

    pub fn remember(&self) -> bool {
        self.remember.is_visible() && self.remember.is_active()
    }

    /// A line under the composer: what happened, red when it went wrong.
    pub fn set_note(&self, note: Option<(&str, bool)>) {
        match note {
            Some((text, failed)) => {
                self.note.set_label(text);
                if failed {
                    self.note.add_css_class("failure");
                } else {
                    self.note.remove_css_class("failure");
                }
                self.note.set_visible(true);
            }
            None => self.note.set_visible(false),
        }
    }
}
