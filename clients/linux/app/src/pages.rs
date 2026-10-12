//! The pages under More: work put out of sight, the taste every brief
//! carries, and what the loop decided for a product.

use std::rc::Rc;

use adw::prelude::*;

use kit::{Plan, Watch};

use crate::app::{App, Place};
use crate::ui;

/// A page the window shows and keeps its subscription alive with.
pub struct Shown {
    pub widget: gtk::Widget,
    pub _watch: Option<Watch>,
}

/// A page's frame: a header with the sidebar toggle and a title, then its content.
fn frame(app: &App, title: &str, content: &impl IsA<gtk::Widget>, end: Option<&gtk::Widget>) -> adw::ToolbarView {
    let bar = adw::HeaderBar::new();
    bar.set_title_widget(Some(&adw::WindowTitle::new(title, "")));
    let sidebar = gtk::ToggleButton::builder().icon_name("sidebar-show-symbolic").tooltip_text("Show the goals").build();
    app.sidebar_split.bind_property("show-sidebar", &sidebar, "active").bidirectional().sync_create().build();
    bar.pack_start(&sidebar);
    if let Some(end) = end {
        bar.pack_end(end);
    }
    let view = adw::ToolbarView::new();
    view.add_top_bar(&bar);
    view.set_content(Some(content));
    view
}

/// Replaces the content of a preferences page with what `fill` adds.
fn refill(page: &adw::PreferencesPage, groups: &std::cell::RefCell<Vec<adw::PreferencesGroup>>, fill: Vec<adw::PreferencesGroup>) {
    for g in groups.borrow_mut().drain(..) {
        page.remove(&g);
    }
    for g in &fill {
        page.add(g);
    }
    *groups.borrow_mut() = fill;
}

fn row(title: &str, subtitle: &str) -> adw::ActionRow {
    let row = adw::ActionRow::builder().title(title).use_markup(false).build();
    if !subtitle.is_empty() {
        row.set_subtitle(subtitle);
    }
    row
}

fn failed(why: &str) -> adw::PreferencesGroup {
    let group = adw::PreferencesGroup::new();
    group.add(&ui::failure(why));
    group
}

/// Work put out of sight, with the way back.
pub fn archived(app: &App, scope: Option<String>) -> Shown {
    let page = adw::PreferencesPage::new();
    let groups = Rc::new(std::cell::RefCell::new(Vec::new()));
    let load = {
        let (app, page, groups) = (app.clone(), page.clone(), Rc::clone(&groups));
        Rc::new(move || {
            let (app, page, groups, scope) = (app.clone(), page.clone(), Rc::clone(&groups), scope.clone());
            app.clone().spawn(async move {
                let group = match app.store.archived().await {
                    Ok(items) => {
                        let shown: Vec<_> = items.into_iter().filter(|w| scope.as_ref().is_none_or(|s| &w.project == s)).collect();
                        let group = adw::PreferencesGroup::builder()
                            .title("Archived")
                            .description(format!("{} item{}", shown.len(), if shown.len() == 1 { "" } else { "s" }))
                            .build();
                        for w in shown {
                            let r = row(&w.title, &format!("{} · {} · {}", w.project, w.kind.name(), w.id));
                            r.add_prefix(&ui::state_icon(w.state));
                            let back = gtk::Button::builder().label("Unarchive").valign(gtk::Align::Center).build();
                            let (a, id) = (app.clone(), w.id.clone());
                            back.connect_clicked(move |_| a.run(Plan::unarchive(&id)));
                            r.add_suffix(&back);
                            r.set_activatable(true);
                            let (a, id) = (app.clone(), w.id.clone());
                            r.connect_activated(move |_| a.open(Place::Work(id.clone())));
                            group.add(&r);
                        }
                        group
                    }
                    Err(e) => failed(&format!("Cannot read the archive: {e}")),
                };
                refill(&page, &groups, vec![group]);
            });
        })
    };
    load();
    let again = Rc::clone(&load);
    let watch = app.store.watch(move |change| {
        if change == kit::Change::Board {
            again();
        }
    });
    Shown { widget: frame(app, "Archived", &page, None).upcast(), _watch: Some(watch) }
}

/// The rules in effect, and what feedback has gathered toward new ones.
pub fn taste(app: &App, scope: Option<String>) -> Shown {
    let page = adw::PreferencesPage::new();
    let groups = Rc::new(std::cell::RefCell::new(Vec::new()));
    let teach = adw::PreferencesGroup::builder()
        .title("Teach")
        .description("A rule in your words, filed as feedback; enough of it becomes a rule every brief carries.")
        .build();
    let entry = adw::EntryRow::builder().title("Always…").show_apply_button(true).build();
    let (a, s) = (app.clone(), scope.clone());
    entry.connect_apply(move |e| match Plan::feedback(&e.text(), s.as_deref()) {
        Ok(plan) => {
            a.run(plan);
            e.set_text("");
        }
        Err(err) => a.complain(&err.to_string()),
    });
    teach.add(&entry);
    page.add(&teach);
    let (a, p, g) = (app.clone(), page.clone(), Rc::clone(&groups));
    app.spawn(async move {
        let fill = match a.store.taste().await {
            Ok(taste) => {
                let rules = adw::PreferencesGroup::builder().title("Rules").description(format!("{} in effect", taste.cards.len())).build();
                for card in &taste.cards {
                    let r = row(&card.title, &card.statement);
                    r.add_suffix(&ui::tag(&card.source, &["meta", "text2"]));
                    rules.add(&r);
                }
                let learned = adw::PreferencesGroup::builder().title("Learned from feedback").build();
                for l in &taste.learned {
                    let r = adw::ExpanderRow::builder().title(ui::escaped(&l.key)).subtitle(format!("{} times", l.count)).build();
                    for text in &l.texts {
                        r.add_row(&row(text, ""));
                    }
                    learned.add(&r);
                }
                vec![rules, learned]
            }
            Err(e) => vec![failed(&format!("Cannot read the taste: {e}"))],
        };
        refill(&p, &g, fill);
    });
    Shown { widget: frame(app, "Taste", &page, None).upcast(), _watch: None }
}

/// What the loop decided for a product since it was last reviewed, with the way to acknowledge it.
pub fn review(app: &App, project: &str) -> Shown {
    let page = adw::PreferencesPage::new();
    let groups = Rc::new(std::cell::RefCell::new(Vec::new()));
    let ack = gtk::Button::with_label("Acknowledge");
    ack.add_css_class("suggested-action");
    let (a, p) = (app.clone(), project.to_string());
    ack.connect_clicked(move |_| a.run(Plan::acknowledge(&p)));
    let (a, pg, g, project_name) = (app.clone(), page.clone(), Rc::clone(&groups), project.to_string());
    app.spawn(async move {
        let fill = match a.store.review(&project_name).await {
            Ok(pass) if pass.is_empty() => {
                let group = adw::PreferencesGroup::builder().title("Nothing to review").description("The loop decided nothing since the last review.").build();
                vec![group]
            }
            Ok(pass) => {
                let mut fill = Vec::new();
                if !pass.claims.is_empty() {
                    let group = adw::PreferencesGroup::builder().title("What the loop decided").build();
                    for c in &pass.claims {
                        let on = c.work.as_ref().map(|w| w.title.clone()).unwrap_or_else(|| c.event.work.clone());
                        let stands = if c.stands { "stands".to_string() } else { format!("reversed: {}", c.reversed) };
                        group.add(&row(&c.event.body, &format!("{on} · {stands}")));
                    }
                    fill.push(group);
                }
                if !pass.cards.is_empty() {
                    let group = adw::PreferencesGroup::builder().title("Rules promoted").build();
                    for c in &pass.cards {
                        group.add(&row(&c.card, &c.text));
                    }
                    fill.push(group);
                }
                if !pass.unshipped.is_empty() {
                    let group = adw::PreferencesGroup::builder().title("Not shipped").build();
                    for u in &pass.unshipped {
                        group.add(&row(&u.work.title, &u.reason));
                    }
                    fill.push(group);
                }
                if !pass.landed.is_empty() {
                    let group = adw::PreferencesGroup::builder().title("Landed").build();
                    for l in &pass.landed {
                        let title = l.work.as_ref().map(|w| w.title.clone()).unwrap_or_default();
                        group.add(&row(&title, &l.url));
                    }
                    fill.push(group);
                }
                fill
            }
            Err(e) => vec![failed(&format!("Cannot read the review: {e}"))],
        };
        refill(&pg, &g, fill);
    });
    Shown { widget: frame(app, &format!("Review {project}"), &page, Some(&ack.upcast())).upcast(), _watch: None }
}
