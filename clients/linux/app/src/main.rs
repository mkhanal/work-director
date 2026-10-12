//! Work Director for Linux: goals as threads on the left, the one chosen as a
//! conversation in the middle, its agents beside it, over `wd serve --stdio`.

mod agents;
mod app;
mod composer;
mod new_goal_page;
mod pages;
mod question;
mod settings;
mod sidebar;
mod turns;
mod ui;
mod window;
mod work_page;

use std::rc::Rc;

use adw::prelude::*;
use gtk::{gdk, gio, glib};

use kit::{locate_wd, Band, Channel, Spawner, Store};

use crate::app::Place;
use crate::settings::Settings;
use crate::window::Window;

const APP_ID: &str = "dev.mkhanal.WorkDirector";

fn main() -> glib::ExitCode {
    let application = adw::Application::builder().application_id(APP_ID).build();
    application.connect_startup(|application| {
        load_css();
        application.set_accels_for_action("win.new-goal", &["<Control>n"]);
        application.set_accels_for_action("win.command", &["<Control>k"]);
    });
    application.connect_activate(activate);
    application.run()
}

fn load_css() {
    let provider = gtk::CssProvider::new();
    provider.load_from_string(include_str!("style.css"));
    if let Some(display) = gdk::Display::default() {
        gtk::style_context_add_provider_for_display(&display, &provider, gtk::STYLE_PROVIDER_PRIORITY_APPLICATION);
    }
}

/// Runs the store's futures on GTK's main loop.
fn spawner() -> Spawner {
    Rc::new(|f: std::pin::Pin<Box<dyn std::future::Future<Output = ()>>>| {
        glib::spawn_future_local(f);
    })
}

fn activate(application: &adw::Application) {
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
        .and_then(|wd| Channel::spawn(&wd, None).map_err(|e| e.to_string()));
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
