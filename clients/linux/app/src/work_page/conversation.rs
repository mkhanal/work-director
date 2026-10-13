//! A goal read as one conversation, and a session as it happens: each turn
//! tagged with the sub-goal it came from, narrowed by who spoke or by sub-goal.

use std::rc::{Rc, Weak};

use adw::prelude::*;

use kit::{
    Event, Feed, FilterChip, GoalDetail, Mark, Outgoing, OutgoingState, Phase, Report, SessionStatus, Step, StepKind,
    ThreadFilter, ThreadItem, ThreadRow, ThreadStep, ToolCall, Work, WorkState, WorkUnderWay, WorkingRow,
};

use super::{Loaded, WorkPage, MERGE_SECONDS};
use crate::agents::phase_mark;
use crate::app::{App, Place};
use crate::question;
use crate::turns::Turn;
use crate::ui::{self, SPACE_1, SPACE_2, SPACE_3};
use crate::working;

impl WorkPage {
    /// A goal as one conversation: what was asked, the plan, what its
    /// supervisor and agents did, and what waits on you, last.
    /// Also where each of your messages is, to jump back to.
    pub(super) fn goal_turns(
        self: &Rc<Self>,
        g: &GoalDetail,
        filter: ThreadFilter,
    ) -> (Vec<Turn>, Vec<(String, String)>) {
        let mut turns = Vec::new();
        let mut yours = Vec::new();
        self.render_filters(g.filters(), &filter);
        if filter == ThreadFilter::All {
            if !g.goal.detail.is_empty() {
                let (detail, at) = (g.goal.detail.clone(), g.goal.created.clone());
                turns.push(
                    Turn::key("asked", &detail)
                        .copying(&detail)
                        .with(move || ui::you_bubble(&detail, "You asked", Some(at.as_str()), None).upcast()),
                );
            }
            let plan: Vec<(String, String, WorkState, Option<String>)> = g
                .tree
                .iter()
                .map(|n| (n.work.id.clone(), n.work.title.clone(), n.work.state, n.opens().map(str::to_string)))
                .collect();
            let (weak, app) = (self.me.clone(), self.app.clone());
            turns.push(Turn::key("plan", &plan).with(move || plan_block(&plan, &app, weak)));
        }

        let mut day = String::new();
        for item in g.items(filter.clone()) {
            let at = match &item {
                ThreadItem::Step(step) => step.event().at.clone(),
                ThreadItem::Updates(run) => run.first().map(|s| s.event().at.clone()).unwrap_or_default(),
            };
            let today = kit::time::day(&at);
            if today != day {
                let label = today.clone();
                turns.push(Turn::key(format!("day-{}", item.id()), &label).with(move || ui::day_line(&label).upcast()));
                day = today;
            }
            match item {
                ThreadItem::Step(step) => {
                    if let Some(turn) = self.step_turn(g, *step, &mut yours) {
                        turns.push(turn);
                    }
                }
                ThreadItem::Updates(run) => {
                    let id = format!("u{}", run.first().map_or(0, ThreadStep::id));
                    let inner: Vec<Turn> =
                        run.iter().filter_map(|s| self.step_turn(g, s.clone(), &mut yours)).collect();
                    let count = run.len();
                    turns.push(Turn::key(&id, &run).with(move || updates_row(count, &at, inner)));
                }
            }
        }

        let outgoing: Vec<Outgoing> = self
            .app
            .store
            .outbox()
            .iter()
            .filter(|m| (m.work == g.goal.id || g.node(&m.work).is_some()) && !m.arrived_in_goal(g))
            .cloned()
            .collect();
        for m in outgoing {
            let app = self.app.clone();
            turns.push(
                Turn::key(format!("out{}", m.id), &m.state)
                    .copying(&m.text)
                    .with(move || outgoing_bubble(&app, &m, "sent")),
            );
        }

        turns.extend(self.asking_turns(g));
        let under_way = g.work_under_way(&self.app.store.in_flight());
        if !under_way.rows.is_empty() {
            turns.push(Turn::key("under-way", &under_way).with(move || working::rows(&under_way)));
        }
        (turns, yours)
    }

    /// One step of a goal's conversation as a turn; none for a question that
    /// waits on the person, which is answered in place below.
    pub(super) fn step_turn(
        self: &Rc<Self>,
        g: &GoalDetail,
        step: ThreadStep,
        yours: &mut Vec<(String, String)>,
    ) -> Option<Turn> {
        let id = format!("e{}", step.id());
        let supervised = g.supervised();
        Some(match step {
            ThreadStep::Event(e) if g.is_yours(&e) => {
                let said = e.decision.as_ref().map(|d| d.answer.clone()).unwrap_or_else(|| e.body.clone());
                yours.push((said.clone(), id.clone()));
                let label = if e.kind == "answer" { "You answered" } else { "You" };
                Turn::key(&id, &e)
                    .copying(&said)
                    .with(move || ui::you_bubble(&said, label, Some(e.at.as_str()), None).upcast())
            }
            ThreadStep::Event(e) if g.finished_line(&e).is_some() => {
                let text = format!("{} · {}", g.finished_line(&e).unwrap_or_default(), kit::time::when(&e.at));
                Turn::key(&id, &text).with(move || ui::event_line(&text, &[]).upcast())
            }
            ThreadStep::Event(e) => {
                let task = self.named(g, &e.work);
                let report = g.reports.get(&e.id.to_string()).cloned();
                let weak = self.me.clone();
                Turn::key(&id, &(&e, &report))
                    .copying(&e.answer_text())
                    .with(move || event_turn(&e, report.as_ref(), task, weak))
            }
            ThreadStep::Exchange(q, answer) => {
                // An open question is answered in its needs-you card, shown once.
                let open = g.asking.as_ref().is_some_and(|a| a.id == format!("event-{}", q.id))
                    || g.needs_you.iter().any(|n| n.work == q.work);
                if answer.is_none() && open {
                    return None;
                }
                if let Some(a) = answer.as_ref().filter(|a| g.is_yours(a)) {
                    yours.push((a.answer_text(), id.clone()));
                }
                let asker = if q.work == g.goal.id {
                    (if supervised { "Supervisor" } else { "Plan" }).to_string()
                } else {
                    g.tag(&q.work).or(g.title_of(&q.work)).unwrap_or(&q.work).to_string()
                };
                let weak = self.me.clone();
                let target = (q.work != g.goal.id).then(|| q.work.clone());
                let decided = answer.as_ref().map(|a| g.decided_for(a)).unwrap_or_default();
                let said = [Some(q.body.clone()), answer.as_ref().map(|a| a.answer_text())]
                    .into_iter()
                    .flatten()
                    .collect::<Vec<_>>()
                    .join("\n\n");
                Turn::key(&id, &(&q, &answer, &decided))
                    .copying(&said)
                    .with(move || exchange_turn(&q, answer.as_ref(), &decided, &asker, target, weak))
            }
            ThreadStep::Look(e) => {
                let text = format!("Supervisor looking · {} · {}", e.body, kit::time::when(&e.at));
                Turn::key(&id, &text).with(move || ui::event_line(&text, &[]).upcast())
            }
            ThreadStep::Supervisor(e, reply) => {
                let tasks = self.told(g, &reply);
                let (weak, goal) = (self.me.clone(), g.goal.id.clone());
                Turn::key(&id, &(&e, &reply, &tasks))
                    .copying(&e.body)
                    .with(move || supervisor_turn(&e, &reply, &tasks, &goal, weak))
            }
            ThreadStep::Declined(e, declined) => Turn::key(&id, &(&e, &declined))
                .copying(&format!("{}\n\n{}", declined.question, declined.why))
                .with(move || declined_turn(&e, &declined)),
            ThreadStep::Stopped(e, said) => {
                let task = self.named(g, &e.work);
                let weak = self.me.clone();
                Turn::key(&id, &(&e, &said)).copying(&said).with(move || stopped_turn(&e, &said, task, weak))
            }
        })
    }

    /// The conversation's chips in the header, rebuilt only when they change.
    fn render_filters(self: &Rc<Self>, chips: Vec<FilterChip>, chosen: &ThreadFilter) {
        let key = (chips, chosen.clone());
        if self.shown_filters.borrow().as_ref() == Some(&key) {
            return;
        }
        let row = &self.header.filters;
        ui::clear(row);
        let (flow, chips) = ui::chip_row();
        let mut first: Option<gtk::ToggleButton> = None;
        for chip in &key.0 {
            let label = match chip.filter {
                ThreadFilter::SubGoal(_) if chip.count > 0 => format!("{} · {}", chip.label, chip.count),
                _ => chip.label.clone(),
            };
            let button = ui::fchip(&label, chip.filter == key.1, first.as_ref());
            if first.is_none() {
                first = Some(button.clone());
            }
            let (weak, filter) = (self.me.clone(), chip.filter.clone());
            button.connect_toggled(move |b| {
                if b.is_active() {
                    if let Some(page) = weak.upgrade() {
                        page.set_filter(filter.clone());
                    }
                }
            });
            chips.append(&button);
        }
        row.append(&flow);
        *self.shown_filters.borrow_mut() = Some(key);
    }

    /// Each task a supervisor's action told, started or filed, where it is and what it is doing.
    pub(super) fn told(
        &self,
        g: &GoalDetail,
        reply: &kit::SupervisorReply,
    ) -> Vec<(Option<Work>, Phase, Option<String>)> {
        reply
            .actions
            .iter()
            .map(|a| {
                let task = a.task.as_ref().and_then(|t| g.tasks.iter().find(|w| &w.id == t)).cloned();
                let activity = task.as_ref().and_then(|t| g.activity.get(&t.id));
                let now = activity.and_then(|a| a.error.clone().or_else(|| a.now.clone()));
                (task, Phase::of(activity), now)
            })
            .collect()
    }

    /// The name of work on a goal: the task's title, or none for the goal itself.
    pub(super) fn named(&self, g: &GoalDetail, id: &str) -> Option<(String, String, WorkState)> {
        g.tasks.iter().find(|t| t.id == id).map(|t| (t.id.clone(), g.tag(id).unwrap_or(&t.title).to_string(), t.state))
    }

    /// A session as it happens: what it was told, said and thought, its tools
    /// grouped, and the questions it waits on, answered in place.
    pub(super) fn session_turns(self: &Rc<Self>, work: &Work, feed: &Rc<Feed>, director: &[String]) -> Vec<Turn> {
        let f = feed.state();
        let mut turns = Vec::new();
        if let Some(why) = &f.failure {
            let why = why.clone();
            turns.push(Turn::key("feed-failure", &why).with(move || ui::failure(&why).upcast()));
        }
        if !f.loaded {
            turns.push(Turn::key("feed-loading", &()).with(|| ui::waiting("Reading the conversation…").upcast()));
        } else if f.steps.is_empty() {
            let text = if f.status == SessionStatus::NoSession {
                "No session yet: the conversation starts when the work is spawned."
            } else {
                "The session has said nothing yet."
            };
            turns.push(Turn::key("feed-empty", &text).with(move || ui::text(text, &["callout", "text2"]).upcast()));
        }
        let running = f.status == SessionStatus::Running;
        let last = f.steps.len().saturating_sub(1);
        // The agent's turns run together until someone else speaks or two minutes pass.
        let mut agent_since: Option<String> = None;
        for row in ThreadRow::rows(&f.steps) {
            let index = row.index();
            let id = format!("s{index}");
            let (step_at, agent) = match &row {
                ThreadRow::Step { step, .. } => {
                    (step.at.clone(), !matches!(step.kind, StepKind::Prompt | StepKind::Question))
                }
                ThreadRow::Tools { .. } => (f.steps.get(index).and_then(|s| s.at.clone()), true),
            };
            let continues = agent
                && agent_since.as_ref().is_some_and(|since| {
                    step_at.as_deref().and_then(|at| kit::time::between(since, at)).is_none_or(|s| s < MERGE_SECONDS)
                });
            if agent {
                if !continues {
                    agent_since = Some(step_at.clone().unwrap_or_default());
                }
            } else {
                agent_since = None;
            }
            let head = (!continues).then(|| (work.title.clone(), work.state, step_at.clone()));
            match row {
                ThreadRow::Step { index, step } => match step.kind {
                    StepKind::Prompt => {
                        let text = step.text().to_string();
                        if director.iter().any(|d| text.contains(d.as_str())) {
                            let at = step.at.clone();
                            turns.push(
                                Turn::key(&id, &step)
                                    .copying(&text)
                                    .with(move || director_answer(&text, at.as_deref(), None)),
                            );
                        } else {
                            let label = if index == 0 { "Brief" } else { "Told" };
                            let at = step.at.clone();
                            turns.push(
                                Turn::key(&id, &step)
                                    .copying(&text)
                                    .with(move || ui::you_bubble(&text, label, at.as_deref(), None).upcast()),
                            );
                        }
                    }
                    StepKind::Question => {
                        let Some(q) = step.question.clone() else { continue };
                        if q.pending() {
                            let (app, work_id, answered) = (self.app.clone(), work.id.clone(), self.reload_feed(feed));
                            let (heard, declined) = (
                                self.heard(&work.id),
                                self.director_said.borrow().get(&work.id).filter(|s| !s.answered).cloned(),
                            );
                            turns.push(Turn::key(&id, &(&q, &declined)).with(move || {
                                let card = question::Card { declined, ..question::Card::headed("Needs you") };
                                question::pending(&app, &work_id, &q, card, answered, heard)
                            }));
                        } else {
                            turns.push(Turn::key(&id, &q).with(move || question::answered(&q)));
                        }
                    }
                    _ => {
                        let open = index == last && running;
                        turns.push(
                            Turn::key(&id, &(&step, open, &head))
                                .copying(&step_said(&step))
                                .with(move || agent_turn(head, agent_body(&step, open))),
                        );
                    }
                },
                ThreadRow::Tools { calls, .. } => {
                    let working = running && index + calls.len() > last;
                    turns.push(
                        Turn::key(&id, &(&calls, working, &head))
                            .copying(&calls.iter().map(tool_said).collect::<Vec<_>>().join("\n\n"))
                            .with(move || agent_turn(head, tool_group(&calls))),
                    );
                }
            }
        }
        drop(f);
        if feed.agent.is_none() {
            let steps = feed.state().steps.clone();
            let outgoing: Vec<Outgoing> = self
                .app
                .store
                .outbox()
                .iter()
                .filter(|m| m.work == work.id && !m.arrived_in_steps(&steps))
                .cloned()
                .collect();
            let waiting = if running { "queued: read once it is ready" } else { "sent" };
            for m in outgoing {
                let app = self.app.clone();
                turns.push(
                    Turn::key(format!("out{}", m.id), &(&m.state, waiting))
                        .copying(&m.text)
                        .with(move || outgoing_bubble(&app, &m, waiting)),
                );
            }
        }
        let under_way = self.session_under_way(work, feed);
        if !under_way.rows.is_empty() {
            turns.push(Turn::key("under-way", &under_way).with(move || working::rows(&under_way)));
        }
        turns
    }

    /// A session's own work under way: what it is doing while it works,
    /// and each write on it the app started and waits on.
    fn session_under_way(&self, work: &Work, feed: &Feed) -> WorkUnderWay {
        let f = feed.state();
        let session = (f.status == SessionStatus::Running).then(|| {
            let last = f.steps.last();
            WorkingRow {
                key: work.id.clone(),
                who: work.title.clone(),
                doing: kit::doing(last.map(|s| s.kind), last.map(Step::line).as_deref()),
                since: last.and_then(|s| s.at.as_deref()).and_then(kit::time::instant),
            }
        });
        let writes = self
            .app
            .store
            .in_flight()
            .iter()
            .filter(|w| w.work == work.id)
            .map(|w| WorkingRow {
                key: format!("write-{}", w.id),
                who: if matches!(w.write, kit::Write::Send | kit::Write::Answer) {
                    work.title.clone()
                } else {
                    "Director".to_string()
                },
                doing: w.write.doing().to_string(),
                since: Some(w.at),
            })
            .collect::<Vec<_>>();
        let mut rows: Vec<WorkingRow> = writes.into_iter().chain(session).collect();
        let more = rows.len().saturating_sub(kit::working::SHOWN);
        rows.truncate(kit::working::SHOWN);
        WorkUnderWay { rows, more }
    }

    pub(super) fn reload_feed(&self, feed: &Rc<Feed>) -> Rc<dyn Fn()> {
        let (weak, feed) = (self.me.clone(), Rc::downgrade(feed));
        Rc::new(move || {
            let (Some(page), Some(feed)) = (weak.upgrade(), feed.upgrade()) else { return };
            let p = Rc::clone(&page);
            page.app.spawn(async move {
                feed.read().await;
                p.render();
            });
        })
    }
}

/// What the director answered work in the person's place.
pub(super) fn director_answers(loaded: &Loaded, id: &str) -> Vec<String> {
    let events: &[Event] = match loaded {
        Loaded::Goal(g, _) => &g.events,
        Loaded::Work(_, e) => e,
    };
    events.iter().filter(|e| e.work == id && e.answered_by_director()).map(|e| e.body.clone()).collect()
}

/// The director's plan: each sub-goal with where it stands, a way into it.
pub(super) fn plan_block(
    plan: &[(String, String, WorkState, Option<String>)],
    app: &App,
    page: Weak<WorkPage>,
) -> gtk::Widget {
    if plan.is_empty() {
        return ui::text("Not planned yet.", &["callout", "text3"]).upcast();
    }
    let card = ui::wd_card(6);
    for (id, title, state, opens) in plan {
        let row = ui::hbox(SPACE_2);
        row.append(&ui::state_badge(*state));
        row.append(&ui::text(title, &["callout"]));
        let button = gtk::Button::builder().child(&row).build();
        button.add_css_class("row-button");
        let (app, page, id, opens) = (app.clone(), page.clone(), id.clone(), opens.clone());
        button.connect_clicked(move |_| match &opens {
            Some(goal) => app.open(Place::Work(goal.clone())),
            None => {
                if let Some(page) = page.upgrade() {
                    page.focus_task(&id);
                }
            }
        });
        card.append(&button);
    }
    let turn = ui::turn(Some(&ui::mark(Mark::Done, 24, "Director").upcast()), Some(&ui::speaker("Director")), &card);
    turn.upcast()
}

/// The sub-goal a turn came from, as a tag that opens its conversation; none for the goal.
pub(super) fn task_link(task: &Option<(String, String, WorkState)>, page: &Weak<WorkPage>) -> Option<gtk::Widget> {
    let (id, path, _) = task.as_ref()?;
    let link = gtk::Button::builder().child(&ui::sgtag(path)).tooltip_text("Show this sub-goal's conversation").build();
    link.add_css_class("row-button");
    link.set_valign(gtk::Align::Center);
    let (page, id) = (page.clone(), id.clone());
    link.connect_clicked(move |_| {
        if let Some(page) = page.upgrade() {
            page.focus_task(&id);
        }
    });
    Some(link.upcast())
}

/// One thing that happened on a goal or a task, as its kind reads.
pub(super) fn event_turn(
    e: &Event,
    report: Option<&Report>,
    task: Option<(String, String, WorkState)>,
    page: Weak<WorkPage>,
) -> gtk::Widget {
    let on = task.as_ref().map(|t| t.1.clone());
    match e.kind.as_str() {
        "report" => {
            let card = ui::card("raised");
            let head = ui::hbox(SPACE_2);
            head.append(&ui::line("Report", &["callout", "strong"]));
            if let Some(link) = task_link(&task, &page) {
                head.append(&link);
            }
            let verdict = report.map(|r| r.verdict.clone()).filter(|v| !v.is_empty()).or_else(|| verdict(&e.body));
            if let Some(verdict) = verdict {
                head.append(&verdict_pill(&verdict));
            }
            let at = ui::line(&kit::time::when(&e.at), &["meta", "text3"]);
            at.set_hexpand(true);
            at.set_xalign(1.0);
            head.append(&at);
            card.append(&head);
            match report {
                Some(r) => card.append(&report_view(r)),
                None => card.append(&folded_markdown(&e.body, 12)),
            }
            card.upcast()
        }
        "sent" => {
            let speaker = match task_link(&task, &page) {
                Some(link) => {
                    let row = ui::hbox(SPACE_1);
                    row.append(&ui::line("Sent to", &["speaker"]));
                    row.append(&link);
                    row.upcast()
                }
                None => ui::speaker("Sent"),
            };
            let meta = ui::meta(&speaker, Some(e.at.as_str()), None);
            ui::turn(
                Some(&ui::avatar("mail-send-symbolic", &["text2"])),
                Some(&meta),
                &ui::folded(&e.body, 4, &["body"]),
            )
            .upcast()
        }
        "decision" | "answer" => director_answer(&e.answer_text(), Some(e.at.as_str()), on.as_deref()),
        "verify" => {
            let passed = e.body.starts_with("pass");
            let text = format!(
                "{}{} · {}",
                if passed { "Verified" } else { "Verify failed" },
                on.map(|t| format!(" · {t}")).unwrap_or_default(),
                kit::time::when(&e.at)
            );
            let classes: &[&str] = if passed { &[] } else { &["failure"] };
            ui::event_line(&text, classes).upcast()
        }
        "pr" => ui::event_line(&format!("Landed · {} · {}", e.body, kit::time::when(&e.at)), &[]).upcast(),
        "state" => {
            let who = on.unwrap_or_else(|| "The goal".to_string());
            let failed = matches!(e.body.as_str(), "blocked" | "abandoned");
            let classes: &[&str] = if failed { &["failure"] } else { &[] };
            ui::event_line(&format!("{who} {} · {}", e.body, kit::time::when(&e.at)), classes).upcast()
        }
        _ => {
            let meta = ui::meta(&ui::speaker(&e.kind), Some(e.at.as_str()), on.as_deref());
            ui::turn(None, Some(&meta), &ui::folded(&e.body, 6, &["body"])).upcast()
        }
    }
}

/// A report's verdict: the value of its `STATUS:` line.
pub(super) fn verdict(body: &str) -> Option<String> {
    body.lines().find_map(|l| l.trim().strip_prefix("STATUS:")).map(|v| v.trim().to_string()).filter(|v| !v.is_empty())
}

/// Markdown that folds after `lines` lines, with Show all to read the rest.
pub(super) fn folded_markdown(body: &str, lines: usize) -> gtk::Widget {
    let total = body.lines().count();
    if total <= lines {
        return ui::markdown(body).upcast();
    }
    let column = ui::vbox(SPACE_2);
    let short: String = body.lines().take(lines).collect::<Vec<_>>().join("\n");
    let shown = gtk::Stack::builder().vhomogeneous(false).build();
    shown.add_named(&ui::markdown(&short), Some("short"));
    shown.add_named(&ui::markdown(body), Some("all"));
    shown.set_visible_child_name("short");
    let more = ui::link("Show all");
    let s = shown.clone();
    more.connect_clicked(move |b| {
        let all = s.visible_child_name().as_deref() == Some("all");
        s.set_visible_child_name(if all { "short" } else { "all" });
        b.set_label(if all { "Show all" } else { "Show less" });
    });
    column.append(&shown);
    column.append(&more);
    column.upcast()
}

/// The director answering in your place, settled: what it answered, and that it was the director.
pub(super) fn director_answer(text: &str, at: Option<&str>, on: Option<&str>) -> gtk::Widget {
    let card = ui::card("settled");
    let detail = on.map(|t| format!("on {t}"));
    card.append(&ui::meta(&ui::speaker("Director answered"), at, detail.as_deref()));
    card.append(&ui::said(text, &["body"]));
    ui::turn(Some(&ui::avatar("emblem-ok-symbolic", &["settled-text"])), None, &card).upcast()
}

/// The director leaving a part of a question to the person, and why.
fn declined_turn(e: &Event, declined: &kit::DirectorChoice) -> gtk::Widget {
    let card = ui::vbox(SPACE_1);
    card.append(&ui::meta(&ui::speaker("Director"), Some(e.at.as_str()), Some("left it to you")));
    card.append(&ui::said(&declined.question, &["callout", "medium"]));
    card.append(&ui::said(&kit::sentence(&declined.why), &["callout", "text2"]));
    ui::turn(Some(&ui::avatar("dialog-question-symbolic", &["attention-text"])), None, &card).upcast()
}

/// A question and its answer in one card: the director's answer set apart
/// in green, the person's as their bubble.
pub(super) fn exchange_turn(
    q: &Event,
    answer: Option<&Event>,
    decided: &[kit::DirectorChoice],
    asker: &str,
    task: Option<String>,
    page: Weak<WorkPage>,
) -> gtk::Widget {
    let card = ui::wd_card(SPACE_1);
    let asked = match (answer, &task) {
        (Some(a), _) if a.answered_by_director() => "Asked the director".to_string(),
        (_, Some(_)) => format!("{asker} asks you"),
        (_, None) => "Asked you".to_string(),
    };
    card.append(&ui::label(&asked));
    for item in q.asked_items() {
        card.append(&ui::said(&item.question, &["callout"]));
        for choice in &item.choices {
            card.append(&ui::text(&format!("○ {}", kit::unmarked(choice)), &["callout", "text2"]));
        }
    }
    match answer {
        Some(a) if a.answered_by_director() => {
            let answered = ui::hbox(SPACE_1);
            answered.add_css_class("wd-card");
            answered.add_css_class("answered");
            answered.set_margin_top(SPACE_2);
            let head = ui::label("Director answered");
            head.add_css_class("settled-text");
            head.set_valign(gtk::Align::Start);
            answered.append(&head);
            let said = ui::said(&format!("· {}", a.answer_text()), &["callout"]);
            said.set_hexpand(true);
            answered.append(&said);
            card.append(&answered);
            for part in decided.iter().filter(|d| !d.why.is_empty()) {
                let why = if decided.len() > 1 {
                    format!("{} · {}: {}", part.question, part.answer.as_deref().unwrap_or(""), part.why)
                } else {
                    format!("Why: {}", part.why)
                };
                card.append(&ui::said(&kit::sentence(&why), &["meta", "text2"]));
            }
        }
        Some(a) => card.append(&ui::you_bubble(&a.answer_text(), "You answered", Some(a.at.as_str()), None)),
        None => card.append(&ui::line("Not answered yet", &["meta", "text3"])),
    }
    let who = ui::hbox(6);
    who.append(&ui::speaker(if task.is_some() { "agent" } else { asker }));
    if let Some(id) = task {
        let tag = gtk::Button::builder().child(&ui::sgtag(asker)).build();
        tag.add_css_class("row-button");
        tag.connect_clicked(move |_| {
            if let Some(page) = page.upgrade() {
                page.focus_task(&id);
            }
        });
        who.append(&tag);
    }
    who.append(&ui::line(&format!("· {}", kit::time::when(&q.at)), &["speaker"]));
    let mark = ui::mark(Mark::NeedsYou, 24, "asked");
    ui::turn(Some(&mark.upcast()), Some(&who.upcast()), &card).upcast()
}

/// A supervisor's reply: what it said, then each agent it told, started or
/// filed, live, and what was refused.
pub(super) fn supervisor_turn(
    e: &Event,
    reply: &kit::SupervisorReply,
    tasks: &[(Option<Work>, Phase, Option<String>)],
    goal: &str,
    page: Weak<WorkPage>,
) -> gtk::Widget {
    let body = supervisor_body(reply, tasks, page.clone());
    let speaker = ui::link("Supervisor");
    speaker.add_css_class("speaker");
    let goal = goal.to_string();
    speaker.connect_clicked(move |_| {
        if let Some(page) = page.upgrade() {
            page.focus_task(&goal);
        }
    });
    let meta = ui::meta(&speaker.upcast(), Some(e.at.as_str()), None);
    ui::turn(Some(&ui::avatar("security-high-symbolic", &["text2"])), Some(&meta), &body).upcast()
}

/// What a supervisor said, the agents it told, started or filed, and what was refused.
pub(super) fn supervisor_body(
    reply: &kit::SupervisorReply,
    tasks: &[(Option<Work>, Phase, Option<String>)],
    page: Weak<WorkPage>,
) -> gtk::Box {
    let body = ui::vbox(SPACE_3);
    if !reply.said.is_empty() {
        body.append(&ui::markdown(&reply.said));
    }
    let chips = ui::vbox(SPACE_2);
    for (action, (task, phase, now)) in reply.actions.iter().zip(tasks) {
        match task {
            Some(task) => chips.append(&agent_chip(task, *phase, &action.line, now.as_deref(), page.clone())),
            None => {
                let row = ui::hbox(SPACE_2);
                row.append(&ui::icon("object-select-symbolic", &["text2"]));
                row.append(&ui::text(&action.line, &["callout", "text2"]));
                chips.append(&row);
            }
        }
    }
    if chips.first_child().is_some() {
        body.append(&chips);
    }
    for line in &reply.refused {
        let row = ui::hbox(SPACE_2);
        row.append(&ui::icon("window-close-symbolic", &["failure"]));
        row.append(&ui::said(line, &["callout", "failure"]));
        body.append(&row);
    }
    body
}

/// An agent inside the conversation: where its session is, what it is
/// doing, and what it was told.
pub(super) fn agent_chip(
    task: &Work,
    phase: Phase,
    told: &str,
    now: Option<&str>,
    page: Weak<WorkPage>,
) -> gtk::Widget {
    let row = ui::hbox(SPACE_2);
    let mark = phase_mark(phase);
    mark.set_valign(gtk::Align::Start);
    mark.set_margin_top(SPACE_1);
    row.append(&mark);
    let column = ui::vbox(2);
    column.set_hexpand(true);
    let top = ui::hbox(SPACE_2);
    top.append(&ui::line(&task.title, &["callout", "strong"]));
    top.append(&ui::state_icon(task.state));
    column.append(&top);
    column.append(&ui::line(told, &["meta", "text2"]));
    if let Some(now) = now.filter(|n| !n.is_empty()) {
        column.append(&ui::line(now, &["meta", "text3"]));
    }
    row.append(&column);
    row.append(&ui::icon("go-next-symbolic", &["text3"]));
    let button = gtk::Button::builder().child(&row).tooltip_text("Show this agent's conversation").build();
    button.add_css_class("agent-chip");
    let id = task.id.clone();
    button.connect_clicked(move |_| {
        if let Some(page) = page.upgrade() {
            page.focus_task(&id);
        }
    });
    button.upcast()
}

/// A task that stopped without a report, and what it last said.
pub(super) fn stopped_turn(
    e: &Event,
    said: &str,
    task: Option<(String, String, WorkState)>,
    page: Weak<WorkPage>,
) -> gtk::Widget {
    let avatar = task.as_ref().map(|t| ui::state_icon(t.2).upcast::<gtk::Widget>());
    let speaker = task_link(&task, &page).unwrap_or_else(|| ui::speaker("A task"));
    let meta = ui::meta(&speaker, Some(e.at.as_str()), Some("stopped without a report"));
    ui::turn(avatar.as_ref(), Some(&meta), &ui::folded(said, 3, &["body", "text2"])).upcast()
}

/// An agent's turn: its mark and name once, then what it said and did.
pub(super) fn agent_turn(head: Option<(String, WorkState, Option<String>)>, body: gtk::Widget) -> gtk::Widget {
    match head {
        Some((title, state, at)) => {
            let meta = ui::meta(&ui::speaker(&title), at.as_deref(), None);
            ui::turn(Some(&ui::state_icon(state).upcast()), Some(&meta), &body).upcast()
        }
        None => ui::turn(None, None, &body).upcast(),
    }
}

pub(super) fn agent_body(step: &Step, open: bool) -> gtk::Widget {
    match step.kind {
        StepKind::Thinking => {
            let expander = gtk::Expander::new(None);
            expander.set_label_widget(Some(&ui::line("Thinking", &["callout", "text3"])));
            expander.set_child(Some(&ui::said(step.text(), &["callout", "text2"])));
            expander.set_expanded(open);
            expander.upcast()
        }
        StepKind::Tool => match &step.tool {
            Some(call) => tool_row(call),
            None => ui::line("a tool call", &["callout", "text2"]).upcast(),
        },
        _ => ui::markdown(step.text()).upcast(),
    }
}

/// What a step said, as its turn copies it.
fn step_said(step: &Step) -> String {
    match &step.tool {
        Some(call) => tool_said(call),
        None => step.text().to_string(),
    }
}

/// A tool call as copied: its line, then its input and result as code.
fn tool_said(call: &ToolCall) -> String {
    let mut parts = vec![[call.name.as_str(), call.summary.as_str()].join(" ").trim().to_string()];
    for block in [Some(call.input.as_str()), call.result.as_deref()].into_iter().flatten().filter(|b| !b.is_empty()) {
        parts.push(format!("```\n{block}\n```"));
    }
    parts.join("\n\n")
}

/// One tool call in a line; its input and result on disclosure.
pub(super) fn tool_row(call: &ToolCall) -> gtk::Widget {
    let head = ui::hbox(SPACE_2);
    head.append(&ui::line(&call.name, &["callout", "strong", "text2"]));
    let summary = ui::line(&call.summary, &["callout", "text2"]);
    summary.set_hexpand(true);
    head.append(&summary);
    if call.failed {
        head.append(&ui::icon("dialog-error-symbolic", &["failure"]));
    } else if call.result.is_none() {
        let spinner = gtk::Spinner::new();
        spinner.start();
        head.append(&spinner);
    }
    let detail = ui::vbox(SPACE_2);
    if !call.input.is_empty() {
        detail.append(&ui::code_block(&call.input));
    }
    if let Some(result) = &call.result {
        detail.append(&ui::code_block(result));
    }
    let expander = gtk::Expander::new(None);
    expander.set_label_widget(Some(&head));
    expander.set_child(Some(&detail));
    expander.upcast()
}

/// A run of tool calls: one line each, a run of more than three folded into
/// "N tool calls".
pub(super) fn tool_group(calls: &[ToolCall]) -> gtk::Widget {
    let column = ui::vbox(SPACE_1);
    for call in calls {
        column.append(&tool_row(call));
    }
    if calls.len() <= 3 {
        return column.upcast();
    }
    let expander = gtk::Expander::new(None);
    expander.set_label_widget(Some(&ui::line(&format!("{} tool calls", calls.len()), &["callout", "text2"])));
    expander.set_child(Some(&column));
    expander.upcast()
}

/// A message on its way: your bubble with where it is, and when wd refused
/// it, why, with Retry and Discard.
pub(super) fn outgoing_bubble(app: &App, m: &Outgoing, waiting: &str) -> gtk::Widget {
    let status = match &m.state {
        OutgoingState::Sending => "· Sending…".to_string(),
        OutgoingState::Sent => format!("· {waiting}"),
        OutgoingState::Failed { .. } => "· not sent".to_string(),
    };
    let status: gtk::Widget = ui::line(&status, &["text3"]).upcast();
    let column = ui::you_bubble(&m.text, "You", None, Some(&status));
    if let OutgoingState::Failed { why, .. } = &m.state {
        let row = ui::hbox(SPACE_2);
        row.set_halign(gtk::Align::End);
        let why = ui::said(why, &["meta", "failure"]);
        why.set_max_width_chars(60);
        row.append(&why);
        let retry = ui::link("Retry");
        retry.add_css_class("failure");
        let discard = ui::link("Discard");
        discard.add_css_class("failure");
        let (a, id) = (app.clone(), m.id);
        retry.connect_clicked(move |_| a.store.retry(id));
        let a = app.clone();
        discard.connect_clicked(move |_| a.store.discard(id));
        row.append(&retry);
        row.append(&discard);
        column.append(&row);
    }
    column.upcast()
}

/// A report in parts: what it said with its verdict, then its files as path
/// chips with any note, then each named section; the first two show and the
/// rest open with "Show N more".
pub(super) fn report_view(r: &Report) -> gtk::Widget {
    let column = ui::vbox(SPACE_3);
    if !r.said.is_empty() {
        column.append(&ui::said(&r.said, &["body"]));
    }
    if !r.files.is_empty() || !r.file_note.is_empty() {
        let value = ui::vbox(SPACE_1);
        let chips = gtk::FlowBox::builder()
            .selection_mode(gtk::SelectionMode::None)
            .column_spacing(SPACE_1 as u32)
            .row_spacing(SPACE_1 as u32)
            .max_children_per_line(12)
            .build();
        for file in &r.files {
            chips.insert(&ui::tag(file, &["code"]), -1);
        }
        value.append(&chips);
        if !r.file_note.is_empty() {
            value.append(&ui::said(&r.file_note, &["callout", "text2"]));
        }
        column.append(&field("Files", &value));
    }
    let rest = ui::vbox(SPACE_3);
    for (i, section) in r.sections.iter().enumerate() {
        let shown = field(&title_case(&section.name), &ui::markdown(&section.text));
        if i < 2 {
            column.append(&shown);
        } else {
            rest.append(&shown);
        }
    }
    let more = r.sections.len().saturating_sub(2);
    if more > 0 {
        let revealer = gtk::Revealer::builder().child(&rest).reveal_child(false).build();
        let open = ui::link(&format!("Show {more} more"));
        let r = revealer.clone();
        open.connect_clicked(move |b| {
            let now = !r.reveals_child();
            r.set_reveal_child(now);
            let label = if now { "Show less".to_string() } else { format!("Show {more} more") };
            b.set_label(&label);
        });
        column.append(&revealer);
        column.append(&open);
    }
    column.upcast()
}

/// A labelled field: the name in a 64 px column, the value beside it.
pub(super) fn field(name: &str, value: &impl IsA<gtk::Widget>) -> gtk::Box {
    let row = ui::hbox(SPACE_3);
    let label = ui::line(name, &["meta", "text2"]);
    label.set_size_request(64, -1);
    label.set_valign(gtk::Align::Start);
    row.append(&label);
    value.set_hexpand(true);
    row.append(value);
    row
}

/// VERIFY as Verify.
pub(super) fn title_case(name: &str) -> String {
    let lower = name.to_lowercase();
    let mut chars = lower.chars();
    chars.next().map(|c| c.to_uppercase().chain(chars).collect()).unwrap_or_default()
}

/// A verdict as a pill: done green, blocked red, needs-input orange.
pub(super) fn verdict_pill(verdict: &str) -> gtk::Label {
    let class = match verdict.to_lowercase().as_str() {
        "done" => "settled-text",
        "blocked" => "failure",
        "needs-input" => "attention-text",
        _ => "text2",
    };
    ui::tag(verdict, &["meta", class])
}

/// A run of updates folded into one line that opens.
pub(super) fn updates_row(count: usize, at: &str, inner: Vec<Turn>) -> gtk::Widget {
    let column = ui::vbox(SPACE_3);
    for turn in inner {
        column.append(&turn.build());
    }
    let expander = gtk::Expander::new(None);
    expander
        .set_label_widget(Some(&ui::line(&format!("{count} updates · {}", kit::time::when(at)), &["meta", "text3"])));
    expander.set_child(Some(&column));
    expander.upcast()
}
