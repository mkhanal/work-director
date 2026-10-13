//! What waits on the person, as the canvas lays each kind out: a course
//! change with what was found and what becomes of each sub-goal, answered by
//! its three buttons; a plan change; a finish-line question with why it is
//! asked; and any other question, answered in place or left to the director.

use std::rc::Rc;

use adw::prelude::*;

use kit::{ChangeKind, FinishLine, GoalChange, GoalDetail, NeedYou, Plan};

use super::WorkPage;
use crate::app::App;
use crate::question::{self, Card};
use crate::turns::Turn;
use crate::ui::{self, SPACE_2, SPACE_3};

impl WorkPage {
    /// What the director just decided of questions no longer open, which the
    /// overview shows where the question was; the conversation reads it from
    /// the ledger with the answer.
    pub(super) fn decided_turns(&self, g: &GoalDetail) -> Vec<Turn> {
        let said = self.director_said.borrow();
        let mut decided: Vec<_> = said.iter().filter(|(work, s)| s.answered && !asking(g, work)).collect();
        decided.sort_by(|a, b| a.0.cmp(b.0));
        decided
            .into_iter()
            .map(|(work, s)| {
                let s = s.clone();
                Turn::key(format!("decided-{work}"), &s).with(move || question::director_decided(&s))
            })
            .collect()
    }

    /// Every question waiting on the person anywhere under the goal, answered
    /// in place, and each question a task's session asks that the goal view
    /// does not hold yet.
    pub(super) fn asking_turns(self: &Rc<Self>, g: &GoalDetail) -> Vec<Turn> {
        // A decline is about the question it declined; once that is answered it says nothing.
        self.director_said.borrow_mut().retain(|work, said| said.answered || asking(g, work));
        let said = self.director_said.borrow().clone();
        let declined = |work: &str| said.get(work).filter(|s| !s.answered).cloned();
        let mut turns = Vec::new();
        for need in &g.needs_you {
            let (app, answered, heard, need) = (self.app.clone(), self.reload(), self.heard(&need.work), need.clone());
            if need.change.as_ref().is_some_and(|c| c.kind == ChangeKind::Course) {
                turns.push(
                    Turn::key(format!("asking-{}", need.work), &need)
                        .with(move || course_change(&app, &need, answered)),
                );
                continue;
            }
            let card = (need.label(&g.goal.id), need.remember_label(&g.goal.project), declined(&need.work));
            turns.push(Turn::key(format!("asking-{}", need.work), &(&need, &card)).with(move || {
                let card = Card {
                    label: card.0,
                    extra: plan_change(&need),
                    after: sources(&need),
                    remember: card.1,
                    declined: card.2,
                };
                question::pending(&app, &need.work, &need.shown_question(), card, answered, heard)
            }));
        }
        let sessions = g
            .tasks
            .iter()
            .filter(|t| g.needs_you.iter().all(|n| n.work != t.id))
            .filter_map(|t| g.activity.get(&t.id).and_then(|a| a.asking.clone()).map(|q| (t.clone(), q)));
        for (task, q) in sessions {
            let label = match g.paths.get(&task.id) {
                Some(path) => format!("Needs you · in {path}"),
                None => format!("Needs you · in {}", task.title),
            };
            let (app, answered, heard, declined) =
                (self.app.clone(), self.reload(), self.heard(&task.id), declined(&task.id));
            turns.push(Turn::key(format!("asking-{}", task.id), &(&task, &q, &declined)).with(move || {
                let card = Card { declined, ..Card::headed(&label) };
                question::pending(&app, &task.id, &q, card, answered, heard)
            }));
        }
        turns
    }
}

/// Whether work under the goal waits on the person for an answer.
fn asking(g: &GoalDetail, work: &str) -> bool {
    g.needs_you.iter().any(|n| n.work == work) || g.activity.get(work).is_some_and(|a| a.asking.is_some())
}

/// What was found and why change course, side by side; either alone takes
/// the row.
fn found_and_why(change: &GoalChange) -> Option<gtk::Widget> {
    let parts: Vec<(&str, &str)> =
        [("What was found", change.found.as_deref()), ("Why change course", Some(change.why.as_str()))]
            .into_iter()
            .filter_map(|(head, said)| said.filter(|s| !s.trim().is_empty()).map(|s| (head, s)))
            .collect();
    if parts.is_empty() {
        return None;
    }
    let grid = gtk::Grid::builder().column_spacing(24).column_homogeneous(true).build();
    for (i, (head, said)) in parts.iter().enumerate() {
        let part = ui::vbox(2);
        part.append(&ui::label(head));
        part.append(&ui::said(said, &["callout"]));
        part.set_hexpand(true);
        grid.attach(&part, i as i32, 0, 1, 1);
    }
    Some(grid.upcast())
}

/// A course change's goal now and as proposed, row by row.
fn compare(need: &NeedYou) -> Option<gtk::Widget> {
    let rows = need.compare();
    if rows.is_empty() {
        return None;
    }
    let grid = gtk::Grid::builder().column_homogeneous(false).build();
    grid.add_css_class("compare");
    let cell = |widget: &gtk::Label, col: i32, row: i32, last: bool, width: Option<i32>| {
        let b = ui::hbox(0);
        b.add_css_class("cell");
        if last {
            b.add_css_class("last");
        }
        match width {
            Some(w) => b.set_size_request(w, -1),
            None => b.set_hexpand(true),
        }
        widget.set_hexpand(true);
        b.append(widget);
        grid.attach(&b, col, row, 1, 1);
    };
    cell(&ui::line(" ", &["meta", "text3"]), 0, 0, false, Some(120));
    cell(&ui::line("Now", &["meta", "text3", "strong"]), 1, 0, false, None);
    cell(&ui::line("Proposed", &["meta", "text3", "strong"]), 2, 0, false, None);
    for (i, (name, now, proposed)) in rows.iter().enumerate() {
        let last = i + 1 == rows.len();
        let r = i as i32 + 1;
        cell(&ui::line(name, &["meta", "text3"]), 0, r, last, Some(120));
        cell(&ui::text(now, &["callout", "struck"]), 1, r, last, None);
        cell(&ui::text(proposed, &["callout"]), 2, r, last, None);
    }
    Some(grid.upcast())
}

/// Each sub-goal as the change would leave it, badged with its fate; with
/// none served, each change the plan makes in a sentence.
fn fates(change: &GoalChange, heading: &str) -> Option<gtk::Widget> {
    if change.sub_goals.is_empty() && change.lines.is_empty() {
        return None;
    }
    let column = ui::vbox(6);
    column.append(&ui::label(heading));
    for fate in &change.sub_goals {
        let row = ui::hbox(SPACE_2);
        let (text, tone) = fate.badge();
        let badge = ui::badge(&text, tone);
        badge.set_valign(gtk::Align::Start);
        row.append(&badge);
        let classes: &[&str] = if fate.struck() { &["callout", "struck"] } else { &["callout"] };
        row.append(&ui::text(&fate.title, classes));
        if let Some(why) = fate.why.as_deref().filter(|w| !w.is_empty()) {
            let said = ui::text(why, &["meta", "text3"]);
            said.set_hexpand(true);
            row.append(&said);
        }
        column.append(&row);
    }
    if change.sub_goals.is_empty() {
        for line in &change.lines {
            let row = ui::hbox(SPACE_2);
            let bullet = ui::line("•", &["callout", "text3"]);
            bullet.set_valign(gtk::Align::Start);
            row.append(&bullet);
            let said = ui::said(&kit::sentence(line.trim_start_matches("- ")), &["callout"]);
            said.set_hexpand(true);
            row.append(&said);
            column.append(&row);
        }
    }
    Some(column.upcast())
}

/// What a plan change would change: what was found, and each sub-goal after it.
fn plan_change(need: &NeedYou) -> Option<gtk::Widget> {
    let change = need.change.as_ref().filter(|c| c.kind == ChangeKind::Plan)?;
    let column = ui::vbox(SPACE_3);
    column.set_margin_top(SPACE_2);
    if let Some(found) = change.found.as_deref().filter(|f| !f.trim().is_empty()) {
        let part = ui::vbox(2);
        part.append(&ui::label("What was found"));
        part.append(&ui::said(found, &["callout"]));
        column.append(&part);
    }
    if let Some(rows) = fates(change, "What would change") {
        column.append(&rows);
    }
    Some(column.upcast())
}

/// Why a finish line is asked: what each source said, when they disagree.
fn sources(need: &NeedYou) -> Option<gtk::Widget> {
    let said = need.sources();
    if said.is_empty() {
        return None;
    }
    let column = ui::vbox(6);
    column.add_css_class("sources");
    column.set_margin_top(SPACE_2);
    column.append(&ui::line("Why I'm asking: these disagree", &["meta", "text2", "medium"]));
    let grid = gtk::Grid::builder().column_spacing(SPACE_3).row_spacing(6).build();
    for (i, (who, what)) in said.iter().enumerate() {
        let name = ui::line(who, &["meta", "text3"]);
        name.set_size_request(110, -1);
        name.set_valign(gtk::Align::Start);
        grid.attach(&name, 0, i as i32, 1, 1);
        let text = ui::said(what, &["callout"]);
        text.set_hexpand(true);
        grid.attach(&text, 1, i as i32, 1, 1);
    }
    column.append(&grid);
    Some(column.upcast())
}

/// A course change on the top goal: what was found and why, the goal now and
/// as proposed, each sub-goal after it, then keep it, edit it or make it.
fn course_change(app: &App, need: &NeedYou, answered: Rc<dyn Fn()>) -> gtk::Widget {
    let card = ui::vbox(SPACE_3);
    card.add_css_class("question");
    let head = ui::label(&need.label(""));
    head.add_css_class("attention-text");
    card.append(&head);
    let asked = need.shown_question().items.first().map(|i| i.question.clone()).unwrap_or_default();
    card.append(&ui::said(&asked, &["body", "medium"]));
    let Some(change) = need.change.as_ref() else { return card.upcast() };
    for part in
        [found_and_why(change), compare(need), fates(change, "Sub-goals after the change")].into_iter().flatten()
    {
        card.append(&part);
    }

    let failure = ui::text("", &["callout", "failure"]);
    failure.set_visible(false);
    let footer = ui::hbox(SPACE_2);
    footer.set_margin_top(6);
    let spacer = ui::hbox(0);
    spacer.set_hexpand(true);
    footer.append(&spacer);
    let keep = ui::button(need.keep_label().as_deref().unwrap_or("Keep it"), false);
    let edit = ui::button("Edit the change…", false);
    let make = ui::button("Change course · Recommended", true);
    let buttons = [keep.clone(), edit.clone(), make.clone()];
    let send: Rc<dyn Fn(Vec<Plan>)> = {
        let (app, failure, buttons) = (app.clone(), failure.clone(), buttons.clone());
        Rc::new(move |plans: Vec<Plan>| {
            for b in &buttons {
                b.set_sensitive(false);
            }
            let (app, failure, buttons, answered) = (app.clone(), failure.clone(), buttons.clone(), answered.clone());
            app.clone().spawn(async move {
                if app.run_in_turn(&plans).await {
                    failure.set_visible(false);
                    answered();
                } else {
                    failure.set_label("Not every step ran; the failure says which.");
                    failure.set_visible(true);
                    for b in &buttons {
                        b.set_sensitive(true);
                    }
                }
            });
        })
    };
    for (button, plan) in [(&keep, need.keep()), (&make, need.change_course())] {
        match plan {
            Some(plan) => {
                button.set_tooltip_text(Some(&plan.command_line()));
                let s = send.clone();
                button.connect_clicked(move |_| s(vec![plan.clone()]));
            }
            None => button.set_sensitive(false),
        }
    }
    let (a, n, s) = (app.clone(), need.clone(), send.clone());
    edit.connect_clicked(move |_| ask_edit_change(&a, &n, s.clone()));
    for b in &buttons {
        footer.append(b);
    }
    card.append(&footer);
    card.append(&failure);
    card.upcast()
}

/// Asks the proposed title, done-when and finish line as the person would
/// have them, then makes the change with what they edited.
fn ask_edit_change(app: &App, need: &NeedYou, send: Rc<dyn Fn(Vec<Plan>)>) {
    let Some(proposed) = need.change.as_ref().and_then(|c| c.proposed.clone()) else { return };
    let dialog =
        adw::AlertDialog::new(Some("Edit the change"), Some("The goal as it will be once the course changes."));
    dialog.add_responses(&[("cancel", "Cancel"), ("go", "Change course")]);
    dialog.set_response_appearance("go", adw::ResponseAppearance::Suggested);
    dialog.set_default_response(Some("go"));
    dialog.set_close_response("cancel");
    let form = ui::vbox(SPACE_2);
    let title = gtk::Entry::builder().text(proposed.title.as_str()).activates_default(true).build();
    let done_when = gtk::Entry::builder()
        .text(proposed.done_when.as_deref().unwrap_or(""))
        .placeholder_text("Done when…")
        .activates_default(true)
        .build();
    let names: Vec<&str> = FinishLine::ALL.iter().map(|f| f.choice().0).collect();
    let finish = gtk::DropDown::from_strings(&names);
    let at = proposed.finish.and_then(|f| FinishLine::ALL.iter().position(|l| *l == f));
    finish.set_selected(at.map_or(gtk::INVALID_LIST_POSITION, |i| i as u32));
    form.append(&ui::label("Goal"));
    form.append(&title);
    form.append(&ui::label("Done when"));
    form.append(&done_when);
    form.append(&ui::label("Finishes at"));
    form.append(&finish);
    dialog.set_extra_child(Some(&form));
    let (a, n, t, d, f) = (app.clone(), need.clone(), title.clone(), done_when.clone(), finish.clone());
    dialog.connect_response(None, move |_, response| {
        if response != "go" {
            return;
        }
        let Some(line) = FinishLine::ALL.get(f.selected() as usize).copied() else {
            return a.complain("Pick where the goal finishes");
        };
        match n.edit_change(&t.text(), &d.text(), line) {
            Ok(plans) => send(plans),
            Err(e) => a.complain(&e.to_string()),
        }
    });
    dialog.present(Some(&app.window));
    dialog.set_focus(Some(&title));
}
