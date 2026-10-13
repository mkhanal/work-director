//! Work Director for Linux: goals as threads on the left, the one chosen as a
//! conversation in the middle, its agents beside it, over `wd serve --stdio`.

mod agents;
mod app;
mod composer;
mod new_goal_page;
mod pages;
mod question;
mod settings;
mod side_panel;
mod sidebar;
mod turns;
mod ui;
mod window;
mod work_page;
mod working;

use std::cell::{OnceCell, RefCell};
use std::collections::HashMap;
use std::rc::Rc;
use std::time::Duration;

use adw::prelude::*;
use gtk::{gdk, gio, glib};

use kit::{locate_wd, login_path, Band, Channel, Spawner, Store};

use crate::app::Place;
use crate::settings::Settings;
use crate::window::Window;

const APP_ID: &str = "dev.mkhanal.WorkDirector";

/// How long the login shell has to say its PATH before the usual directories stand in.
const LOGIN_SHELL_WAIT: Duration = Duration::from_secs(5);

fn main() -> glib::ExitCode {
    // Asked while GTK starts; the first activation waits for the answer.
    let login = Login {
        asking: RefCell::new(Some(std::thread::spawn(|| {
            login_path::resolve(&inherited_environment(), LOGIN_SHELL_WAIT)
        }))),
        environment: OnceCell::new(),
    };
    let application = adw::Application::builder().application_id(APP_ID).build();
    application.connect_startup(|application| {
        load_css();
        application.set_accels_for_action("win.new-goal", &["<Control>n"]);
        application.set_accels_for_action("win.command", &["<Control>k"]);
    });
    application.connect_activate(move |application| activate(application, &login));
    application.run()
}

/// The tokens, and over them the dark ones while the system is dark.
fn load_css() {
    let Some(display) = gdk::Display::default() else { return };
    let light = gtk::CssProvider::new();
    light.load_from_string(include_str!("style.css"));
    gtk::style_context_add_provider_for_display(&display, &light, gtk::STYLE_PROVIDER_PRIORITY_APPLICATION);
    let dark = gtk::CssProvider::new();
    dark.load_from_string(include_str!("dark.css"));
    let follow = move |manager: &adw::StyleManager| {
        if manager.is_dark() {
            gtk::style_context_add_provider_for_display(&display, &dark, gtk::STYLE_PROVIDER_PRIORITY_APPLICATION + 1);
        } else {
            gtk::style_context_remove_provider_for_display(&display, &dark);
        }
    };
    let manager = adw::StyleManager::default();
    follow(&manager);
    manager.connect_dark_notify(follow);
}

/// Runs the store's futures on GTK's main loop.
fn spawner() -> Spawner {
    Rc::new(|f: std::pin::Pin<Box<dyn std::future::Future<Output = ()>>>| {
        glib::spawn_future_local(f);
    })
}

/// The app's environment, minus any variable that is not text.
fn inherited_environment() -> HashMap<String, String> {
    std::env::vars_os().filter_map(|(k, v)| Some((k.into_string().ok()?, v.into_string().ok()?))).collect()
}

/// The environment every wd the app starts gets, once the login shell has answered.
struct Login {
    asking: RefCell<Option<std::thread::JoinHandle<HashMap<String, String>>>>,
    environment: OnceCell<HashMap<String, String>>,
}

impl Login {
    fn environment(&self) -> &HashMap<String, String> {
        self.environment.get_or_init(|| {
            let asked = self.asking.borrow_mut().take().map(|asking| asking.join());
            asked.and_then(Result::ok).unwrap_or_else(inherited_environment)
        })
    }
}

fn activate(application: &adw::Application, login: &Login) {
    if let Some(window) = application.active_window() {
        window.present();
        return;
    }
    let exe = match std::env::current_exe() {
        Ok(exe) => exe,
        Err(e) => return refuse(application, &format!("cannot find the app's own executable: {e}")),
    };
    let channel = locate_wd(&exe, std::env::var_os("WD_BIN").as_deref())
        .map_err(|e| e.to_string())
        .and_then(|wd| Channel::spawn(&wd, Some(login.environment())).map_err(|e| e.to_string()));
    let channel = match channel {
        Ok(channel) => channel,
        Err(why) => return refuse(application, &why),
    };
    let (settings, problem) = Settings::load();
    let store = Store::new(channel, spawner());
    let window = Window::new(application, Rc::clone(&store), Rc::new(settings));
    if let Some(problem) = problem {
        window.app.complain(&problem);
    }

    let told = application.clone();
    store.on_arrival(move |arrival| {
        let note = gio::Notification::new(&arrival.work.title);
        let why = match arrival.band {
            Band::NeedsYou => "needs you",
            Band::ReadyToPush => "is ready to push",
            _ => "is ready to close",
        };
        note.set_body(Some(&format!("{} {why}", arrival.work.project)));
        note.set_default_action_and_target_value("app.open", Some(&arrival.work.id.to_variant()));
        told.send_notification(Some(&arrival.work.id), &note);
    });

    let open = gio::SimpleAction::new("open", Some(glib::VariantTy::STRING));
    let shown = Rc::clone(&window);
    open.connect_activate(move |_, param| {
        if let Some(id) = param.and_then(|p| p.get::<String>()) {
            shown.present();
            shown.app.open(Place::Work(id));
        }
    });
    application.add_action(&open);
    let ending = Rc::clone(&store);
    application.connect_shutdown(move |_| ending.stop());

    let starting = Rc::clone(&store);
    glib::spawn_future_local(async move { starting.start().await });
    window.present();
}

/// A window that says why the app cannot start, and nothing else.
fn refuse(application: &adw::Application, why: &str) {
    let page = ui::empty_state("dialog-error-symbolic", "Cannot start", why);
    let view = adw::ToolbarView::new();
    view.add_top_bar(&adw::HeaderBar::new());
    view.set_content(Some(&page));
    let window = adw::ApplicationWindow::builder()
        .application(application)
        .title("Work Director")
        .default_width(640)
        .default_height(420)
        .content(&view)
        .build();
    window.present();
}
