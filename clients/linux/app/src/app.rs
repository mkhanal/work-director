//! What every page shares: the store, the window to put dialogs and toasts
//! on, and the one way a write runs.

use std::cell::RefCell;
use std::future::Future;
use std::rc::Rc;

use adw::prelude::*;
use gtk::{gio, glib};

use kit::{Link, Plan, PlanError, Store, Work, WorkState};

use crate::settings::Settings;
use crate::ui;

type Opener = Rc<dyn Fn(Place)>;
type PathShower = Rc<dyn Fn(&[Link])>;

/// What the main pane shows.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Place {
    NewGoal,
    Work(String),
    Archived,
    Taste,
    Review(String),
}

#[derive(Clone)]
pub struct App {
    pub store: Rc<Store>,
    pub settings: Rc<Settings>,
    pub window: adw::ApplicationWindow,
    pub toasts: adw::ToastOverlay,
    /// The split that holds the sidebar, so a page can show or hide it.
    pub sidebar_split: adw::OverlaySplitView,
    opener: Rc<RefCell<Option<Opener>>>,
    path: Rc<RefCell<Option<PathShower>>>,
}

impl App {
    pub fn new(
        store: Rc<Store>,
        settings: Rc<Settings>,
        window: adw::ApplicationWindow,
        toasts: adw::ToastOverlay,
        sidebar_split: adw::OverlaySplitView,
    ) -> App {
        App {
            store,
            settings,
            window,
            toasts,
            sidebar_split,
            opener: Rc::new(RefCell::new(None)),
            path: Rc::new(RefCell::new(None)),
        }
    }

    /// Sets what opening a place does; the window sets it once it is built.
    pub fn set_opener(&self, open: impl Fn(Place) + 'static) {
        *self.opener.borrow_mut() = Some(Rc::new(open));
    }

    pub fn open(&self, place: Place) {
        let open = self.opener.borrow().clone();
        if let Some(open) = open {
            open(place);
        }
    }

    /// Sets what showing an open goal's breadcrumb does; the window sets it.
    pub fn set_path_shower(&self, show: impl Fn(&[Link]) + 'static) {
        *self.path.borrow_mut() = Some(Rc::new(show));
    }

    /// Shows the breadcrumb of the goal open, top goal first.
    pub fn show_path(&self, chain: &[Link]) {
        let show = self.path.borrow().clone();
        if let Some(show) = show {
            show(chain);
        }
    }

    pub fn spawn(&self, f: impl Future<Output = ()> + 'static) {
        glib::spawn_future_local(f);
    }

    pub fn toast(&self, text: &str) {
        self.toasts.add_toast(adw::Toast::new(&ui::escaped(text)));
    }

    /// A failure stays until it is dismissed: it is the one thing a person
    /// must not miss.
    pub fn complain(&self, text: &str) {
        let toast = adw::Toast::new(&ui::escaped(text));
        toast.set_timeout(0);
        self.toasts.add_toast(toast);
    }

    /// Runs a write at once and shows the command it ran, with an undo when
    /// there is one; a refusal says why.
    pub fn run(&self, plan: Plan) {
        let app = self.clone();
        self.spawn(async move {
            match app.store.run(&plan).await {
                Ok(result) if result.code == 0 => {
                    let toast = adw::Toast::new(&ui::escaped(&plan.command_line()));
                    if let Some(undo) = plan.undo() {
                        toast.set_button_label(Some("Undo"));
                        let again = app.clone();
                        toast.connect_button_clicked(move |_| again.run(undo.clone()));
                    }
                    app.toasts.add_toast(toast);
                }
                Ok(result) => app.complain(&format!("{} failed: {}", plan.command_line(), result.complaint())),
                Err(e) => app.complain(&format!("{} failed: {e}", plan.command_line())),
            }
            app.store.refresh().await;
        });
    }

    /// Runs writes one after another, showing each command it ran and
    /// stopping at the first refused; whether every one ran.
    pub async fn run_in_turn(&self, plans: &[Plan]) -> bool {
        let mut all = true;
        for plan in plans {
            match self.store.run(plan).await {
                Ok(result) if result.code == 0 => self.toast(&plan.command_line()),
                Ok(result) => {
                    self.complain(&format!("{} failed: {}", plan.command_line(), result.complaint()));
                    all = false;
                }
                Err(e) => {
                    self.complain(&format!("{} failed: {e}", plan.command_line()));
                    all = false;
                }
            }
            if !all {
                break;
            }
        }
        self.store.refresh().await;
        all
    }

    /// Asks for the words a write needs, then runs it.
    pub fn ask(
        &self,
        heading: &str,
        body: &str,
        action: &str,
        initial: &str,
        plan: impl Fn(&str) -> Result<Plan, PlanError> + 'static,
    ) {
        let dialog = adw::AlertDialog::new(Some(heading), Some(body));
        dialog.add_responses(&[("cancel", "Cancel"), ("go", action)]);
        dialog.set_response_appearance("go", adw::ResponseAppearance::Suggested);
        dialog.set_default_response(Some("go"));
        dialog.set_close_response("cancel");
        let entry = gtk::Entry::builder().text(initial).activates_default(true).build();
        dialog.set_extra_child(Some(&entry));
        let (app, field) = (self.clone(), entry.clone());
        dialog.connect_response(None, move |_, response| {
            if response != "go" {
                return;
            }
            match plan(&field.text()) {
                Ok(plan) => app.run(plan),
                Err(e) => app.complain(&e.to_string()),
            }
        });
        dialog.present(Some(&self.window));
        // Typing goes into the field, not onto the default button.
        dialog.set_focus(Some(&entry));
    }

    /// Asks for a decision on work, in one line.
    pub fn ask_decision(&self, work: &Work) {
        let id = work.id.clone();
        self.ask(
            &format!("Record a decision on {}", work.title),
            "The decision, in one line.",
            "Record",
            "",
            move |t| Plan::decide(&id, t),
        );
    }

    /// Asks what reopened work is for.
    pub fn ask_reopen(&self, work: &Work) {
        let id = work.id.clone();
        self.ask(
            &format!("Reopen {}", work.title),
            "What is being worked on? A question about finished work is answered without reopening it.",
            "Reopen",
            "",
            move |t| Plan::reopen(&id, t),
        );
    }

    /// Runs a wd command typed as text, split without a shell.
    pub fn ask_command(&self) {
        self.ask("Run a wd command", "Typed as you would in a terminal; nothing is expanded.", "Run", "", |t| {
            Plan::typed(t)
        });
    }
}

/// A plan as a menu item that runs it through `win.run`.
fn plan_item(plan: &Plan) -> gio::MenuItem {
    let item = gio::MenuItem::new(Some(&plan.title), None);
    item.set_action_and_target_value(Some("win.run"), Some(&(plan.title.clone(), plan.argv.clone()).to_variant()));
    item
}

fn id_item(label: &str, action: &str, id: &str) -> gio::MenuItem {
    let item = gio::MenuItem::new(Some(label), None);
    item.set_action_and_target_value(Some(action), Some(&id.to_variant()));
    item
}

/// Every write work's state allows: a right-click on the work wherever it is
/// listed, and the page's own menu, show the same ones.
pub fn work_menu(work: &Work, opened: bool) -> gio::Menu {
    let menu = gio::Menu::new();
    let first = gio::Menu::new();
    if !opened {
        first.append_item(&id_item("Open", "win.open", &work.id));
    }
    first.append_item(&id_item("Copy Title and Id", "win.copy-text", &kit::copy::work(work)));
    first.append_item(&id_item("Rename…", "win.rename", &work.id));
    first.append_item(&id_item("Record a Decision…", "win.decide", &work.id));
    menu.append_section(None, &first);

    let state = gio::Menu::new();
    if matches!(work.state, WorkState::Paused | WorkState::Dropped) {
        state.append_item(&plan_item(&Plan::resume(&work.id, "")));
    } else if work.live() {
        state.append_item(&plan_item(&Plan::pause(&work.id)));
    }
    if work.kind.is_goal() && !work.at_rest() && work.state != WorkState::Paused {
        state.append_item(&plan_item(&Plan::drive(&work.id)));
    }
    if work.state == WorkState::Review {
        state.append_item(&plan_item(&Plan::verify(&work.id)));
        state.append_item(&plan_item(&Plan::soft_done(&work.id)));
    }
    if work.state == WorkState::SoftDone {
        state.append_item(&plan_item(&Plan::done(&work.id)));
    }
    if work.state == WorkState::Done {
        state.append_item(&id_item("Reopen…", "win.reopen", &work.id));
    }
    if work.has_session() {
        state.append_item(&plan_item(&Plan::open(&work.id)));
    }
    menu.append_section(None, &state);

    let end = gio::Menu::new();
    if work.live() {
        end.append_item(&plan_item(&Plan::cancel(&work.id, "")));
    }
    if work.archived.is_some() {
        end.append_item(&plan_item(&Plan::unarchive(&work.id)));
    } else {
        end.append_item(&plan_item(&Plan::archive(work, "")));
    }
    menu.append_section(None, &end);
    menu
}
