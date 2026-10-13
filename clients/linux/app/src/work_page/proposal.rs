//! A breakdown waiting for approval, as the proposal canvas lays it out: the
//! proposed sub-goals to reorder, merge, edit, split or drop, and beside
//! them why the planner broke the goal down so.

use std::rc::Rc;

use adw::prelude::*;
use gtk::{gdk, glib};

use kit::{GoalDetail, Plan, ProposedCard, Reasoning};

use super::overview::ask_sub_goal;
use crate::app::App;
use crate::ui::{self, SPACE_2, SPACE_3};

/// What ask mode means, approve, re-plan or discard, then each proposed
/// sub-goal; the planner's reasoning beside them, under them when narrow.
pub(super) fn proposal(
    cards: &[ProposedCard],
    reasoning: Option<&Reasoning>,
    g: &Rc<GoalDetail>,
    app: &App,
) -> gtk::Widget {
    let goal = g.goal.id.as_str();
    let column = ui::vbox(14);
    column.set_hexpand(true);
    let bar = ui::hbox(SPACE_2);
    let ask = ui::line("Ask mode: nothing starts until you approve", &["meta", "text3"]);
    ask.set_hexpand(true);
    bar.append(&ask);
    let discard = ui::button("Discard", false);
    let replan = ui::button("Re-plan with a note…", false);
    let approve = ui::button("Approve and start", true);
    let (a, g_id) = (app.clone(), goal.to_string());
    discard.connect_clicked(move |_| a.run(Plan::cancel(&g_id, "discarded the proposed breakdown")));
    let (a, g_id) = (app.clone(), goal.to_string());
    replan.connect_clicked(move |_| ask_replan(&a, &g_id));
    let (a, g_id) = (app.clone(), goal.to_string());
    approve.connect_clicked(move |_| a.run(Plan::approve(&g_id)));
    bar.append(&discard);
    bar.append(&replan);
    bar.append(&approve);
    let page = ui::vbox(14);
    page.append(&bar);
    let head = ui::hbox(10);
    head.append(&ui::line("Proposed sub-goals", &["title"]));
    head.append(&ui::line("drag to reorder · each is about one agent's work", &["meta", "text3"]));
    column.append(&head);
    for (index, card) in cards.iter().enumerate() {
        let others: Vec<&ProposedCard> = cards.iter().filter(|c| c.id != card.id).collect();
        column.append(&proposed_card(card, index, &others, g, app));
    }
    let add = ui::button("+ Add sub-goal", false);
    add.set_halign(gtk::Align::Start);
    let (a, g_id) = (app.clone(), goal.to_string());
    add.connect_clicked(move |_| ask_sub_goal(&a, &g_id));
    column.append(&add);

    let Some(panel) = reasoning.and_then(reasoning_panel) else {
        page.append(&column);
        return page.upcast();
    };
    let body = ui::hbox(32);
    body.append(&column);
    body.append(&panel);
    // The window is narrow exactly when its sidebar folds away.
    app.sidebar_split
        .bind_property("collapsed", &body, "orientation")
        .transform_to(|_, narrow: bool| {
            Some(if narrow { gtk::Orientation::Vertical } else { gtk::Orientation::Horizontal })
        })
        .sync_create()
        .build();
    app.sidebar_split
        .bind_property("collapsed", &panel, "width-request")
        .transform_to(|_, narrow: bool| Some(if narrow { -1 } else { 280 }))
        .sync_create()
        .build();
    page.append(&body);
    page.upcast()
}

/// Why this breakdown, what was left out and how you'd judge it, each part
/// the planner gave; none when it gave none.
fn reasoning_panel(reasoning: &Reasoning) -> Option<gtk::Widget> {
    let parts = reasoning.parts();
    if parts.is_empty() {
        return None;
    }
    let panel = ui::vbox(SPACE_3);
    panel.add_css_class("reasoning");
    panel.set_valign(gtk::Align::Start);
    for (head, said) in parts {
        let section = ui::vbox(SPACE_2);
        section.append(&ui::label(head));
        for paragraph in said.split("\n\n").map(str::trim).filter(|p| !p.is_empty()) {
            section.append(&ui::said(paragraph, &["callout"]));
        }
        panel.append(&section);
    }
    Some(panel.upcast())
}

/// One proposed sub-goal: dragged by its handle to reorder, and edited,
/// split, merged with another or dropped from its links.
fn proposed_card(
    card: &ProposedCard,
    index: usize,
    others: &[&ProposedCard],
    g: &Rc<GoalDetail>,
    app: &App,
) -> gtk::Widget {
    let row = ui::hbox(SPACE_3);
    row.add_css_class("wd-card");
    row.add_css_class("proposed-card");
    let handle = ui::line("⠿", &["drag-handle", "text3"]);
    handle.set_valign(gtk::Align::Start);
    handle.set_tooltip_text(Some("Drag to reorder"));
    row.append(&handle);
    let number = ui::line(&card.number.to_string(), &["number-dot"]);
    number.set_xalign(0.5);
    number.set_valign(gtk::Align::Start);
    row.append(&number);
    let words = ui::vbox(6);
    words.set_hexpand(true);
    words.append(&ui::text(&card.title, &["body", "medium"]));
    words.append(&ui::text(&card.meta, &["meta", "text3"]));
    let chips = ui::hbox(6);
    for chip in &card.chips {
        chips.append(&ui::line(chip, &["chip-tag"]));
    }
    words.append(&chips);
    row.append(&words);

    let links = ui::hbox(2);
    links.set_valign(gtk::Align::Start);
    let edit = ui::inline_link("Edit");
    named(&edit, "Edit", card, "");
    let (a, c) = (app.clone(), card.clone());
    edit.connect_clicked(move |_| ask_edit(&a, &c));
    links.append(&edit);
    if card.splits {
        let split = ui::inline_link("Split");
        named(&split, "Split", card, "");
        let (a, id) = (app.clone(), card.id.clone());
        split.connect_clicked(move |_| a.run(Plan::split_sub_goal(&id)));
        links.append(&split);
    }
    match others {
        [] => {}
        [other] => {
            let merge = ui::inline_link(&format!("Merge with {}", other.number));
            named(&merge, "Merge", card, &format!(" with {}", other.number));
            let (a, plan) = (app.clone(), card.merge(other));
            merge.connect_clicked(move |_| a.run(plan.clone()));
            links.append(&merge);
        }
        _ => {
            let list = ui::vbox(0);
            let popover = gtk::Popover::new();
            for other in others {
                let pick = ui::inline_link(&format!("{} · {}", other.number, other.title));
                pick.set_halign(gtk::Align::Start);
                let (a, plan, p) = (app.clone(), card.merge(other), popover.clone());
                pick.connect_clicked(move |_| {
                    p.popdown();
                    a.run(plan.clone());
                });
                list.append(&pick);
            }
            popover.set_child(Some(&list));
            let merge = gtk::MenuButton::builder().label("Merge with…").popover(&popover).build();
            merge.add_css_class("inline-link");
            named(&merge, "Merge", card, " with…");
            links.append(&merge);
        }
    }
    let drop = ui::inline_link("Drop");
    named(&drop, "Drop", card, "");
    let (a, id, title) = (app.clone(), card.id.clone(), card.title.clone());
    drop.connect_clicked(move |_| {
        let id = id.clone();
        a.ask(&format!("Drop {title}"), "Why it is not needed, in one line.", "Drop", "", move |why| {
            Plan::drop_sub_goal(&id, why)
        });
    });
    links.append(&drop);
    row.append(&links);

    // A card drags from anywhere on it, so its words are copied from its menu instead.
    row.add_css_class("draggable");
    ui::context_menu(&row, &ui::copy_menu(&format!("{}\n{}", card.title, card.meta)));
    let drag = gtk::DragSource::new();
    drag.set_actions(gdk::DragAction::MOVE);
    let id = card.id.clone();
    drag.connect_prepare(move |_, _, _| Some(gdk::ContentProvider::for_value(&id.to_value())));
    row.add_controller(drag);
    let target = gtk::DropTarget::new(glib::Type::STRING, gdk::DragAction::MOVE);
    let r = row.clone();
    target.connect_enter(move |_, _, _| {
        r.add_css_class("drop-above");
        gdk::DragAction::MOVE
    });
    let r = row.clone();
    target.connect_leave(move |_| r.remove_css_class("drop-above"));
    let (a, g, r, id) = (app.clone(), g.clone(), row.clone(), card.id.clone());
    target.connect_drop(move |_, value, _, _| {
        r.remove_css_class("drop-above");
        let Ok(moved) = value.get::<String>() else { return false };
        if moved == id {
            return false;
        }
        let plans = g.reorder(&moved, index);
        let app = a.clone();
        a.spawn(async move {
            app.run_in_turn(&plans).await;
        });
        true
    });
    row.add_controller(target);
    row.upcast()
}

/// A card's action announced with the card it acts on: there is one per card.
fn named(control: &impl IsA<gtk::Widget>, action: &str, card: &ProposedCard, then: &str) {
    ui::announce(control, &card.announced(action, then));
}

/// Asks a proposed sub-goal's new title and done-when; only what changed is sent.
fn ask_edit(app: &App, card: &ProposedCard) {
    let dialog = adw::AlertDialog::new(Some("Edit the sub-goal"), None);
    dialog.add_responses(&[("cancel", "Cancel"), ("save", "Save")]);
    dialog.set_response_appearance("save", adw::ResponseAppearance::Suggested);
    dialog.set_default_response(Some("save"));
    dialog.set_close_response("cancel");
    let done = card.meta.strip_prefix("Done when ").unwrap_or("").to_string();
    let form = ui::vbox(SPACE_2);
    let title = gtk::Entry::builder().text(card.title.as_str()).activates_default(true).build();
    let done_when =
        gtk::Entry::builder().text(done.as_str()).placeholder_text("Done when…").activates_default(true).build();
    form.append(&title);
    form.append(&done_when);
    dialog.set_extra_child(Some(&form));
    let (a, id, was_title, t, d) = (app.clone(), card.id.clone(), card.title.clone(), title.clone(), done_when.clone());
    dialog.connect_response(None, move |_, response| {
        if response != "save" {
            return;
        }
        let (new_title, new_done) = (t.text().to_string(), d.text().to_string());
        let title = (new_title.trim() != was_title).then_some(new_title.as_str());
        let done_when = (new_done.trim() != done).then_some(new_done.as_str());
        match Plan::edit_sub_goal(&id, title, done_when) {
            Ok(plan) => a.run(plan),
            Err(e) => a.complain(&e.to_string()),
        }
    });
    dialog.present(Some(&app.window));
    dialog.set_focus(Some(&title));
}

/// Asks a note for the next plan, keeps it as a decision, then plans again.
fn ask_replan(app: &App, goal: &str) {
    let dialog = adw::AlertDialog::new(
        Some("Re-plan with a note"),
        Some("What the next breakdown should do differently. It is kept as a decision."),
    );
    dialog.add_responses(&[("cancel", "Cancel"), ("go", "Re-plan")]);
    dialog.set_response_appearance("go", adw::ResponseAppearance::Suggested);
    dialog.set_default_response(Some("go"));
    dialog.set_close_response("cancel");
    let entry = gtk::Entry::builder().activates_default(true).build();
    dialog.set_extra_child(Some(&entry));
    let (a, g, e) = (app.clone(), goal.to_string(), entry.clone());
    dialog.connect_response(None, move |_, response| {
        if response != "go" {
            return;
        }
        match Plan::replan_with(&g, &e.text()) {
            Ok(plans) => {
                let app = a.clone();
                a.spawn(async move {
                    app.run_in_turn(&plans).await;
                });
            }
            Err(why) => a.complain(&why.to_string()),
        }
    });
    dialog.present(Some(&app.window));
    dialog.set_focus(Some(&entry));
}
