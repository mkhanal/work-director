//! The one place a person types to Work Director: a new goal and a running
//! one read the same. The key that sends is the person's choice.

use std::cell::{Cell, RefCell};
use std::path::{Path, PathBuf};
use std::rc::Rc;

use adw::prelude::*;
use gtk::{gdk, gio, glib};

use kit::{Chord, PendingFile, ReturnPress};

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
    /// Who a goal's message goes to: the whole goal or one sub-goal.
    pub to: gtk::DropDown,
    note: gtk::Label,
    /// The files waiting to go with the next message, shown above the field.
    files: RefCell<Vec<PendingFile>>,
    tray: gtk::Box,
    pasted: Cell<u32>,
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
        let to = gtk::DropDown::from_strings(&[]);
        to.add_css_class("flat");
        to.add_css_class("to-button");
        to.set_valign(gtk::Align::Center);
        to.set_tooltip_text(Some("Who this message goes to"));
        to.set_visible(false);
        let bar = ui::hbox(SPACE_2);
        bar.append(&chips);
        bar.append(&to);
        bar.append(&remember);
        bar.append(&stop);
        bar.append(&send);

        let tray = ui::hbox(SPACE_2);
        tray.set_halign(gtk::Align::Start);
        tray.set_visible(false);
        tray.add_css_class("attachments");
        let card = ui::vbox(SPACE_2);
        card.add_css_class("composer");
        card.append(&tray);
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
            to,
            note,
            files: RefCell::new(Vec::new()),
            tray,
            pasted: Cell::new(0),
            on_send: Rc::new(RefCell::new(None)),
            on_stop: Rc::new(RefCell::new(None)),
        });

        let hinted = settings.clone();
        let hint_now = move || hint.set_label(&format!("{} · Esc Esc stops", hinted.send_key().hint()));
        hint_now();

        let buffer = composer.view.buffer();
        let weak = Rc::downgrade(&composer);
        buffer.connect_changed(move |b| {
            if let Some(composer) = weak.upgrade() {
                composer.placeholder.set_visible(b.char_count() == 0);
                composer.refresh_send();
            }
        });

        // Ctrl+V of an image or of files attaches them; of text, pastes it.
        let paste = gtk::EventControllerKey::new();
        paste.set_propagation_phase(gtk::PropagationPhase::Capture);
        let weak = Rc::downgrade(&composer);
        paste.connect_key_pressed(move |_, key, _, state| {
            let pasting = state.contains(gdk::ModifierType::CONTROL_MASK) && matches!(key, gdk::Key::v | gdk::Key::V);
            let Some(composer) = weak.upgrade().filter(|_| pasting) else { return glib::Propagation::Proceed };
            if composer.paste() {
                glib::Propagation::Stop
            } else {
                glib::Propagation::Proceed
            }
        });
        composer.view.add_controller(paste);

        let actions = gio::SimpleActionGroup::new();
        let remove = gio::SimpleAction::new("remove", Some(glib::VariantTy::STRING));
        let weak = Rc::downgrade(&composer);
        remove.connect_activate(move |_, path| {
            if let (Some(composer), Some(path)) = (weak.upgrade(), path.and_then(|p| p.get::<String>())) {
                composer.remove_file(&path);
            }
        });
        actions.add_action(&remove);
        composer.widget.insert_action_group("composer", Some(&actions));

        let drop = gtk::DropTarget::new(gdk::FileList::static_type(), gdk::DragAction::COPY);
        let weak = Rc::downgrade(&composer);
        drop.connect_drop(move |_, value, _, _| {
            let (Some(composer), Ok(list)) = (weak.upgrade(), value.get::<gdk::FileList>()) else { return false };
            composer.attach(&list.files());
            true
        });
        card.add_controller(drop);

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
        if text.trim().is_empty() && self.files.borrow().is_empty() {
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
        self.files.borrow_mut().clear();
        self.show_files();
    }

    /// The paths of the files waiting to be sent.
    pub fn files(&self) -> Vec<String> {
        self.files.borrow().iter().map(|f| f.path.clone()).collect()
    }

    fn refresh_send(&self) {
        let empty = self.text().trim().is_empty() && self.files.borrow().is_empty();
        self.send.set_sensitive(self.view.is_sensitive() && !empty);
    }

    /// Attaches what the clipboard holds when it is an image or files;
    /// whether it did, so text is pasted as text.
    fn paste(self: &Rc<Self>) -> bool {
        let clipboard = self.view.clipboard();
        // Another app offers types by name; what they can be read as is what counts.
        let formats = clipboard.formats().union_deserialize_types();
        let weak = Rc::downgrade(self);
        if formats.contains_type(gdk::FileList::static_type()) {
            clipboard.read_value_async(
                gdk::FileList::static_type(),
                glib::Priority::DEFAULT,
                gio::Cancellable::NONE,
                move |read| {
                    let Some(composer) = weak.upgrade() else { return };
                    match read.map(|v| v.get::<gdk::FileList>()) {
                        Ok(Ok(list)) => composer.attach(&list.files()),
                        Ok(Err(e)) => composer.set_note(Some((&format!("cannot paste those files: {e}"), true))),
                        Err(e) => composer.set_note(Some((&format!("cannot paste those files: {e}"), true))),
                    }
                },
            );
            return true;
        }
        if formats.contains_type(gdk::Texture::static_type()) {
            clipboard.read_texture_async(gio::Cancellable::NONE, move |read| {
                let Some(composer) = weak.upgrade() else { return };
                match read {
                    Ok(Some(texture)) => composer.attach_image(&texture),
                    Ok(None) => {}
                    Err(e) => composer.set_note(Some((&format!("cannot paste that image: {e}"), true))),
                }
            });
            return true;
        }
        false
    }

    /// A pasted image, written as a PNG of its own to send.
    fn attach_image(&self, texture: &gdk::Texture) {
        let n = self.pasted.get() + 1;
        self.pasted.set(n);
        let dir = glib::user_cache_dir().join("work-director").join("pasted");
        let stamp = chrono::Utc::now().format("%Y%m%d-%H%M%S");
        let path = dir.join(format!("pasted-{stamp}-{n}.png"));
        let saved = std::fs::create_dir_all(&dir)
            .map_err(|e| e.to_string())
            .and_then(|_| texture.save_to_png(&path).map_err(|e| e.to_string()));
        match saved {
            Ok(()) => self.attach_paths(&[path]),
            Err(e) => self.set_note(Some((&format!("cannot keep the pasted image: {e}"), true))),
        }
    }

    fn attach(&self, files: &[gio::File]) {
        let paths: Vec<PathBuf> = files.iter().filter_map(gio::File::path).collect();
        if paths.len() < files.len() {
            self.set_note(Some(("only files on this computer can be attached", true)));
        }
        self.attach_paths(&paths);
    }

    fn attach_paths(&self, paths: &[PathBuf]) {
        for path in paths {
            match PendingFile::at(Path::new(path)) {
                Ok(file) => kit::attach::add(&mut self.files.borrow_mut(), file),
                Err(e) => self.set_note(Some((&e.to_string(), true))),
            }
        }
        self.show_files();
    }

    fn remove_file(&self, path: &str) {
        self.files.borrow_mut().retain(|f| f.path != path);
        self.show_files();
    }

    /// The files waiting, each removable: an image as a thumbnail, any other
    /// file by its name and size.
    fn show_files(&self) {
        ui::clear(&self.tray);
        let files = self.files.borrow().clone();
        self.tray.set_visible(!files.is_empty());
        for file in &files {
            let chip = ui::hbox(SPACE_2);
            chip.add_css_class("attachment-chip");
            if file.image {
                let picture = gtk::Picture::for_filename(&file.path);
                picture.set_content_fit(gtk::ContentFit::Cover);
                picture.set_size_request(48, 48);
                picture.set_can_shrink(true);
                picture.set_tooltip_text(Some(&format!("{} · {}", file.name, file.size_label())));
                picture.update_property(&[gtk::accessible::Property::Label(&file.name)]);
                chip.append(&picture);
            } else {
                chip.append(&ui::icon("text-x-generic-symbolic", &["text2"]));
                let words = ui::vbox(0);
                let name = ui::name_line(&file.name, &["callout"]);
                words.append(&name);
                words.append(&ui::text(&file.size_label(), &["meta", "text3"]));
                chip.append(&words);
            }
            let remove = gtk::Button::from_icon_name("window-close-symbolic");
            remove.add_css_class("flat");
            remove.add_css_class("circular");
            remove.set_valign(gtk::Align::Start);
            remove.set_tooltip_text(Some(&format!("Remove {}", file.name)));
            ui::announce(&remove, &format!("Remove {}", file.name));
            remove.set_action_name(Some("composer.remove"));
            remove.set_action_target_value(Some(&file.path.to_variant()));
            chip.append(&remove);
            self.tray.append(&chip);
        }
        self.refresh_send();
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
        self.refresh_send();
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
