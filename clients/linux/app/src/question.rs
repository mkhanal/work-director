//! A question laid out as it was asked: each item with its options, picked in
//! place and sent as the answer. Answered, it stays where it was asked,
//! showing what was chosen.

use std::cell::{Cell, RefCell};
use std::rc::Rc;

use adw::prelude::*;

use kit::{AnswerDraft, Plan, Question, QuestionItem};

use crate::app::App;
use crate::ui::{self, SPACE_1, SPACE_2, SPACE_3};

const RECOMMENDED: &str = " (recommended)";

/// The label an option is shown with, and whether it is the recommended one.
fn shown(label: &str) -> (&str, bool) {
    let cut = label.len().saturating_sub(RECOMMENDED.len());
    if label.is_char_boundary(cut) && label[cut..].eq_ignore_ascii_case(RECOMMENDED) {
        (label[..cut].trim_end(), true)
    } else {
        (label, false)
    }
}

/// The question waiting on the person, answered in place; `answered` runs
/// once wd has taken the answer.
pub fn pending(app: &App, work: &str, question: &Question, title: &str, answered: Rc<dyn Fn()>) -> gtk::Widget {
    let card = ui::card("attention");
    card.append(&ui::line(title, &["meta", "attention-text"]));
    let draft = Rc::new(RefCell::new(AnswerDraft::new(question.clone())));
    let pages =
        gtk::Stack::builder().vhomogeneous(false).transition_type(gtk::StackTransitionType::SlideLeftRight).build();
    let answer = gtk::Button::with_label("Answer");
    answer.add_css_class("suggested-action");
    answer.set_sensitive(false);
    let command = ui::line("", &["meta", "text3"]);
    command.set_ellipsize(gtk::pango::EllipsizeMode::Middle);
    command.set_hexpand(true);
    let failure = ui::text("", &["callout", "failure"]);
    failure.set_visible(false);

    let refresh: Rc<dyn Fn()> = {
        let (draft, answer, command, work, q) =
            (draft.clone(), answer.clone(), command.clone(), work.to_string(), question.clone());
        Rc::new(move || {
            let plan = draft.borrow().answers().and_then(|a| Plan::answer(&work, &q, &a).ok());
            answer.set_sensitive(plan.is_some());
            command.set_label(&plan.map(|p| p.command_line()).unwrap_or_default());
        })
    };

    let count = question.items.len();
    for (i, item) in question.items.iter().enumerate() {
        let page = item_page(item, i, count, &draft, &refresh);
        pages.add_named(&page, Some(&i.to_string()));
    }
    card.append(&pages);

    let footer = ui::hbox(SPACE_2);
    footer.append(&command);
    if count > 1 {
        let at = Rc::new(Cell::new(0usize));
        let back = gtk::Button::with_label("Back");
        let next = gtk::Button::with_label("Next");
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
    footer.append(&answer);
    card.append(&footer);
    card.append(&failure);

    let (app, work, q) = (app.clone(), work.to_string(), question.clone());
    answer.connect_clicked(move |button| {
        let Some(plan) = draft.borrow().answers().and_then(|a| Plan::answer(&work, &q, &a).ok()) else { return };
        button.set_sensitive(false);
        let (app, failure, button, answered) = (app.clone(), failure.clone(), button.clone(), answered.clone());
        app.clone().spawn(async move {
            match app.store.run(&plan).await {
                Ok(result) if result.code == 0 => {
                    failure.set_visible(false);
                    app.toast(&plan.command_line());
                    answered();
                }
                Ok(result) => {
                    failure.set_label(result.complaint());
                    failure.set_visible(true);
                    button.set_sensitive(true);
                }
                Err(e) => {
                    failure.set_label(&e.to_string());
                    failure.set_visible(true);
                    button.set_sensitive(true);
                }
            }
            app.store.refresh().await;
        });
    });
    card.upcast()
}

fn item_page(
    item: &QuestionItem,
    i: usize,
    count: usize,
    draft: &Rc<RefCell<AnswerDraft>>,
    refresh: &Rc<dyn Fn()>,
) -> gtk::Box {
    let page = ui::vbox(SPACE_3);
    let head = ui::hbox(SPACE_2);
    if count > 1 {
        head.append(&ui::line(&format!("{} of {count}", i + 1), &["meta", "text2"]));
    }
    if !item.header.is_empty() {
        head.append(&ui::tag(&item.header, &["meta"]));
    }
    if item.multi {
        head.append(&ui::line("pick any", &["meta", "text3"]));
    }
    if head.first_child().is_some() {
        page.append(&head);
    }
    page.append(&ui::said(&item.question, &["body", "strong"]));

    let list = gtk::ListBox::new();
    list.add_css_class("boxed-list");
    list.set_selection_mode(gtk::SelectionMode::None);
    let other =
        adw::EntryRow::builder().title(if item.multi { "Add something else…" } else { "Something else…" }).build();
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
        let row = adw::ActionRow::builder().title(label).use_markup(false).activatable(true).build();
        if !option.description.is_empty() {
            row.set_subtitle(&option.description);
        }
        row.add_prefix(&check);
        row.set_activatable_widget(Some(&check));
        if recommended {
            let tag = ui::tag("Recommended", &["meta", "settled-text"]);
            tag.set_valign(gtk::Align::Center);
            row.add_suffix(&tag);
        }
        let (draft, refresh, label, multi, other) =
            (draft.clone(), refresh.clone(), option.label.clone(), item.multi, other.clone());
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
            refresh();
        });
        list.append(&row);
    }
    let (d, r) = (draft.clone(), refresh.clone());
    other.connect_changed(move |entry| {
        d.borrow_mut().set_typed(i, &entry.text());
        r();
    });
    list.append(&other);
    page.append(&list);
    page
}

/// A question already answered, or asked of a session and answered there:
/// its items with what was chosen, and any reply instead.
pub fn answered(question: &Question) -> gtk::Widget {
    let card = ui::card("raised");
    card.append(&ui::line("Asked", &["meta", "text2"]));
    for (i, item) in question.items.iter().enumerate() {
        let answer = question.answers.as_ref().and_then(|a| a.get(i));
        let parts: Vec<&str> = answer.map(|a| a.split(", ").collect()).unwrap_or_default();
        let column = ui::vbox(SPACE_1);
        column.append(&ui::said(&item.question, &["body", "strong"]));
        for option in &item.options {
            let chosen = parts.contains(&option.label.as_str());
            let row = ui::hbox(SPACE_2);
            let mark = if chosen { "object-select-symbolic" } else { "radio-symbolic" };
            row.append(&ui::icon(mark, &[if chosen { "accent-text" } else { "text3" }]));
            row.append(&ui::text(shown(&option.label).0, &["callout", if chosen { "strong" } else { "text2" }]));
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
