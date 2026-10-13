//! A question laid out as the canvas's question card: each item with its
//! options, picked in place and sent as the answer, or left to the
//! director. Answered, it stays where it was asked, showing what was chosen.

use std::cell::{Cell, RefCell};
use std::rc::Rc;

use adw::prelude::*;

use kit::{AnswerDraft, DirectorAnswer, Plan, Question, QuestionItem, Tone};

use crate::app::App;
use crate::ui::{self, SPACE_1, SPACE_2};

/// The label an option is shown with, and whether it is the recommended one.
fn shown(label: &str) -> (&str, bool) {
    let bare = kit::unmarked(label);
    (bare, bare.len() != label.len())
}

/// What a question card carries beyond the question: the line heading it,
/// what sits between the first question and its options, what sits under the
/// options, the from-now-on box of a finish-line question, and what the
/// director last said of it when it declined.
#[derive(Default)]
pub struct Card {
    pub label: String,
    pub extra: Option<gtk::Widget>,
    pub after: Option<gtk::Widget>,
    pub remember: Option<String>,
    pub declined: Option<DirectorAnswer>,
}

impl Card {
    pub fn headed(label: &str) -> Card {
        Card { label: label.to_string(), ..Card::default() }
    }
}

/// The question waiting on the person, answered in place. `answered` runs
/// once wd has taken the answer; `director` gets whatever the director said
/// when left to decide, answered or declined.
pub fn pending(
    app: &App,
    work: &str,
    question: &Question,
    card_says: Card,
    answered: Rc<dyn Fn()>,
    director: Rc<dyn Fn(DirectorAnswer)>,
) -> gtk::Widget {
    let card = ui::vbox(SPACE_2);
    card.add_css_class("question");
    let head = ui::label(&card_says.label);
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
    let remember =
        card_says.remember.as_deref().map(|said| ui::choice(&ui::line(said, &["callout"]), said, false, None));

    let answer_plan: Rc<dyn Fn() -> Option<Plan>> = {
        let (draft, work, q, remember) = (draft.clone(), work.to_string(), question.clone(), remember.clone());
        Rc::new(move || {
            let plan = draft.borrow().answers().and_then(|a| Plan::answer(&work, &q, &a).ok())?;
            Some(if remember.as_ref().is_some_and(|r| r.is_active()) { plan.remembering() } else { plan })
        })
    };
    let refresh: Rc<dyn Fn()> = {
        let (answer, answer_plan) = (answer.clone(), answer_plan.clone());
        Rc::new(move || {
            let plan = answer_plan();
            answer.set_sensitive(plan.is_some());
            answer.set_tooltip_text(plan.map(|p| p.command_line()).as_deref());
        })
    };

    let count = question.items.len();
    let mut extra = card_says.extra;
    for (i, item) in question.items.iter().enumerate() {
        let page = item_page(item, i, count, extra.take().as_ref(), &draft, &refresh);
        pages.add_named(&page, Some(&i.to_string()));
    }
    card.append(&pages);
    if let Some(after) = &card_says.after {
        card.append(after);
    }
    if let Some(declined) = &card_says.declined {
        card.append(&declined_note(declined));
    }

    let footer = ui::hbox(SPACE_2);
    footer.set_margin_top(6);
    match &remember {
        Some(check) => {
            check.add_css_class("callout");
            check.set_hexpand(true);
            check.set_halign(gtk::Align::Start);
            let r = refresh.clone();
            check.connect_toggled(move |_| r());
            footer.append(check);
        }
        None => {
            let spacer = ui::hbox(0);
            spacer.set_hexpand(true);
            footer.append(&spacer);
        }
    }
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
            let (app, failure, answered, director) = (app.clone(), failure.clone(), answered.clone(), director.clone());
            app.clone().spawn(async move {
                let leaves_it = plan.argv.iter().any(|w| w == "--director");
                let outcome = match app.store.run(&plan).await {
                    Ok(result) if result.code != 0 => Err(result.complaint().to_string()),
                    Ok(result) if leaves_it => {
                        DirectorAnswer::read(&result.stdout).map(Some).map_err(|e| e.to_string())
                    }
                    Ok(_) => Ok(None),
                    Err(e) => Err(e.to_string()),
                };
                match outcome {
                    Ok(said) => {
                        failure.set_visible(false);
                        match said {
                            Some(said) if !said.answered => {
                                app.toast("The director left it to you");
                                for b in &buttons {
                                    b.set_sensitive(true);
                                }
                                director(said);
                            }
                            Some(said) => {
                                app.toast("The director decided it");
                                director(said);
                                answered();
                            }
                            None => {
                                app.toast(&plan.command_line());
                                answered();
                            }
                        }
                    }
                    Err(why) => {
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
    let (s, pair) = (send.clone(), [answer.clone(), decide.clone()]);
    answer.connect_clicked(move |_| {
        if let Some(plan) = answer_plan() {
            s(plan, pair.clone());
        }
    });
    let pair = [answer.clone(), decide.clone()];
    let plan = Plan::director_decides(work);
    decide.set_tooltip_text(Some(&plan.command_line()));
    decide.connect_clicked(move |_| send(plan.clone(), pair.clone()));
    card.upcast()
}

/// Why the director would not decide: each item it declined, with why. The
/// question stays open.
fn declined_note(said: &DirectorAnswer) -> gtk::Widget {
    let note = ui::wd_card(SPACE_1);
    note.add_css_class("declined");
    note.append(&ui::label("The director left this to you"));
    for item in &said.declined {
        note.append(&ui::said(&item.question, &["callout", "medium"]));
        note.append(&ui::said(&kit::sentence(&item.why), &["meta", "text2"]));
    }
    note.upcast()
}

/// What the director decided when left to: each item with its answer and why.
pub fn director_decided(said: &DirectorAnswer) -> gtk::Widget {
    let card = ui::card("settled");
    card.append(&ui::meta(&ui::speaker("Director decided"), None, None));
    for item in &said.decided {
        let column = ui::vbox(2);
        column.append(&ui::said(&item.question, &["callout", "text2"]));
        column.append(&ui::said(item.answer.as_deref().unwrap_or(""), &["body", "medium"]));
        if !item.why.is_empty() {
            column.append(&ui::said(&format!("Why: {}", item.why), &["meta", "text3"]));
        }
        card.append(&column);
    }
    ui::turn(Some(&ui::avatar("emblem-ok-symbolic", &["settled-text"])), None, &card).upcast()
}

fn item_page(
    item: &QuestionItem,
    i: usize,
    count: usize,
    extra: Option<&gtk::Widget>,
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
    if let Some(extra) = extra {
        extra.set_margin_bottom(SPACE_2);
        page.append(extra);
    }

    let rows: Rc<RefCell<Vec<(gtk::ToggleButton, String)>>> = Rc::new(RefCell::new(Vec::new()));
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
    other.update_property(&[gtk::accessible::Property::Label("Something else")]);
    let mut group: Option<gtk::ToggleButton> = None;
    for option in &item.options {
        let recommended = shown(&option.label).1;
        let words = ui::hbox(SPACE_2);
        let said = ui::vbox(2);
        said.set_hexpand(true);
        said.append(&ui::text(option.shown(), &["callout", "medium"]));
        if !option.description.is_empty() {
            said.append(&ui::text(&option.description, &["meta", "text3"]));
        }
        words.append(&said);
        if recommended {
            let badge = ui::badge("Recommended", Tone::Settled);
            badge.set_valign(gtk::Align::Start);
            words.append(&badge);
        }
        words.set_hexpand(true);
        let row = ui::choice(&words, option.shown(), !item.multi, group.as_ref());
        if !option.description.is_empty() {
            row.update_property(&[gtk::accessible::Property::Description(&option.description)]);
        }
        row.add_css_class("option");
        if !item.multi && group.is_none() {
            group = Some(row.clone());
        }
        rows.borrow_mut().push((row.clone(), option.label.clone()));
        let (draft, refresh, label, multi, other, mark) =
            (draft.clone(), refresh.clone(), option.label.clone(), item.multi, other.clone(), mark_chosen.clone());
        row.connect_toggled(move |check| {
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
