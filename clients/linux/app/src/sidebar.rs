//! Top goals banded as the canvas has them: what needs you, what is working,
//! what is completed, then what was let go folded away; filtered by product
//! and by search. The open goal's breadcrumb shows under its row.

use std::cell::{Cell, RefCell};
use std::rc::{Rc, Weak};

use adw::prelude::*;
use gtk::{gdk, gio, glib};

use kit::{Link, SidebarBand, SidebarRow, Tone, Watch};

use crate::app::{work_menu, App, Place};
use crate::ui::{self, SPACE_1, SPACE_2, SPACE_3};
use crate::working;

/// What a row of the list does when chosen.
#[derive(Debug, Clone, PartialEq, Eq)]
enum Row {
    Header,
    Open(Place),
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
    show_settled: Cell<bool>,
    place: RefCell<Place>,
    /// The open goal's breadcrumb, top goal first.
    path: RefCell<Vec<Link>>,
    /// Set while the list is rebuilt, so selecting the current row opens nothing.
    quiet: Cell<bool>,
    watch: RefCell<Option<Watch>>,
}

impl Sidebar {
    pub fn new(app: &App) -> Rc<Sidebar> {
        let products = gtk::DropDown::from_strings(&["All products"]);
        products.add_css_class("flat");
        products.add_css_class("sidebar-title");
        products.set_tooltip_text(Some("The product everything is scoped to"));
        let new_goal = ui::button("+ New Goal", false);
        new_goal.add_css_class("small");
        new_goal.set_action_name(Some("win.new-goal"));
        new_goal.set_tooltip_text(Some("New goal (Ctrl+N)"));
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
        let main =
            gtk::MenuButton::builder().icon_name("open-menu-symbolic").menu_model(&menu).tooltip_text("Menu").build();
        main.add_css_class("flat");
        ui::announce(&main, "Menu");
        main.set_primary(true);
        let header = adw::HeaderBar::new();
        header.set_show_title(false);
        header.pack_start(&products);
        header.pack_end(&main);
        header.pack_end(&new_goal);

        let search = gtk::SearchEntry::builder().placeholder_text("Search goals").hexpand(true).build();
        search.add_css_class("search");
        search.set_margin_start(SPACE_1);
        search.set_margin_end(SPACE_1);
        search.set_margin_bottom(10);

        let list = gtk::ListBox::new();
        list.set_selection_mode(gtk::SelectionMode::Single);
        let scroller =
            gtk::ScrolledWindow::builder().hscrollbar_policy(gtk::PolicyType::Never).vexpand(true).child(&list).build();
        let body = ui::vbox(0);
        body.set_margin_start(10);
        body.set_margin_end(10);
        body.set_margin_top(SPACE_1);
        body.append(&search);
        body.append(&scroller);
        let widget = adw::ToolbarView::new();
        widget.add_css_class("sidebar-pane");
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
            show_settled: Cell::new(false),
            place: RefCell::new(Place::NewGoal),
            path: RefCell::new(Vec::new()),
            quiet: Cell::new(false),
            watch: RefCell::new(None),
        });
        sidebar.wire();
        sidebar.copy_keys();
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
                kit::Change::Outbox | kit::Change::Writes | kit::Change::Failure => {}
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
                Some(Row::FoldSettled) => {
                    sidebar.show_settled.set(!sidebar.show_settled.get());
                    sidebar.render();
                }
                Some(Row::Header) | None => {}
            }
        });
    }

    /// Ctrl+C on a goal in the list copies its title and id: the row with
    /// focus, else the one selected.
    fn copy_keys(&self) {
        let keys = gtk::EventControllerKey::new();
        let weak = self.me.clone();
        keys.connect_key_pressed(move |_, key, _, state| {
            let copy = state.contains(gdk::ModifierType::CONTROL_MASK) && matches!(key, gdk::Key::c | gdk::Key::C);
            let Some(sidebar) = weak.upgrade().filter(|_| copy) else { return glib::Propagation::Proceed };
            let row =
                sidebar.list.focus_child().and_downcast::<gtk::ListBoxRow>().or_else(|| sidebar.list.selected_row());
            let chosen =
                row.and_then(|r| usize::try_from(r.index()).ok()).and_then(|i| sidebar.rows.borrow().get(i).cloned());
            let Some(Row::Open(Place::Work(id))) = chosen else { return glib::Propagation::Proceed };
            let work = sidebar.app.store.board().as_ref().and_then(|b| {
                b.goals
                    .iter()
                    .map(|g| &g.work)
                    .chain(b.standalone.iter().map(|s| &s.work))
                    .find(|w| w.id == id)
                    .cloned()
            });
            match work {
                Some(work) => {
                    ui::copy_text(&sidebar.list, &kit::copy::work(&work));
                    glib::Propagation::Stop
                }
                None => glib::Propagation::Proceed,
            }
        });
        self.list.add_controller(keys);
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

    /// Marks the place shown in the main pane.
    pub fn set_place(&self, place: &Place) {
        *self.place.borrow_mut() = place.clone();
        let keeps = matches!(place, Place::Work(id) if self.path.borrow().iter().any(|l| &l.id == id));
        if !keeps {
            self.path.borrow_mut().clear();
        }
        self.render();
    }

    /// Shows the open goal's breadcrumb under its top goal's row.
    pub fn set_path(&self, chain: &[Link]) {
        if *self.path.borrow() == chain {
            return;
        }
        *self.path.borrow_mut() = chain.to_vec();
        self.render();
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
        self.quiet.set(true);
        while let Some(row) = self.list.row_at_index(0) {
            self.list.remove(&row);
        }
        let mut rows = Vec::new();
        let scope = self.scope();
        let query = self.search.text().to_lowercase();
        let matches = |r: &&SidebarRow| {
            query.is_empty() || r.work.title.to_lowercase().contains(&query) || r.work.id.starts_with(&query)
        };
        let bands =
            self.app.store.board().as_ref().map(|b| b.bands(scope.as_deref(), chrono::Utc::now())).unwrap_or_default();

        let mut first = true;
        for (title, list) in
            [("Needs you", &bands.needs_you), ("Working", &bands.working), ("Completed", &bands.completed)]
        {
            let shown: Vec<&SidebarRow> = list.iter().filter(matches).collect();
            if shown.is_empty() {
                continue;
            }
            self.list.append(&header_row(title, None, first));
            first = false;
            rows.push(Row::Header);
            for r in shown {
                self.list.append(&self.goal_row(r));
                rows.push(Row::Open(Place::Work(r.work.id.clone())));
            }
        }
        if first {
            let row = gtk::ListBoxRow::builder().selectable(false).activatable(false).build();
            let label = ui::text("Nothing here yet.", &["callout", "text3"]);
            label.set_margin_start(10);
            label.set_margin_top(SPACE_2);
            row.set_child(Some(&label));
            self.list.append(&row);
            rows.push(Row::Header);
        }
        let settled: Vec<&SidebarRow> = bands.settled.iter().filter(matches).collect();
        if !settled.is_empty() {
            let open = self.show_settled.get();
            let header = header_row(&format!("Settled {}", settled.len()), Some(open), false);
            header.set_activatable(true);
            self.list.append(&header);
            rows.push(Row::FoldSettled);
            if open {
                for r in settled {
                    self.list.append(&self.goal_row(r));
                    rows.push(Row::Open(Place::Work(r.work.id.clone())));
                }
            }
        }
        self.list.append(&header_row("More", None, false));
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
        self.quiet.set(false);
    }

    /// The row to mark: the place's own, or for a nested goal its top goal's.
    fn select(&self) {
        let place = match &*self.place.borrow() {
            Place::Work(id) => match self.path.borrow().first() {
                Some(top) if self.path.borrow().iter().any(|l| &l.id == id) => Place::Work(top.id.clone()),
                _ => Place::Work(id.clone()),
            },
            other => other.clone(),
        };
        let at = self.rows.borrow().iter().position(|r| *r == Row::Open(place.clone()));
        match at.and_then(|i| self.list.row_at_index(i as i32)) {
            Some(row) => self.list.select_row(Some(&row)),
            None => self.list.unselect_all(),
        }
    }

    /// A goal: its dot, its title, and its meta with what needs you; the open
    /// nested goal's breadcrumb under it.
    fn goal_row(&self, r: &SidebarRow) -> gtk::ListBoxRow {
        let line = ui::hbox(SPACE_2);
        let dot = gtk::Box::new(gtk::Orientation::Horizontal, 0);
        dot.add_css_class("sidebar-dot");
        dot.add_css_class(match r.band {
            SidebarBand::NeedsYou => "needs-you",
            SidebarBand::Working => "working",
            SidebarBand::Completed => "completed",
            SidebarBand::Settled => "settled",
        });
        dot.set_valign(gtk::Align::Start);
        dot.set_halign(gtk::Align::Center);
        dot.set_size_request(8, 8);
        line.append(&dot);
        let column = ui::vbox(2);
        column.set_hexpand(true);
        let title = ui::text(&r.work.title, &["goal-title"]);
        column.append(&title);
        let meta = ui::hbox(6);
        meta.append(&ui::line(&r.meta, &["meta", "text3"]));
        if r.proposed {
            meta.append(&ui::line("·", &["meta", "text3"]));
            meta.append(&ui::badge("Proposed", Tone::Attention));
        } else if r.needs_you > 0 {
            meta.append(&ui::line("·", &["meta", "text3"]));
            meta.append(&ui::badge(&format!("{} needs you", r.needs_you), Tone::Attention));
        }
        if r.working {
            let mark = working::mark();
            mark.add_css_class("sidebar-working");
            meta.append(&mark);
        }
        column.append(&meta);
        let path = self.path.borrow();
        if path.len() > 1 && path[0].id == r.work.id {
            let nested = ui::vbox(SPACE_1);
            nested.add_css_class("goal-path");
            for link in &path[1..] {
                let button = gtk::Button::builder().child(&ui::line(&link.title, &["meta", "medium"])).build();
                button.add_css_class("row-button");
                let (app, id) = (self.app.clone(), link.id.clone());
                button.connect_clicked(move |_| app.open(Place::Work(id.clone())));
                nested.append(&button);
            }
            column.append(&nested);
        }
        line.append(&column);
        let row = gtk::ListBoxRow::builder().child(&line).tooltip_text(r.work.state.look().label).build();
        row.add_css_class("goal-row");

        let click = gtk::GestureClick::new();
        click.set_button(gdk::BUTTON_SECONDARY);
        let (work, weak) = (r.work.clone(), row.downgrade());
        click.connect_pressed(move |_, _, x, y| {
            let Some(row) = weak.upgrade() else { return };
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

fn nav_row(icon: &str, label: &str) -> gtk::ListBoxRow {
    let row = ui::hbox(SPACE_3);
    row.append(&ui::icon(icon, &["text2"]));
    row.append(&ui::line(label, &["callout"]));
    let row = gtk::ListBoxRow::builder().child(&row).build();
    row.add_css_class("nav-row");
    row
}

/// A band's header; a folded one says whether it is open.
fn header_row(title: &str, open: Option<bool>, first: bool) -> gtk::ListBoxRow {
    let row = ui::hbox(SPACE_1);
    row.set_margin_top(if first { 6 } else { 14 });
    row.set_margin_bottom(6);
    row.set_margin_start(10);
    if let Some(open) = open {
        row.append(&ui::icon(if open { "pan-down-symbolic" } else { "pan-end-symbolic" }, &["text3"]));
    }
    row.append(&ui::micro(title, &["text3"]));
    gtk::ListBoxRow::builder().child(&row).selectable(false).activatable(false).build()
}
