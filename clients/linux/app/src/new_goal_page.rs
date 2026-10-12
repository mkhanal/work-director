//! The home page: say what should get done. Before anything starts, the
//! goals it may continue are offered in its place.

use std::cell::RefCell;
use std::rc::{Rc, Weak};

use adw::prelude::*;

use kit::{Found, NewGoal, NewGoalPhase, Watch};

use crate::app::{App, Place};
use crate::composer::Composer;
use crate::ui::{self, SPACE_2, SPACE_3, SPACE_4, SPACE_6, SPACE_7};

pub struct NewGoalPage {
    app: App,
    me: Weak<NewGoalPage>,
    pub widget: adw::ToolbarView,
    flow: Rc<NewGoal>,
    /// The product in scope, or none for all products.
    scope: Option<String>,
    products: gtk::DropDown,
    offered: RefCell<Vec<String>>,
    composer: Rc<Composer>,
    status: gtk::Box,
    empty: adw::StatusPage,
    form: gtk::Box,
    watch: RefCell<Option<Watch>>,
}

impl NewGoalPage {
    pub fn new(app: &App, scope: Option<String>) -> Rc<NewGoalPage> {
        let column = ui::vbox(SPACE_4);
        column.set_margin_top(SPACE_7);
        column.set_margin_bottom(SPACE_6);
        column.set_margin_start(SPACE_6);
        column.set_margin_end(SPACE_6);
        column.append(&ui::text("What should get done?", &["display"]));
        let product_row = ui::hbox(SPACE_2);
        product_row.append(&ui::line("Product", &["meta", "text2"]));
        let products = gtk::DropDown::from_strings(&[]);
        products.add_css_class("flat");
        match &scope {
            Some(name) => product_row.append(&ui::line(name, &["meta", "strong"])),
            None => product_row.append(&products),
        }
        column.append(&product_row);
        let composer = Composer::new(Rc::clone(&app.settings), 4, 12);
        composer.set_placeholder("What do you want done? Work Director checks for a goal it continues, then plans and starts it");
        composer.set_remember_offered(false);
        column.append(&composer.widget);
        let status = ui::vbox(SPACE_3);
        column.append(&status);

        let form = column;
        let empty = adw::StatusPage::builder()
            .icon_name("folder-new-symbolic")
            .title("No product yet")
            .description("Add the repository you want worked on, then say what you want done.")
            .build();
        let add = gtk::Button::builder().label("Add Product…").action_name("win.add-product").halign(gtk::Align::Center).build();
        add.add_css_class("suggested-action");
        add.add_css_class("pill");
        empty.set_child(Some(&add));
        let body = ui::vbox(0);
        body.append(&form);
        body.append(&empty);
        let clamp = adw::Clamp::builder().maximum_size(640).child(&body).build();
        let scroller = gtk::ScrolledWindow::builder().hscrollbar_policy(gtk::PolicyType::Never).vexpand(true).child(&clamp).build();

        let bar = adw::HeaderBar::new();
        bar.set_show_title(false);
        let sidebar = gtk::ToggleButton::builder().icon_name("sidebar-show-symbolic").tooltip_text("Show the goals").build();
        app.sidebar_split.bind_property("show-sidebar", &sidebar, "active").bidirectional().sync_create().build();
        bar.pack_start(&sidebar);
        let widget = adw::ToolbarView::new();
        widget.add_top_bar(&bar);
        widget.set_content(Some(&scroller));

        let page = Rc::new_cyclic(|me: &Weak<NewGoalPage>| NewGoalPage {
            app: app.clone(),
            me: me.clone(),
            widget,
            flow: Rc::new(NewGoal::new(Rc::clone(&app.store))),
            scope,
            products,
            offered: RefCell::new(Vec::new()),
            composer,
            status,
            empty,
            form,
            watch: RefCell::new(None),
        });
        let weak = page.me.clone();
        page.composer.connect_send(move |text| {
            if let Some(page) = weak.upgrade() {
                page.submit(text);
            }
        });
        let weak = page.me.clone();
        *page.watch.borrow_mut() = Some(app.store.watch(move |change| {
            if change == kit::Change::Projects {
                if let Some(page) = weak.upgrade() {
                    page.render_products();
                }
            }
        }));
        page.render_products();
        page.render();
        page.composer.focus();
        page
    }

    fn render_products(&self) {
        let names: Vec<String> = self.app.store.projects().iter().map(|p| p.name.clone()).collect();
        let strs: Vec<&str> = names.iter().map(String::as_str).collect();
        self.products.set_model(Some(&gtk::StringList::new(&strs)));
        let empty = names.is_empty();
        *self.offered.borrow_mut() = names;
        self.empty.set_visible(empty);
        self.form.set_visible(!empty);
    }

    /// The product the goal is filed in: the one in scope, else the one chosen.
    fn product(&self) -> Option<String> {
        self.scope.clone().or_else(|| {
            usize::try_from(self.products.selected()).ok().and_then(|i| self.offered.borrow().get(i).cloned())
        })
    }

    fn submit(self: &Rc<Self>, text: String) {
        let Some(product) = self.product() else {
            self.app.complain("Choose a product to file the goal in");
            return;
        };
        let page = Rc::clone(self);
        self.app.spawn(async move {
            page.composer.set_busy(true);
            page.render_phase(&NewGoalPhase::Finding);
            page.flow.submit(&product, &text).await;
            page.after(&product, &text);
        });
    }

    fn after(self: &Rc<Self>, product: &str, text: &str) {
        self.composer.set_busy(false);
        let phase = self.flow.phase();
        if let NewGoalPhase::Started(started) = &phase {
            self.composer.clear();
            self.flow.reset();
            self.app.open(Place::Work(started.goal.id.clone()));
            return;
        }
        self.render_offers(&phase, product, text);
    }

    fn render(&self) {
        self.render_phase(&self.flow.phase());
    }

    fn render_phase(&self, phase: &NewGoalPhase) {
        ui::clear(&self.status);
        match phase {
            NewGoalPhase::Idle | NewGoalPhase::Started(_) => self.status.append(&ui::text(
                "Say what you want done, the way you would start a conversation. The first line becomes the title. \
                 If it continues a goal you already have, you can pick that goal up instead of starting a new one.",
                &["callout", "text2"],
            )),
            NewGoalPhase::Finding => self.status.append(&ui::waiting("Looking for goals this continues…")),
            NewGoalPhase::Starting => self.status.append(&ui::waiting("Planning and starting its tasks…")),
            NewGoalPhase::Failed(why) => self.status.append(&ui::failure(why)),
            NewGoalPhase::Offering(_) => {}
        }
    }

    fn render_offers(self: &Rc<Self>, phase: &NewGoalPhase, product: &str, text: &str) {
        self.render_phase(phase);
        let NewGoalPhase::Offering(found) = phase else { return };
        self.status.append(&ui::line(&"This may continue a goal you already have".to_uppercase(), &["micro", "text3"]));
        for f in found {
            self.status.append(&self.offer(f, text));
        }
        let fresh = ui::link("Start a new goal instead");
        let (weak, product, text) = (self.me.clone(), product.to_string(), text.to_string());
        fresh.connect_clicked(move |_| {
            let Some(page) = weak.upgrade() else { return };
            let (product, text) = (product.clone(), text.clone());
            page.clone().app.spawn(async move {
                page.render_phase(&NewGoalPhase::Starting);
                page.flow.start_new(&product, &text).await;
                page.after(&product, &text);
            });
        });
        self.status.append(&fresh);
    }

    fn offer(self: &Rc<Self>, f: &Found, text: &str) -> gtk::Widget {
        let card = ui::card("raised");
        let top = ui::hbox(SPACE_2);
        let title = ui::text(&f.goal.title, &["body", "strong"]);
        title.set_hexpand(true);
        top.append(&title);
        top.append(&ui::state_pill(f.goal.state));
        card.append(&top);
        card.append(&ui::said(&f.why, &["callout", "text2"]));
        let bottom = ui::hbox(SPACE_2);
        let id = ui::line(&f.goal.id, &["meta", "code", "text3"]);
        id.set_hexpand(true);
        bottom.append(&id);
        let go = gtk::Button::with_label("Continue this goal");
        go.add_css_class("suggested-action");
        let (weak, goal, text) = (self.me.clone(), f.goal.id.clone(), text.to_string());
        go.connect_clicked(move |_| {
            let Some(page) = weak.upgrade() else { return };
            let (goal, text) = (goal.clone(), text.clone());
            page.clone().app.spawn(async move {
                page.render_phase(&NewGoalPhase::Starting);
                page.flow.continue_goal(&goal, &text).await;
                page.after(&goal, &text);
            });
        });
        bottom.append(&go);
        card.append(&bottom);
        card.upcast()
    }
}
