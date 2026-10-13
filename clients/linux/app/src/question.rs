//! A question laid out as the canvas's question card: each item with its
//! options, picked in place and sent as the answer, or left to the
//! director. Answered, it stays where it was asked, showing what was chosen.

use std::cell::{Cell, RefCell};
use std::rc::Rc;

use adw::prelude::*;

use kit::{AnswerDraft, Plan, Question, QuestionItem, Tone};

use crate::app::App;
use crate::ui::{self, SPACE_1, SPACE_2};

/// The label an option is shown with, and whether it is the recommended one.
fn shown(label: &str) -> (&str, bool) {
    let bare = kit::unmarked(label);
    (bare, bare.len() != label.len())
}

/// The question waiting on the person, answered in place; `label` heads the
/// card, `extra` sits between the question and its buttons, and `answered`
/// runs once wd has taken the answer.
pub fn pending(
    app: &App,
    work: &str,
    question: &Question,
    label: &str,
    extra: Option<gtk::Widget>,
    answered: Rc<dyn Fn()>,
) -> gtk::Widget {
    let card = ui::vbox(SPACE_2);
    card.add_css_class("question");
    let head = ui::label(label);
    head.add_css_class("attention-text");
    card.append(&head);
    let draft = Rc::new(RefCell::new(AnswerDraft::new(question.clone())));
    let pages =
        gtk::Stack::builder().vhomogeneous(false).transition_type(gtk::StackTransitionType::SlideLeftRight).build();
    let answer = ui::button("Answer", true);
    answer.set_sensitive(false);
    let decide = ui::button("Let the director decide", false);
    let failure = ui::text("", &["callout", "failure"]);
    failure.set_visible(false);

    let refresh: Rc<dyn Fn()> = {
        let (draft, answer, work, q) = (draft.clone(), answer.clone(), work.to_string(), question.clone());
        Rc::new(move || {
            let plan = draft.borrow().answers().and_then(|a| Plan::answer(&work, &q, &a).ok());
            answer.set_sensitive(plan.is_some());
            answer.set_tooltip_text(plan.map(|p| p.command_line()).as_deref());
        })
    };

    let count = question.items.len();
    for (i, item) in question.items.iter().enumerate() {
        let page = item_page(item, i, count, &draft, &refresh);
        pages.add_named(&page, Some(&i.to_string()));
    }
    card.append(&pages);
    if let Some(extra) = extra {
        card.append(&extra);
    }

    let footer = ui::hbox(SPACE_2);
    footer.set_margin_top(6);
    let spacer = ui::hbox(0);
    spacer.set_hexpand(true);
    footer.append(&spacer);
    if count > 1 {
        let at = Rc::new(Cell::new(0usize));
        let back = ui::button("Back", false);
        let next = ui::button("Next", false);
        back.set_sensitive(false);
        let step: Rc<dyn Fn(isize)> = {
            let (at, pages, back, next) = (at.clone(), pages.clone(), back.clone(), next.clone());
            Rc::new(move |by| {
                let now = (at.get() as isize + by).clamp(0, count as isize - 1) as usize;
                at.set(now);
                pages.set_visible_child_name(&now.to_string());
                back.set_sensitive(now > 0);
                next.set_sensitive(now + 1 < count);
            })
        };
        let s = step.clone();
        back.connect_clicked(move |_| s(-1));
        next.connect_clicked(move |_| step(1));
        footer.append(&back);
        footer.append(&next);
    }
    footer.append(&decide);
    footer.append(&answer);
    card.append(&footer);
    card.append(&failure);

    let send = {
        let (app, failure, answered) = (app.clone(), failure.clone(), answered.clone());
        Rc::new(move |plan: Plan, buttons: [gtk::Button; 2]| {
            for b in &buttons {
                b.set_sensitive(false);
            }
            let (app, failure, answered) = (app.clone(), failure.clone(), answered.clone());
            app.clone().spawn(async move {
                let refused = match app.store.run(&plan).await {
                    Ok(result) if result.code == 0 => None,
                    Ok(result) => Some(result.complaint().to_string()),
                    Err(e) => Some(e.to_string()),
                };
                match refused {
                    None => {
                        failure.set_visible(false);
                        app.toast(&plan.command_line());
                        answered();
                    }
                    Some(why) => {
                        failure.set_label(&why);
                        failure.set_visible(true);
                        for b in &buttons {
                            b.set_sensitive(true);
                        }
                    }
                }
                app.store.refresh().await;
            });
        })
    };
    let (s, work_id, q, pair) = (send.clone(), work.to_string(), question.clone(), [answer.clone(), decide.clone()]);
    answer.connect_clicked(move |_| {
        if let Some(plan) = draft.borrow().answers().and_then(|a| Plan::answer(&work_id, &q, &a).ok()) {
            s(plan, pair.clone());
        }
    });
    let (work_id, q, pair) = (work.to_string(), question.clone(), [answer.clone(), decide.clone()]);
    match Plan::director_decides(&work_id, &q) {
        Ok(plan) => {
            decide.set_tooltip_text(Some(&plan.command_line()));
            decide.connect_clicked(move |_| send(plan.clone(), pair.clone()));
        }
        Err(_) => decide.set_visible(false),
    }
    card.upcast()
}

fn item_page(
    item: &QuestionItem,
    i: usize,
    count: usize,
    draft: &Rc<RefCell<AnswerDraft>>,
    refresh: &Rc<dyn Fn()>,
) -> gtk::Box {
    let page = ui::vbox(0);
    let head = ui::hbox(SPACE_2);
    if count > 1 {
        head.append(&ui::line(&format!("{} of {count}", i + 1), &["meta", "text3"]));
    }
    if !item.header.is_empty() {
        head.append(&ui::sgtag(&item.header));
    }
    if item.multi {
        head.append(&ui::line("pick any", &["meta", "text3"]));
    }
    if head.first_child().is_some() {
        head.set_margin_bottom(SPACE_1);
        page.append(&head);
    }
    let asked = ui::said(&item.question, &["body", "medium"]);
    asked.set_margin_top(6);
    asked.set_margin_bottom(SPACE_2);
    page.append(&asked);

    let rows: Rc<RefCell<Vec<(gtk::Box, String)>>> = Rc::new(RefCell::new(Vec::new()));
    let mark_chosen = {
        let (rows, draft) = (rows.clone(), draft.clone());
        Rc::new(move || {
            for (row, label) in rows.borrow().iter() {
                if draft.borrow().is_picked(label, i) {
                    row.add_css_class("chosen");
                } else {
                    row.remove_css_class("chosen");
                }
            }
        })
    };
    let other = gtk::Entry::builder()
        .placeholder_text(if item.multi { "Add something else…" } else { "Something else…" })
        .hexpand(true)
        .has_frame(false)
        .build();
    other.add_css_class("callout");
    let mut group: Option<gtk::CheckButton> = None;
    for option in &item.options {
        let check = gtk::CheckButton::new();
        if !item.multi {
            if let Some(first) = &group {
                check.set_group(Some(first));
            } else {
                group = Some(check.clone());
            }
        }
        let (label, recommended) = shown(&option.label);
        let row = ui::hbox(SPACE_2);
        row.add_css_class("option");
        row.append(&check);
        let words = ui::vbox(2);
        words.set_hexpand(true);
        words.append(&ui::text(label, &["callout", "medium"]));
        if !option.description.is_empty() {
            words.append(&ui::text(&option.description, &["meta", "text3"]));
        }
        row.append(&words);
        if recommended {
            row.append(&ui::badge("Recommended", Tone::Settled));
        }
        let click = gtk::GestureClick::new();
        let (c, multi) = (check.clone(), item.multi);
        click.connect_released(move |_, _, _, _| c.set_active(!multi || !c.is_active()));
        words.add_controller(click);
        rows.borrow_mut().push((row.clone(), option.label.clone()));
        let (draft, refresh, label, multi, other, mark) =
            (draft.clone(), refresh.clone(), option.label.clone(), item.multi, other.clone(), mark_chosen.clone());
        check.connect_toggled(move |check| {
            let picked = draft.borrow().is_picked(&label, i);
            if multi {
                if check.is_active() != picked {
                    draft.borrow_mut().toggle(&label, i);
                }
            } else if check.is_active() && !picked {
                draft.borrow_mut().toggle(&label, i);
                other.set_text("");
            }
            mark();
            refresh();
        });
        page.append(&row);
    }
    let row = ui::hbox(SPACE_2);
    row.add_css_class("option");
    let mark = gtk::Label::new(Some("○"));
    mark.add_css_class("text3");
    row.append(&mark);
    row.append(&other);
    page.append(&row);
    let (d, r) = (draft.clone(), refresh.clone());
    other.connect_changed(move |entry| {
        d.borrow_mut().set_typed(i, &entry.text());
        r();
    });
    page
}

/// A question already answered, or asked of a session and answered there:
/// its items with what was chosen, and any reply instead.
pub fn answered(question: &Question) -> gtk::Widget {
    let card = ui::wd_card(SPACE_2);
    card.append(&ui::label("Asked"));
    for (i, item) in question.items.iter().enumerate() {
        let answer = question.answers.as_ref().and_then(|a| a.get(i));
        let parts: Vec<&str> = answer.map(|a| a.split(", ").collect()).unwrap_or_default();
        let column = ui::vbox(SPACE_1);
        column.append(&ui::said(&item.question, &["callout", "medium"]));
        for option in &item.options {
            let chosen = parts.contains(&option.label.as_str());
            let row = ui::hbox(SPACE_2);
            row.append(&ui::line(if chosen { "◉" } else { "○" }, &[if chosen { "accent-text" } else { "text3" }]));
            row.append(&ui::text(shown(&option.label).0, &["callout", if chosen { "medium" } else { "text2" }]));
            column.append(&row);
        }
        if let Some(answer) = answer.filter(|a| !item.options.iter().any(|o| a.split(", ").any(|p| p == o.label))) {
            column.append(&ui::said(answer, &["callout"]));
        }
        card.append(&column);
    }
    if let Some(reply) = &question.reply {
        card.append(&ui::said(reply, &["callout", "text2"]));
    }
    card.upcast()
}
