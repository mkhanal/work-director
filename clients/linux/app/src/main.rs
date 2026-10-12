//! Work Director for Linux: goals as threads on the left, the one chosen as a
//! conversation in the middle, its agents beside it, over `wd serve --stdio`.

use std::rc::Rc;

use adw::prelude::*;
use gtk::{gdk, gio, glib};

use kit::{locate_wd, Channel, Spawner, Store};

const APP_ID: &str = "dev.mkhanal.WorkDirector";

fn main() -> glib::ExitCode {
    let app = adw::Application::builder().application_id(APP_ID).build();
    app.connect_startup(|_| load_css());
    app.connect_activate(activate);
    app.run()
}

fn load_css() {
    let provider = gtk::CssProvider::new();
    provider.load_from_string(include_str!("style.css"));
    let display = gdk::Display::default().expect("a display to show the app on");
    gtk::style_context_add_provider_for_display(&display, &provider, gtk::STYLE_PROVIDER_PRIORITY_APPLICATION);
}

fn spawner() -> Spawner {
    Rc::new(|f| {
        glib::MainContext::default().spawn_local(f);
    })
}

fn activate(app: &adw::Application) {
    if let Some(window) = app.active_window() {
        window.present();
        return;
    }
    let exe = std::env::current_exe().unwrap_or_default();
    let started = locate_wd(&exe, std::env::var_os("WD_BIN").as_deref())
        .map_err(|e| e.to_string())
        .and_then(|wd| Channel::spawn(&wd, None).map_err(|e| e.to_string()));
    let content: gtk::Widget = match started {
        Ok(channel) => {
            let store = Store::new(channel, spawner());
            let s = Rc::clone(&store);
            glib::MainContext::default().spawn_local(async move { s.start().await });
            let page = adw::StatusPage::builder().title("Work Director").icon_name("emblem-ok-symbolic").build();
            page.upcast()
        }
        Err(why) => adw::StatusPage::builder()
            .title("Cannot start wd")
            .description(glib::markup_escape_text(&why).as_str())
            .icon_name("dialog-error-symbolic")
            .build()
            .upcast(),
    };
    let toasts = adw::ToastOverlay::new();
    let split = adw::OverlaySplitView::new();
    split.set_content(Some(&content));
    let view = adw::ToolbarView::new();
    view.add_top_bar(&adw::HeaderBar::new());
    view.set_content(Some(&split));
    toasts.set_child(Some(&view));
    let toast = adw::Toast::builder().title("Ready").button_label("Undo").build();
    toast.connect_button_clicked(|_| {});
    toasts.add_toast(toast);
    let window = adw::ApplicationWindow::builder()
        .application(app)
        .title("Work Director")
        .default_width(1280)
        .default_height(820)
        .content(&toasts)
        .build();
    let dialog = adw::AlertDialog::new(Some("Probe"), None);
    dialog.add_response("ok", "OK");
    let _ = gio::Cancellable::NONE;
    window.present();
    dialog.present(Some(&window));
}
