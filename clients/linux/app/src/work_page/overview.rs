//! A goal's overview, as the canvas lays it out: what done means, what needs
//! you, its sub-goals with their steps, and what keeps it from done; a
//! sub-goal's page adds what the director did under it.

use std::collections::HashMap;
use std::rc::{Rc, Weak};

use adw::prelude::*;

use kit::{GoalDetail, Mark, Plan, RecordView, StepState, SubGoalRow};

use super::WorkPage;
use crate::app::{App, Place};
use crate::question;
use crate::turns::Turn;
use crate::ui::{self, SPACE_1, SPACE_2, SPACE_3};

impl WorkPage {
    pub(super) fn overview_turns(
        self: &Rc<Self>,
        g: &GoalDetail,
        record: &RecordView,
        folds: &HashMap<String, bool>,
    ) -> Vec<Turn> {
        let mut turns = Vec::new();
        let cards = (g.done_when.clone(), g.finish_card(), g.course.clone());
        turns.push(Turn::key("o-cards", &cards).with(move || cards_row(&cards.0, cards.1.as_ref(), cards.2.as_ref())));
        turns.extend(self.asking_turns(g));

        let mut rows = g.sub_goal_rows();
        for row in &mut rows {
            if let Some(open) = folds.get(&row.id) {
                row.open = *open;
            }
        }
        let head = (g.progress, g.progress_line(), g.nested());
        let (app, goal, weak) = (self.app.clone(), g.goal.id.clone(), self.me.clone());
        turns.push(Turn::key("o-subgoals", &(&rows, &head)).with(move || sub_goals(&rows, &head, &app, &goal, weak)));

        if !g.not_done_because.is_empty() {
            let because = g.not_done_because.clone();
            turns.push(Turn::key("o-because", &because).with(move || not_done_because(&because)));
        }
        if g.nested() {
            let did: Vec<(String, String)> =
                record.director_did().into_iter().map(|e| (kit::time::when(&e.at), e.what.clone())).collect();
            if !did.is_empty() {
                turns.push(Turn::key("o-did", &did).with(move || director_did(&did)));
            }
        }
        turns
    }

    /// Every question waiting on the person anywhere under the goal, answered
    /// in place, and each question a task's session asks that the goal view
    /// does not hold yet.
    pub(super) fn asking_turns(self: &Rc<Self>, g: &GoalDetail) -> Vec<Turn> {
        let mut turns = Vec::new();
        for need in &g.needs_you {
            let label =
                if need.work == g.goal.id { "Needs you".to_string() } else { format!("Needs you · in {}", need.path) };
            let (app, answered, need) = (self.app.clone(), self.reload(), need.clone());
            turns.push(
                Turn::key(format!("asking-{}", need.work), &need)
                    .with(move || question::pending(&app, &need.work, &need.question, &label, None, answered)),
            );
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
            let (app, answered) = (self.app.clone(), self.reload());
            turns.push(
                Turn::key(format!("asking-{}", task.id), &(&task, &q))
                    .with(move || question::pending(&app, &task.id, &q, &label, None, answered)),
            );
        }
        turns
    }
}

/// Done when, where it finishes (a top goal only), and its course.
fn cards_row(
    done_when: &Option<String>,
    finish: Option<&(String, String)>,
    course: Option<&kit::Course>,
) -> gtk::Widget {
    let grid = gtk::Grid::builder().column_spacing(SPACE_3).column_homogeneous(true).build();
    let card = |label: &str| {
        let c = ui::wd_card(SPACE_1);
        c.set_hexpand(true);
        c.append(&ui::label(label));
        c
    };
    let done = card("Done when");
    let text = match done_when {
        Some(d) => ui::said(d, &["callout"]),
        None => ui::text("Not set yet", &["callout", "text3"]),
    };
    done.append(&text);
    let mut cards = vec![done];
    if let Some((line, from)) = finish {
        let c = card("Finishes at");
        c.append(&ui::text(line, &["body", "strong"]));
        c.append(&ui::text(from, &["meta", "text3"]));
        cards.push(c);
    }
    let c = card("Course");
    match course {
        Some(course) => {
            let row = ui::hbox(6);
            let (text, tone) = course.badge();
            row.append(&ui::badge(&text, tone));
            let ago = kit::time::ago(&course.at, chrono::Utc::now());
            let checked = if ago == "now" { "checked just now".to_string() } else { format!("checked {ago} ago") };
            row.append(&ui::line(&checked, &["meta", "text3"]));
            c.append(&row);
            if !course.why.is_empty() {
                let why = ui::text(&course.why, &["meta", "text3"]);
                why.set_margin_top(2);
                c.append(&why);
            }
        }
        None => c.append(&ui::text("Not checked yet", &["meta", "text3"])),
    }
    cards.push(c);
    for (i, c) in cards.iter().enumerate() {
        grid.attach(c, i as i32, 0, 1, 1);
    }
    grid.upcast()
}

/// The sub-goals header, then one row each in a card.
fn sub_goals(
    rows: &[SubGoalRow],
    head: &(kit::Progress, String, bool),
    app: &App,
    goal: &str,
    page: Weak<WorkPage>,
) -> gtk::Widget {
    let column = ui::vbox(SPACE_2);
    let top = ui::hbox(SPACE_3);
    top.append(&ui::line("Sub-goals", &["title"]));
    if head.0.total > 0 && !head.2 {
        top.append(&ui::progress_bar(head.0.done, head.0.total));
    }
    if head.0.total > 0 {
        top.append(&ui::line(&head.1, &["meta", "text3"]));
    }
    let spacer = ui::hbox(0);
    spacer.set_hexpand(true);
    top.append(&spacer);
    let add = ui::button("+ Add sub-goal", false);
    let (a, goal) = (app.clone(), goal.to_string());
    add.connect_clicked(move |_| ask_sub_goal(&a, &goal));
    top.append(&add);
    column.append(&top);
    if rows.is_empty() {
        column.append(&ui::text("Not planned yet.", &["callout", "text3"]));
        return column.upcast();
    }
    let card = ui::wd_card(0);
    card.add_css_class("flush");
    for (i, row) in rows.iter().enumerate() {
        card.append(&sub_goal_row(row, i + 1 == rows.len(), app, page.clone()));
    }
    column.append(&card);
    column.upcast()
}

/// One sub-goal: its state icon, title, meta and badge; a goal's row opens
/// its page, a task's folds its steps.
fn sub_goal_row(row: &SubGoalRow, last: bool, app: &App, page: Weak<WorkPage>) -> gtk::Widget {
    let outer = ui::vbox(0);
    outer.add_css_class("sub-goal");
    if last {
        outer.add_css_class("last");
    }
    if row.mark == Mark::NeedsYou {
        outer.add_css_class("asking");
    }
    if row.struck {
        outer.add_css_class("superseded");
    }
    let top = ui::hbox(SPACE_3);
    let mark = ui::state_mark(row.state, 20);
    mark.set_margin_top(1);
    top.append(&mark);
    let words = ui::vbox(2);
    words.set_hexpand(true);
    let mut title_classes = vec!["body", "medium"];
    if row.struck {
        title_classes.push("struck");
    }
    match &row.opens {
        Some(id) => {
            let title = gtk::Button::builder().child(&ui::text(&format!("{} ›", row.title), &title_classes)).build();
            title.add_css_class("row-button");
            title.set_halign(gtk::Align::Start);
            let (app, id) = (app.clone(), id.clone());
            title.connect_clicked(move |_| app.open(Place::Work(id.clone())));
            words.append(&title);
        }
        None => words.append(&ui::text(&row.title, &title_classes)),
    }
    let meta = ui::hbox(SPACE_1);
    let foldable = row.opens.is_none() && !row.steps.is_empty();
    if !row.meta.is_empty() {
        let said = ui::text(&row.meta, &["meta", "text3"]);
        said.set_hexpand(!foldable || row.open);
        meta.append(&said);
    }
    if foldable && !row.open {
        let done = row.steps.iter().filter(|s| s.state == StepState::Done).count();
        let show = ui::inline_link(&format!("Show steps ({done} of {})", row.steps.len()));
        let (p, id) = (page.clone(), row.id.clone());
        show.connect_clicked(move |_| {
            if let Some(page) = p.upgrade() {
                page.set_fold(&id, true);
            }
        });
        meta.append(&show);
    }
    if meta.first_child().is_some() {
        words.append(&meta);
    }
    top.append(&words);
    let (text, tone) = &row.badge;
    let badge = ui::badge(text, *tone);
    badge.set_valign(gtk::Align::Start);
    badge.set_margin_top(2);
    top.append(&badge);
    outer.append(&top);
    if foldable && row.open {
        let steps = ui::vbox(2);
        steps.set_margin_start(32);
        steps.set_margin_top(10);
        for step in &row.steps {
            let line = ui::hbox(10);
            let (mark, marked, said): (&str, &[&str], &[&str]) = match step.state {
                StepState::Done => ("✓", &["step", "done-text"], &["step", "step-text"]),
                StepState::Doing => ("●", &["step", "strong", "accent-text"], &["step", "strong", "accent-text"]),
                StepState::Todo => ("○", &["step", "text3"], &["step", "text3"]),
                StepState::Dropped => ("○", &["step", "text3"], &["step", "struck"]),
            };
            line.append(&ui::line(mark, marked));
            line.append(&ui::text(&step.text, said));
            steps.append(&line);
        }
        let hide = ui::inline_link("Hide steps");
        hide.set_halign(gtk::Align::Start);
        hide.set_margin_top(SPACE_1);
        let id = row.id.clone();
        hide.connect_clicked(move |_| {
            if let Some(page) = page.upgrade() {
                page.set_fold(&id, false);
            }
        });
        steps.append(&hide);
        outer.append(&steps);
    }
    outer.upcast()
}

/// What keeps the goal from done, the first in the colour of what needs doing.
fn not_done_because(because: &[String]) -> gtk::Widget {
    let card = ui::wd_card(6);
    card.set_margin_top(0);
    card.append(&ui::label("Not done yet, because"));
    let row = gtk::FlowBox::builder()
        .selection_mode(gtk::SelectionMode::None)
        .column_spacing(24)
        .row_spacing(SPACE_1 as u32)
        .max_children_per_line(4)
        .build();
    for (i, reason) in because.iter().enumerate() {
        let item = ui::hbox(6);
        item.append(&ui::line("○", &["callout", if i == 0 { "needs-text" } else { "waiting-text" }]));
        item.append(&ui::text(&capital(reason), &["callout"]));
        row.insert(&item, -1);
    }
    card.append(&row);
    card.upcast()
}

fn capital(text: &str) -> String {
    let mut chars = text.chars();
    chars.next().map(|c| c.to_uppercase().chain(chars).collect()).unwrap_or_default()
}

/// The director's own steps under a sub-goal, newest first.
fn director_did(did: &[(String, String)]) -> gtk::Widget {
    let column = ui::vbox(0);
    let title = ui::line("What the director did here", &["title"]);
    title.set_margin_bottom(6);
    column.append(&title);
    for (when, what) in did {
        let row = ui::hbox(SPACE_3);
        row.add_css_class("director-line");
        let at = ui::line(when, &["meta", "text3"]);
        at.set_size_request(52, -1);
        at.set_valign(gtk::Align::Start);
        row.append(&at);
        let text = ui::said(what, &["callout"]);
        text.set_hexpand(true);
        row.append(&text);
        column.append(&row);
    }
    column.upcast()
}

/// Asks a new sub-goal's outcome and what makes it done, then adds it.
fn ask_sub_goal(app: &App, goal: &str) {
    let dialog = adw::AlertDialog::new(Some("Add a sub-goal"), Some("One outcome, and what makes it done."));
    dialog.add_responses(&[("cancel", "Cancel"), ("add", "Add")]);
    dialog.set_response_appearance("add", adw::ResponseAppearance::Suggested);
    dialog.set_default_response(Some("add"));
    dialog.set_close_response("cancel");
    let form = ui::vbox(SPACE_2);
    let outcome = gtk::Entry::builder().placeholder_text("The outcome").activates_default(true).build();
    let done_when = gtk::Entry::builder().placeholder_text("Done when…").activates_default(true).build();
    form.append(&outcome);
    form.append(&done_when);
    dialog.set_extra_child(Some(&form));
    let (a, goal, o, d) = (app.clone(), goal.to_string(), outcome.clone(), done_when.clone());
    dialog.connect_response(None, move |_, response| {
        if response != "add" {
            return;
        }
        match Plan::add_sub_goal(&goal, &o.text(), &d.text()) {
            Ok(plan) => a.run(plan),
            Err(e) => a.complain(&e.to_string()),
        }
    });
    dialog.present(Some(&app.window));
    // Typing goes into the outcome, not onto the default button.
    dialog.set_focus(Some(&outcome));
}
