//! Every goal as a thread: what needs you, what is working, what is ready,
//! then queued and settled folded away, filtered by product, by what they
//! wait on and by search.

use std::cell::{Cell, RefCell};
use std::rc::{Rc, Weak};

use adw::prelude::*;
use gtk::{gdk, gio, glib};

use kit::{Band, BoardThread, Watch, WorkState};

use crate::app::{work_menu, App, Place};
use crate::ui::{self, SPACE_1, SPACE_2, SPACE_3, SPACE_4};

/// What a row of the list does when chosen.
#[derive(Debug, Clone, PartialEq, Eq)]
enum Row {
    Header,
    Open(Place),
    FoldQueued,
    FoldSettled,
}

pub struct Sidebar {
    app: App,
    me: Weak<Sidebar>,
    pub widget: adw::ToolbarView,
    products: gtk::DropDown,
    /// The product names the drop-down offers after "All products".
    offered: RefCell<Vec<String>>,
    search: gtk::SearchEntry,
    list: gtk::ListBox,
    rows: RefCell<Vec<Row>>,
    band: Cell<Option<Band>>,
    show_queued: Cell<bool>,
    show_settled: Cell<bool>,
    place: RefCell<Place>,
    /// Set while the list is rebuilt, so selecting the current row opens nothing.
    quiet: Cell<bool>,
    watch: RefCell<Option<Watch>>,
}

impl Sidebar {
    pub fn new(app: &App) -> Rc<Sidebar> {
        let products = gtk::DropDown::from_strings(&["All products"]);
        products.add_css_class("flat");
        products.set_tooltip_text(Some("The product everything is scoped to"));
        let new_goal = gtk::Button::builder()
            .icon_name("document-edit-symbolic")
            .action_name("win.new-goal")
            .tooltip_text("New goal (Ctrl+N)")
            .build();
        let menu = gio::Menu::new();
        menu.append(Some("Add Product…"), Some("win.add-product"));
        menu.append(Some("Run a wd Command…"), Some("win.command"));
        let keys = gio::Menu::new();
        for key in kit::SendKey::ALL {
            let item = gio::MenuItem::new(Some(key.title()), None);
            item.set_action_and_target_value(Some("win.send-key"), Some(&key.name().to_variant()));
            keys.append_item(&item);
        }
        menu.append_section(Some("Sending"), &keys);
        let main = gtk::MenuButton::builder().icon_name("open-menu-symbolic").menu_model(&menu).tooltip_text("Menu").build();
        main.set_primary(true);
        let header = adw::HeaderBar::new();
        header.set_show_title(false);
        header.pack_start(&products);
        header.pack_end(&main);
        header.pack_end(&new_goal);

        let search = gtk::SearchEntry::builder().placeholder_text("Search goals").hexpand(true).build();
        let bands = gio::Menu::new();
        let everything = gio::MenuItem::new(Some("Everything open"), None);
        everything.set_action_and_target_value(Some("win.band"), Some(&"".to_variant()));
        bands.append_item(&everything);
        for band in Band::ALL.into_iter().filter(|b| *b != Band::AtRest) {
            let item = gio::MenuItem::new(Some(band.label()), None);
            item.set_action_and_target_value(Some("win.band"), Some(&band_name(band).to_variant()));
            bands.append_item(&item);
        }
        let filter = gtk::MenuButton::builder()
            .icon_name("funnel-symbolic")
            .menu_model(&bands)
            .tooltip_text("Show only what waits on one thing")
            .build();
        filter.add_css_class("flat");
        let tools = ui::hbox(SPACE_1);
        tools.set_margin_start(SPACE_2);
        tools.set_margin_end(SPACE_2);
        tools.set_margin_bottom(SPACE_2);
        tools.append(&search);
        tools.append(&filter);

        let list = gtk::ListBox::new();
        list.add_css_class("navigation-sidebar");
        list.set_selection_mode(gtk::SelectionMode::Single);
        let scroller = gtk::ScrolledWindow::builder()
            .hscrollbar_policy(gtk::PolicyType::Never)
            .vexpand(true)
            .child(&list)
            .build();
        let body = ui::vbox(0);
        body.append(&tools);
        body.append(&scroller);
        let widget = adw::ToolbarView::new();
        widget.add_top_bar(&header);
        widget.set_content(Some(&body));

        let sidebar = Rc::new_cyclic(|me: &Weak<Sidebar>| Sidebar {
            app: app.clone(),
            me: me.clone(),
            widget,
            products,
            offered: RefCell::new(Vec::new()),
            search,
            list,
            rows: RefCell::new(Vec::new()),
            band: Cell::new(None),
            show_queued: Cell::new(false),
            show_settled: Cell::new(false),
            place: RefCell::new(Place::NewGoal),
            quiet: Cell::new(false),
            watch: RefCell::new(None),
        });
        sidebar.wire();
        sidebar.render_products();
        sidebar.render();
        sidebar
    }

    fn wire(&self) {
        let weak = self.me.clone();
        *self.watch.borrow_mut() = Some(self.app.store.watch(move |change| {
            let Some(sidebar) = weak.upgrade() else { return };
            match change {
                kit::Change::Board => sidebar.render(),
                kit::Change::Projects => {
                    sidebar.render_products();
                    sidebar.render();
                }
                kit::Change::Outbox | kit::Change::Failure => {}
            }
        }));
        let weak = self.me.clone();
        self.search.connect_search_changed(move |_| {
            if let Some(sidebar) = weak.upgrade() {
                sidebar.render();
            }
        });
        let weak = self.me.clone();
        self.products.connect_selected_notify(move |d| {
            let Some(sidebar) = weak.upgrade() else { return };
            if sidebar.quiet.get() {
                return;
            }
            let scope = usize::try_from(d.selected())
                .ok()
                .and_then(|i| i.checked_sub(1))
                .and_then(|i| sidebar.offered.borrow().get(i).cloned());
            if let Err(e) = sidebar.app.settings.set_scope(scope.as_deref()) {
                sidebar.app.complain(&format!("cannot keep the product in scope: {e}"));
            }
            sidebar.render();
            if matches!(*sidebar.place.borrow(), Place::NewGoal | Place::Review(_)) {
                sidebar.app.open(Place::NewGoal);
            }
        });
        let weak = self.me.clone();
        self.list.connect_row_activated(move |_, row| {
            let Some(sidebar) = weak.upgrade() else { return };
            let chosen = usize::try_from(row.index()).ok().and_then(|i| sidebar.rows.borrow().get(i).cloned());
            match chosen {
                Some(Row::Open(place)) => sidebar.app.open(place),
                Some(Row::FoldQueued) => {
                    sidebar.show_queued.set(!sidebar.show_queued.get());
                    sidebar.render();
                }
                Some(Row::FoldSettled) => {
                    sidebar.show_settled.set(!sidebar.show_settled.get());
                    sidebar.render();
                }
                Some(Row::Header) | None => {}
            }
        });
    }

    /// The product in scope; none is all products.
    pub fn scope(&self) -> Option<String> {
        self.app.settings.scope().filter(|s| self.app.store.projects().iter().any(|p| &p.name == s))
    }

    pub fn set_scope(&self, scope: &str) {
        if let Some(i) = self.offered.borrow().iter().position(|p| p == scope) {
            self.products.set_selected(i as u32 + 1);
        }
    }

    pub fn set_band(&self, band: Option<Band>) {
        self.band.set(band);
        self.render();
    }

    /// Marks the place shown in the main pane.
    pub fn set_place(&self, place: &Place) {
        *self.place.borrow_mut() = place.clone();
        self.select();
    }

    fn render_products(&self) {
        let names: Vec<String> = self.app.store.projects().iter().map(|p| p.name.clone()).collect();
        let mut shown = vec!["All products".to_string()];
        shown.extend(names.iter().cloned());
        let strs: Vec<&str> = shown.iter().map(String::as_str).collect();
        self.quiet.set(true);
        self.products.set_model(Some(&gtk::StringList::new(&strs)));
        let selected = self.app.settings.scope().and_then(|s| names.iter().position(|n| *n == s)).map_or(0, |i| i + 1);
        self.products.set_selected(selected as u32);
        self.quiet.set(false);
        *self.offered.borrow_mut() = names;
    }

    fn render(&self) {
        while let Some(row) = self.list.row_at_index(0) {
            self.list.remove(&row);
        }
        let mut rows = Vec::new();
        let scope = self.scope();
        let query = self.search.text().to_lowercase();
        let matches = |t: &&BoardThread| {
            query.is_empty() || t.work.title.to_lowercase().contains(&query) || t.work.id.starts_with(&query)
        };
        let threads = self.app.store.board().as_ref().map(|b| b.threads(scope.as_deref(), self.band.get())).unwrap_or_default();

        let new_goal = nav_row("document-edit-symbolic", "New Goal");
        self.list.append(&new_goal);
        rows.push(Row::Open(Place::NewGoal));

        let open: Vec<&BoardThread> = threads.open.iter().filter(matches).collect();
        let sections: [(&str, &[Band]); 3] = [
            ("Needs you", &[Band::NeedsYou]),
            ("Working", &[Band::InFlight, Band::InReview, Band::Paused]),
            ("Ready", &[Band::ReadyToPush, Band::ReadyToClose]),
        ];
        for (title, bands) in sections {
            let shown: Vec<&&BoardThread> = open.iter().filter(|t| bands.contains(&t.band)).collect();
            if shown.is_empty() {
                continue;
            }
            self.list.append(&header_row(title, None));
            rows.push(Row::Header);
            for t in shown {
                self.list.append(&self.thread_row(t));
                rows.push(Row::Open(Place::Work(t.work.id.clone())));
            }
        }
        if open.is_empty() {
            let none = match self.band.get() {
                Some(b) => format!("Nothing {}.", b.label().to_lowercase()),
                None => "Nothing open.".to_string(),
            };
            let row = gtk::ListBoxRow::builder().selectable(false).activatable(false).build();
            let label = ui::text(&none, &["callout", "text2"]);
            label.set_margin_start(SPACE_2);
            row.set_child(Some(&label));
            self.list.append(&row);
            rows.push(Row::Header);
        }
        for (title, list, shown, fold) in [
            ("Queued", &threads.queued, self.show_queued.get(), Row::FoldQueued),
            ("Settled", &threads.settled, self.show_settled.get(), Row::FoldSettled),
        ] {
            let matching: Vec<&BoardThread> = list.iter().filter(matches).collect();
            let header = header_row(&format!("{title} {}", matching.len()), Some(shown));
            header.set_activatable(true);
            self.list.append(&header);
            rows.push(fold);
            if shown {
                for t in matching {
                    self.list.append(&self.thread_row(t));
                    rows.push(Row::Open(Place::Work(t.work.id.clone())));
                }
            }
        }
        self.list.append(&header_row("More", None));
        rows.push(Row::Header);
        self.list.append(&nav_row("starred-symbolic", "Taste"));
        rows.push(Row::Open(Place::Taste));
        self.list.append(&nav_row("folder-symbolic", "Archived"));
        rows.push(Row::Open(Place::Archived));
        let reviews: Vec<String> = match &scope {
            Some(p) => vec![p.clone()],
            None => self.app.store.projects().iter().map(|p| p.name.clone()).collect(),
        };
        for project in reviews {
            let label = if scope.is_some() { "What the loop decided".to_string() } else { format!("Review {project}") };
            self.list.append(&nav_row("view-list-symbolic", &label));
            rows.push(Row::Open(Place::Review(project)));
        }
        *self.rows.borrow_mut() = rows;
        self.select();
    }

    fn select(&self) {
        let place = self.place.borrow().clone();
        let at = self.rows.borrow().iter().position(|r| *r == Row::Open(place.clone()));
        match at.and_then(|i| self.list.row_at_index(i as i32)) {
            Some(row) => self.list.select_row(Some(&row)),
            None => self.list.unselect_all(),
        }
    }

    /// A goal as two lines: where it is and its title; then product, how far, how long ago.
    fn thread_row(&self, t: &BoardThread) -> gtk::ListBoxRow {
        let column = ui::vbox(2);
        column.set_margin_top(SPACE_1);
        column.set_margin_bottom(SPACE_1);
        let top = ui::hbox(SPACE_2);
        top.append(&state_dot(t.work.state, t.band));
        let title = ui::line(&t.work.title, &["callout", if t.work.kind.is_goal() { "strong" } else { "plain" }]);
        top.append(&title);
        column.append(&top);
        let mut parts = vec![t.work.project.clone()];
        if let Some(r) = t.rollup.as_ref().filter(|r| r.total > 0) {
            parts.push(format!("{}/{}", r.done, r.total));
        }
        parts.push(kit::time::ago(&t.work.updated, chrono::Utc::now()));
        let meta = ui::line(&parts.join(" · "), &["meta", "text3"]);
        meta.set_margin_start(8 + SPACE_2);
        column.append(&meta);
        let row = gtk::ListBoxRow::builder().child(&column).tooltip_text(t.work.state.look().label).build();

        let click = gtk::GestureClick::new();
        click.set_button(gdk::BUTTON_SECONDARY);
        let (work, r) = (t.work.clone(), row.downgrade());
        click.connect_pressed(move |_, _, x, y| {
            let Some(row) = r.upgrade() else { return };
            let popover = gtk::PopoverMenu::from_model(Some(&work_menu(&work, false)));
            popover.set_parent(&row);
            popover.set_has_arrow(false);
            popover.set_pointing_to(Some(&gdk::Rectangle::new(x as i32, y as i32, 1, 1)));
            popover.connect_closed(|p| {
                let p = p.clone();
                glib::idle_add_local_once(move || p.unparent());
            });
            popover.popup();
        });
        row.add_controller(click);
        row
    }
}

pub fn band_name(band: Band) -> &'static str {
    match band {
        Band::NeedsYou => "needs-you",
        Band::InFlight => "in-flight",
        Band::Paused => "paused",
        Band::ReadyToPush => "ready-to-push",
        Band::InReview => "in-review",
        Band::ReadyToClose => "ready-to-close",
        Band::AtRest => "at-rest",
    }
}

pub fn band_named(name: &str) -> Option<Band> {
    Band::ALL.into_iter().find(|b| band_name(*b) == name)
}

fn nav_row(icon: &str, label: &str) -> gtk::ListBoxRow {
    let row = ui::hbox(SPACE_3);
    row.append(&ui::icon(icon, &[]));
    row.append(&ui::line(label, &["callout"]));
    gtk::ListBoxRow::builder().child(&row).build()
}

/// A section header; a folded one says whether it is open.
fn header_row(title: &str, open: Option<bool>) -> gtk::ListBoxRow {
    let row = ui::hbox(SPACE_1);
    row.set_margin_top(SPACE_4);
    if let Some(open) = open {
        row.append(&ui::icon(if open { "pan-down-symbolic" } else { "pan-end-symbolic" }, &["text3"]));
    }
    row.append(&ui::line(&title.to_uppercase(), &["micro", "text3"]));
    gtk::ListBoxRow::builder().child(&row).selectable(false).activatable(false).build()
}

/// Where a thread is, as a dot coloured by its band; waiting on you, an orange ring.
fn state_dot(state: WorkState, band: Band) -> gtk::Widget {
    let dot = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    dot.add_css_class("dot");
    dot.set_valign(gtk::Align::Center);
    if band == Band::NeedsYou {
        dot.add_css_class("asking");
    } else {
        dot.add_css_class(band.css_class());
        dot.add_css_class(state.look().tint.css_class());
    }
    dot.upcast()
}
