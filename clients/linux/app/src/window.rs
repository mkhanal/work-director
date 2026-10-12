//! The window: goals as threads on the left, the place chosen in the middle,
//! with its agents beside it.

use std::cell::RefCell;
use std::rc::{Rc, Weak};

use adw::prelude::*;
use gtk::{gio, glib};

use kit::{Plan, SendKey, Store};

use crate::app::{App, Place};
use crate::new_goal_page::NewGoalPage;
use crate::pages;
use crate::settings::Settings;
use crate::sidebar::{band_named, Sidebar};
use crate::ui;
use crate::work_page::WorkPage;

type IdAction = Box<dyn Fn(&Window, String)>;

/// The page in the main pane.
struct Current {
    /// The work page shown, to rename in place.
    work: Option<Rc<WorkPage>>,
    /// What the page needs kept alive while it shows.
    _page: Box<dyn std::any::Any>,
}

pub struct Window {
    pub app: App,
    me: Weak<Window>,
    sidebar: Rc<Sidebar>,
    current: RefCell<Current>,
    banner: adw::Banner,
    watch: RefCell<Option<kit::Watch>>,
}

impl Window {
    pub fn new(application: &adw::Application, store: Rc<Store>, settings: Rc<Settings>) -> Rc<Window> {
        let window = adw::ApplicationWindow::builder()
            .application(application)
            .title("Work Director")
            .default_width(1280)
            .default_height(820)
            .width_request(360)
            .height_request(480)
            .build();
        let split = adw::OverlaySplitView::builder()
            .min_sidebar_width(240.0)
            .max_sidebar_width(380.0)
            .sidebar_width_fraction(0.22)
            .vexpand(true)
            .build();
        let banner = adw::Banner::new("");
        let content = ui::vbox(0);
        content.append(&banner);
        content.append(&split);
        let toasts = adw::ToastOverlay::new();
        toasts.set_child(Some(&content));
        window.set_content(Some(&toasts));
        let narrow = adw::Breakpoint::new(adw::BreakpointCondition::new_length(
            adw::BreakpointConditionLengthType::MaxWidth,
            760.0,
            adw::LengthUnit::Sp,
        ));
        narrow.add_setter(&split, "collapsed", Some(&true.to_value()));
        window.add_breakpoint(narrow);

        let app = App::new(store, settings, window, toasts, split);
        let sidebar = Sidebar::new(&app);
        app.sidebar_split.set_sidebar(Some(&sidebar.widget));
        let win = Rc::new_cyclic(|me: &Weak<Window>| Window {
            app: app.clone(),
            me: me.clone(),
            sidebar,
            current: RefCell::new(Current { work: None, _page: Box::new(()) }),
            banner,
            watch: RefCell::new(None),
        });
        let weak = win.me.clone();
        app.set_opener(move |place| {
            if let Some(win) = weak.upgrade() {
                win.open(place);
            }
        });
        let weak = win.me.clone();
        *win.watch.borrow_mut() = Some(app.store.watch(move |change| {
            if change == kit::Change::Failure {
                if let Some(win) = weak.upgrade() {
                    let failure = win.app.store.failure();
                    win.banner.set_title(&ui::escaped(failure.as_deref().unwrap_or("")));
                    win.banner.set_revealed(failure.is_some());
                }
            }
        }));
        win.actions();
        let first = app.settings.last_work().map_or(Place::NewGoal, Place::Work);
        win.open(first);
        win
    }

    pub fn present(&self) {
        self.app.window.present();
    }

    /// Shows a place in the main pane.
    pub fn open(&self, place: Place) {
        if let (Place::Work(id), Some(page)) = (&place, &self.current.borrow().work) {
            if page.id() == id {
                return;
            }
        }
        let scope = self.sidebar.scope();
        let (widget, current): (gtk::Widget, Current) = match &place {
            Place::NewGoal => {
                let page = NewGoalPage::new(&self.app, scope);
                (page.widget.clone().upcast(), Current { work: None, _page: Box::new(page) })
            }
            Place::Work(id) => {
                let page = WorkPage::new(&self.app, id);
                (page.widget.clone().upcast(), Current { work: Some(Rc::clone(&page)), _page: Box::new(page) })
            }
            Place::Archived => {
                let shown = pages::archived(&self.app, scope);
                (shown.widget.clone(), Current { work: None, _page: Box::new(shown) })
            }
            Place::Taste => {
                let shown = pages::taste(&self.app, scope);
                (shown.widget.clone(), Current { work: None, _page: Box::new(shown) })
            }
            Place::Review(project) => {
                let shown = pages::review(&self.app, project);
                (shown.widget.clone(), Current { work: None, _page: Box::new(shown) })
            }
        };
        self.app.sidebar_split.set_content(Some(&widget));
        *self.current.borrow_mut() = current;
        self.sidebar.set_place(&place);
        let last = match &place {
            Place::Work(id) => Some(id.as_str()),
            _ => None,
        };
        if let Err(e) = self.app.settings.set_last_work(last) {
            self.app.complain(&format!("cannot remember the page open: {e}"));
        }
        if self.app.sidebar_split.is_collapsed() {
            self.app.sidebar_split.set_show_sidebar(false);
        }
    }

    fn rename(&self, id: &str) {
        self.open(Place::Work(id.to_string()));
        if let Some(page) = &self.current.borrow().work {
            page.start_rename();
        }
    }

    fn actions(&self) {
        let window = &self.app.window;
        let simple = |name: &str, f: Box<dyn Fn(&Window)>| {
            let action = gio::SimpleAction::new(name, None);
            let weak = self.me.clone();
            action.connect_activate(move |_, _| {
                if let Some(win) = weak.upgrade() {
                    f(&win);
                }
            });
            window.add_action(&action);
        };
        simple("new-goal", Box::new(|w| w.open(Place::NewGoal)));
        simple("command", Box::new(|w| w.app.ask_command()));
        simple("add-product", Box::new(|w| w.add_product()));

        let with_id = |name: &str, f: IdAction| {
            let action = gio::SimpleAction::new(name, Some(glib::VariantTy::STRING));
            let weak = self.me.clone();
            action.connect_activate(move |_, param| {
                let (Some(win), Some(id)) = (weak.upgrade(), param.and_then(|p| p.get::<String>())) else { return };
                f(&win, id);
            });
            window.add_action(&action);
        };
        with_id("open", Box::new(|w, id| w.open(Place::Work(id))));
        with_id("rename", Box::new(|w, id| w.rename(&id)));
        with_id("decide", Box::new(|w, id| w.ask_about(id, false)));
        with_id("reopen", Box::new(|w, id| w.ask_about(id, true)));

        let run = gio::SimpleAction::new("run", Some(glib::VariantTy::new("(sas)").expect("(sas) is a variant type")));
        let weak = self.me.clone();
        run.connect_activate(move |_, param| {
            let (Some(win), Some((title, argv))) =
                (weak.upgrade(), param.and_then(|p| p.get::<(String, Vec<String>)>()))
            else {
                return;
            };
            win.app.run(Plan { title, argv });
        });
        window.add_action(&run);

        let key = gio::SimpleAction::new_stateful(
            "send-key",
            Some(glib::VariantTy::STRING),
            &self.app.settings.send_key().name().to_variant(),
        );
        let weak = self.me.clone();
        key.connect_activate(move |action, param| {
            let (Some(win), Some(name)) = (weak.upgrade(), param.and_then(|p| p.get::<String>())) else { return };
            let Some(chosen) = SendKey::named(&name) else { return };
            action.set_state(&name.to_variant());
            if let Err(e) = win.app.settings.set_send_key(chosen) {
                win.app.complain(&format!("cannot keep the send key: {e}"));
            }
        });
        window.add_action(&key);

        let band = gio::SimpleAction::new_stateful("band", Some(glib::VariantTy::STRING), &"".to_variant());
        let weak = self.me.clone();
        band.connect_activate(move |action, param| {
            let (Some(win), Some(name)) = (weak.upgrade(), param.and_then(|p| p.get::<String>())) else { return };
            action.set_state(&name.to_variant());
            win.sidebar.set_band(band_named(&name));
        });
        window.add_action(&band);
    }

    /// Asks the words a decision or a reopen needs, for work read fresh.
    fn ask_about(&self, id: String, reopen: bool) {
        let app = self.app.clone();
        self.app.spawn(async move {
            match app.store.work(&id).await {
                Ok(work) if reopen => app.ask_reopen(&work),
                Ok(work) => app.ask_decision(&work),
                Err(e) => app.complain(&e.to_string()),
            }
        });
    }

    /// Adds a product from a folder: its name offered from the folder's own,
    /// the agents offered those `wd doctor` lists, those on PATH first.
    fn add_product(&self) {
        let dialog = gtk::FileDialog::builder().title("Choose the product's folder").modal(true).build();
        let weak = self.me.clone();
        dialog.select_folder(Some(&self.app.window), gio::Cancellable::NONE, move |chosen| {
            let Some(win) = weak.upgrade() else { return };
            let folder = match chosen {
                Ok(folder) => folder,
                Err(e) if e.matches(gtk::DialogError::Dismissed) => return,
                Err(e) => return win.app.complain(&format!("cannot choose a folder: {e}")),
            };
            let Some(path) = folder.path() else {
                return win.app.complain("the folder chosen is not on this computer");
            };
            let path = path.display().to_string();
            let app = win.app.clone();
            app.clone().spawn(async move {
                match app.store.doctor().await {
                    Ok(doctor) => {
                        let mut runners = doctor.runners;
                        runners.sort_by_key(|r| !r.detected);
                        let names: Vec<String> = runners.into_iter().map(|r| r.runner).collect();
                        if let Some(win) = weak.upgrade() {
                            win.confirm_product(&path, &names);
                        }
                    }
                    Err(e) => app.complain(&format!("cannot list the agents: {e}")),
                }
            });
        });
    }

    fn confirm_product(&self, path: &str, runners: &[String]) {
        let dialog = adw::AlertDialog::new(Some("Add a product"), Some(path));
        dialog.add_responses(&[("cancel", "Cancel"), ("add", "Add")]);
        dialog.set_response_appearance("add", adw::ResponseAppearance::Suggested);
        dialog.set_default_response(Some("add"));
        dialog.set_close_response("cancel");
        let form = ui::vbox(ui::SPACE_2);
        let name = gtk::Entry::builder().text(Plan::product_name(path)).activates_default(true).build();
        let strs: Vec<&str> = runners.iter().map(String::as_str).collect();
        let runner = gtk::DropDown::from_strings(&strs);
        let mode = gtk::DropDown::from_strings(&["ask", "auto"]);
        for (label, widget) in [
            ("Name", name.clone().upcast::<gtk::Widget>()),
            ("Agent", runner.clone().upcast()),
            ("Mode", mode.clone().upcast()),
        ] {
            form.append(&ui::line(label, &["meta", "text2"]));
            form.append(&widget);
        }
        dialog.set_extra_child(Some(&form));
        let (weak, path, runners) = (self.me.clone(), path.to_string(), runners.to_vec());
        dialog.connect_response(None, move |_, response| {
            let Some(win) = weak.upgrade() else { return };
            if response != "add" {
                return;
            }
            let chosen =
                usize::try_from(runner.selected()).ok().and_then(|i| runners.get(i)).cloned().unwrap_or_default();
            let mode = if mode.selected() == 1 { "auto" } else { "ask" };
            let plan = match Plan::add_product(&name.text(), &path, &chosen, mode) {
                Ok(plan) => plan,
                Err(e) => return win.app.complain(&e.to_string()),
            };
            let product = name.text().trim().to_string();
            let (app, weak) = (win.app.clone(), win.me.clone());
            app.clone().spawn(async move {
                match app.store.run(&plan).await {
                    Ok(r) if r.code == 0 => {
                        if let Err(e) = app.store.reload_projects().await {
                            return app.complain(&e.to_string());
                        }
                        app.toast(&plan.command_line());
                        if let Some(win) = weak.upgrade() {
                            win.sidebar.set_scope(&product);
                        }
                    }
                    Ok(r) => app.complain(&format!("{} failed: {}", plan.command_line(), r.complaint())),
                    Err(e) => app.complain(&e.to_string()),
                }
            });
        });
        dialog.present(Some(&self.app.window));
    }
}
