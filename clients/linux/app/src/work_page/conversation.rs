//! A goal read as one conversation, and a session as it happens: each turn
//! tagged with the sub-goal it came from, narrowed by who spoke or by sub-goal.
//! Every turn but an interactive card is text, so the conversation between
//! cards selects and copies as one.

use std::rc::{Rc, Weak};

use adw::prelude::*;

use kit::markdown::Align;
use kit::transcript::{self, Act, Doc, Icon, Ink, Look, Size, Style, Weight, BODY, HEAD_GAP, TURN_GAP};
use kit::{
    Attachment, Event, Feed, FilterChip, GoalDetail, Mark, Outgoing, OutgoingState, Phase, Question, Report,
    SessionStatus, Step, StepKind, ThreadFilter, ThreadItem, ThreadRow, ThreadStep, Tone, ToolCall, Work, WorkState,
    WorkUnderWay, WorkingRow,
};

use super::{Loaded, WorkPage, MERGE_SECONDS};
use crate::agents::phase_mark;
use crate::app::{App, Place};
use crate::question;
use crate::surface::Rich;
use crate::turns::Turn;
use crate::ui;
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
                        .text(move || Rich::new(transcript::told("You asked", Some(at.as_str()), &detail))),
                );
            }
            let plan: Vec<(String, String, WorkState, Option<String>)> = g
                .tree
                .iter()
                .map(|n| (n.work.id.clone(), n.work.title.clone(), n.work.state, n.opens().map(str::to_string)))
                .collect();
            let (weak, app) = (self.me.clone(), self.app.clone());
            turns.push(Turn::key("plan", &plan).text(move || plan_block(&plan, &app, weak)));
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
                turns.push(
                    Turn::key(format!("day-{}", item.id()), &label)
                        .text(move || Rich::new(transcript::event_line(&label, Ink::Text3, true))),
                );
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
                    turns.push(Turn::key(&id, &run).text(move || updates_row(count, &at, inner)));
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
                    .text(move || outgoing_bubble(&app, &m, "sent")),
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
            ThreadStep::Event(e) if e.kind == "attachment" => {
                let files: Vec<Attachment> = g.attached_alone(&e).into_iter().cloned().collect();
                let names = files.iter().map(|f| f.name.as_str()).collect::<Vec<_>>().join(", ");
                yours.push((format!("Attached {names}"), id.clone()));
                Turn::key(&id, &(&e, &files))
                    .copying(&files.iter().map(|f| f.path.as_str()).collect::<Vec<_>>().join("\n"))
                    .text(move || yours_with_files("", "You attached", Some(e.at.as_str()), &files))
            }
            ThreadStep::Event(e) if g.is_yours(&e) => {
                let said = e.decision.as_ref().map(|d| d.answer.clone()).unwrap_or_else(|| e.body.clone());
                let files: Vec<Attachment> = g.attached_to(&e).into_iter().cloned().collect();
                yours.push((if said.is_empty() { "Sent files".to_string() } else { said.clone() }, id.clone()));
                let label = if e.kind == "answer" { "You answered" } else { "You" };
                let waiting = g.waits_for_director(e.id);
                Turn::key(&id, &(&e, &files, waiting)).copying(&said).text(move || {
                    let mut rich = yours_with_files(&said, label, Some(e.at.as_str()), &files);
                    if waiting {
                        transcript::waiting_for_director(&mut rich.doc);
                    }
                    rich
                })
            }
            ThreadStep::Event(e) if g.finished_line(&e).is_some() => {
                let text = format!("{} · {}", g.finished_line(&e).unwrap_or_default(), kit::time::when(&e.at));
                Turn::key(&id, &text).text(move || Rich::new(transcript::event_line(&text, Ink::Text3, false)))
            }
            ThreadStep::Event(e) => {
                let task = self.named(g, &e.work);
                let report = g.reports.get(&e.id.to_string()).cloned();
                let weak = self.me.clone();
                Turn::key(&id, &(&e, &report))
                    .copying(&e.answer_text())
                    .text(move || event_turn(&e, report.as_ref(), task, weak))
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
                let files: Vec<Attachment> =
                    answer.as_ref().map(|a| g.attached_to(a).into_iter().cloned().collect()).unwrap_or_default();
                let waiting = answer.as_ref().is_some_and(|a| g.waits_for_director(a.id));
                Turn::key(&id, &(&q, &answer, &decided, &files, waiting)).copying(&said).text(move || {
                    let mut rich = exchange_turn(&q, answer.as_ref(), &decided, &files, &asker, target, weak);
                    if waiting {
                        transcript::waiting_for_director(&mut rich.doc);
                    }
                    rich
                })
            }
            ThreadStep::Look(e) => {
                let text = format!("Supervisor looking · {} · {}", e.body, kit::time::when(&e.at));
                Turn::key(&id, &text).text(move || Rich::new(transcript::event_line(&text, Ink::Text3, false)))
            }
            ThreadStep::Supervisor(e, reply) => {
                let tasks = self.told(g, &reply);
                let (weak, goal) = (self.me.clone(), g.goal.id.clone());
                Turn::key(&id, &(&e, &reply, &tasks))
                    .copying(&e.body)
                    .text(move || supervisor_turn(&e, &reply, &tasks, &goal, weak))
            }
            ThreadStep::Declined(e, declined) => Turn::key(&id, &(&e, &declined))
                .copying(&format!("{}\n\n{}", declined.question, declined.why))
                .text(move || declined_turn(&e, &declined)),
            ThreadStep::Stopped(e, said) => {
                let task = self.named(g, &e.work);
                let weak = self.me.clone();
                Turn::key(&id, &(&e, &said)).copying(&said).text(move || stopped_turn(&e, &said, task, weak))
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
                        let at = step.at.clone();
                        if director.iter().any(|d| text.contains(d.as_str())) {
                            turns.push(
                                Turn::key(&id, &step)
                                    .copying(&text)
                                    .text(move || director_answer(&text, at.as_deref(), None)),
                            );
                        } else {
                            let label = if index == 0 { "Brief" } else { "Told" };
                            turns.push(
                                Turn::key(&id, &step)
                                    .copying(&text)
                                    .text(move || Rich::new(transcript::told(label, at.as_deref(), &text))),
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
                            turns.push(Turn::key(&id, &q).text(move || answered_question(&q)));
                        }
                    }
                    _ => {
                        let open = index == last && running;
                        turns.push(Turn::key(&id, &(&step, open, &head)).copying(&step_said(&step)).text(move || {
                            Rich::new(transcript::turn(agent_head(head), transcript::step(&step, open)))
                        }));
                    }
                },
                ThreadRow::Tools { calls, .. } => {
                    let working = running && index + calls.len() > last;
                    turns.push(
                        Turn::key(&id, &(&calls, working, &head))
                            .copying(&calls.iter().map(tool_said).collect::<Vec<_>>().join("\n\n"))
                            .text(move || Rich::new(transcript::turn(agent_head(head), transcript::tools(&calls)))),
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
                        .text(move || outgoing_bubble(&app, &m, waiting)),
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
                since: f
                    .usage
                    .as_ref()
                    .and_then(|u| u.started.as_deref())
                    .or(last.and_then(|s| s.at.as_deref()))
                    .and_then(kit::time::instant),
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

/// An agent's head, for a turn that does not continue the one before.
fn agent_head(head: Option<(String, WorkState, Option<String>)>) -> Option<Doc> {
    head.map(|(title, state, at)| transcript::agent_head(&title, state, at.as_deref()))
}

/// A turn's head and body as one: the head's actions and widgets first.
fn turn(head: Option<Rich>, body: Rich) -> Rich {
    let (head_doc, mut acts, mut embeds) = match head {
        Some(h) => (Some(h.doc), h.acts, h.embeds),
        None => (None, Vec::new(), Vec::new()),
    };
    acts.extend(body.acts);
    embeds.extend(body.embeds);
    Rich { doc: transcript::turn(head_doc, body.doc), acts, embeds }
}

/// The sub-goal a turn came from, as a tag that opens its conversation.
fn task_tag(rich: &mut Rich, task: &Option<(String, String, WorkState)>, page: &Weak<WorkPage>) -> bool {
    let Some((id, path, _)) = task.as_ref() else { return false };
    let (page, id) = (page.clone(), id.clone());
    let act = rich.act(move || {
        if let Some(page) = page.upgrade() {
            page.focus_task(&id);
        }
    });
    rich.doc.pill(Look::Tag, path, tag_style().act(act));
    true
}

fn tag_style() -> Style {
    Style::of(Size::Small).weight(Weight::Medium).ink(Ink::Step)
}

/// Your words as a bubble, with the files you sent under it.
pub(super) fn yours_with_files(said: &str, label: &str, at: Option<&str>, files: &[Attachment]) -> Rich {
    let mut rich = Rich::new(transcript::told(label, at, said));
    if !files.is_empty() {
        rich.doc.para(HEAD_GAP, false).align(Align::End);
        let names = files.iter().map(|f| f.name.as_str()).collect::<Vec<_>>().join(", ");
        let files = files.to_vec();
        rich.embed(&names, move || ui::attachments(&files).upcast());
    }
    rich
}

/// The director's plan: each sub-goal with where it stands, a way into it.
pub(super) fn plan_block(
    plan: &[(String, String, WorkState, Option<String>)],
    app: &App,
    page: Weak<WorkPage>,
) -> Rich {
    let mut rich = Rich::new(transcript::head(Some(Icon::Mark(Mark::Done)), "Director", None, None));
    let mut body = Rich::default();
    if plan.is_empty() {
        body.doc.text("Not planned yet.", Style::CALLOUT.ink(Ink::Text3));
    } else {
        body.doc.open_frame(Look::Plan);
        for (i, (id, title, state, opens)) in plan.iter().enumerate() {
            let (app, page, id, opens) = (app.clone(), page.clone(), id.clone(), opens.clone());
            let act = body.act(move || match &opens {
                Some(goal) => app.open(Place::Work(goal.clone())),
                None => {
                    if let Some(page) = page.upgrade() {
                        page.focus_task(&id);
                    }
                }
            });
            body.doc.para(if i == 0 { 0 } else { 6 }, false);
            let (badge, tone) = state.badge();
            let badge_style = Style::of(Size::Small).weight(Weight::Semibold).ink(Ink::Badge(tone));
            body.doc.pill(Look::Badge(tone), badge, badge_style.act(act.clone()));
            body.doc.ornament("  ", Style::CALLOUT);
            body.doc.push(transcript::Content::Text(title.clone()), Style::CALLOUT.act(act));
        }
        body.doc.close_frame();
    }
    let head = std::mem::take(&mut rich);
    turn(Some(head), body)
}

/// One thing that happened on a goal or a task, as its kind reads.
pub(super) fn event_turn(
    e: &Event,
    report: Option<&Report>,
    task: Option<(String, String, WorkState)>,
    page: Weak<WorkPage>,
) -> Rich {
    let on = task.as_ref().map(|t| t.1.clone());
    match e.kind.as_str() {
        "report" => {
            let mut rich = Rich::default();
            rich.doc.indent(BODY);
            rich.doc.open_frame(Look::Card);
            rich.doc.para(TURN_GAP, true);
            rich.doc.push(transcript::Content::Text("Report".into()), Style::CALLOUT.weight(Weight::Semibold));
            rich.doc.ornament(" ", Style::CALLOUT);
            task_tag(&mut rich, &task, &page);
            let verdict = report.map(|r| r.verdict.clone()).filter(|v| !v.is_empty()).or_else(|| verdict(&e.body));
            if let Some(verdict) = verdict {
                let tone = verdict_tone(&verdict);
                rich.doc.ornament(" ", Style::CALLOUT);
                rich.doc.pill(
                    Look::Badge(tone),
                    &verdict,
                    Style::of(Size::Small).weight(Weight::Semibold).ink(Ink::Badge(tone)),
                );
            }
            transcript::meta_tail(&mut rich.doc, Some(e.at.as_str()), None);
            let mut body = match report {
                Some(r) => report_view(r),
                None => Rich::new(transcript::markdown_folded(&e.body, 12)),
            };
            if let Some(first) = body.doc.paras.first_mut() {
                first.above += 12;
            }
            rich.append(body);
            rich.doc.close_frame();
            rich
        }
        "sent" => {
            let mut head = Rich::new(transcript::head(
                Some(Icon::Named("mail-send-symbolic", Ink::Text2)),
                if task.is_some() { "Sent to " } else { "Sent" },
                None,
                None,
            ));
            task_tag(&mut head, &task, &page);
            transcript::meta_tail(&mut head.doc, Some(e.at.as_str()), None);
            let mut body = Doc::new();
            transcript::folded(&mut body, &e.body, 4, Style::BODY);
            turn(Some(head), Rich::new(body))
        }
        "decision" | "answer" => director_answer(&e.answer_text(), Some(e.at.as_str()), on.as_deref()),
        "verify" => {
            let passed = e.body.starts_with("pass");
            let text = format!(
                "{}{} · {}",
                if passed { "Checks passed" } else { "Checks failed" },
                on.map(|t| format!(" · {t}")).unwrap_or_default(),
                kit::time::when(&e.at)
            );
            Rich::new(transcript::event_line(&text, if passed { Ink::Text3 } else { Ink::Failure }, false))
        }
        "pr" => Rich::new(transcript::event_line(
            &format!("Delivered · {} · {}", e.body, kit::time::when(&e.at)),
            Ink::Text3,
            false,
        )),
        "state" => {
            let who = on.unwrap_or_else(|| "The goal".to_string());
            let failed = matches!(e.body.as_str(), "blocked" | "abandoned");
            let text = format!("{who}: {} · {}", kit::words::event_body("state", &e.body), kit::time::when(&e.at));
            Rich::new(transcript::event_line(&text, if failed { Ink::Failure } else { Ink::Text3 }, false))
        }
        _ => {
            let head = transcript::head(None, &kit::words::event(&e.kind), Some(e.at.as_str()), on.as_deref());
            let mut body = Doc::new();
            transcript::folded(&mut body, &e.body, 6, Style::BODY);
            Rich::new(transcript::turn(Some(head), body))
        }
    }
}

/// A report's verdict: the value of its `STATUS:` line.
pub(super) fn verdict(body: &str) -> Option<String> {
    body.lines().find_map(|l| l.trim().strip_prefix("STATUS:")).map(|v| v.trim().to_string()).filter(|v| !v.is_empty())
}

/// A verdict's tone: done settled, blocked and needs-input for the person.
fn verdict_tone(verdict: &str) -> Tone {
    match verdict.to_lowercase().as_str() {
        "done" => Tone::Settled,
        "blocked" | "needs-input" => Tone::Attention,
        _ => Tone::Waiting,
    }
}

/// The director answering in your place, settled: what it answered, and that it was the director.
pub(super) fn director_answer(text: &str, at: Option<&str>, on: Option<&str>) -> Rich {
    let mut doc = Doc::new();
    doc.indent(BODY);
    doc.open_frame(Look::Settled);
    doc.para(TURN_GAP, true).avatar(Icon::Named("emblem-ok-symbolic", Ink::Settled));
    doc.push(transcript::Content::Text("Director answered".into()), Style::SPEAKER);
    let detail = on.map(|t| format!("on {t}"));
    transcript::meta_tail(&mut doc, at, detail.as_deref());
    doc.para(4, false);
    doc.text(text, Style::BODY);
    doc.close_frame();
    Rich::new(doc)
}

/// The director leaving a part of a question to the person, and why.
fn declined_turn(e: &Event, declined: &kit::DirectorChoice) -> Rich {
    let head = transcript::head(
        Some(Icon::Named("dialog-question-symbolic", Ink::Attention)),
        "Director",
        Some(e.at.as_str()),
        Some("left it to you"),
    );
    let mut body = Doc::new();
    body.text(&declined.question, Style::CALLOUT.weight(Weight::Medium));
    body.para(4, false);
    body.text(&kit::sentence(&declined.why), Style::CALLOUT.ink(Ink::Text2));
    Rich::new(transcript::turn(Some(head), body))
}

/// A question and its answer in one card: the director's answer set apart
/// in green, the person's as their bubble.
pub(super) fn exchange_turn(
    q: &Event,
    answer: Option<&Event>,
    decided: &[kit::DirectorChoice],
    files: &[Attachment],
    asker: &str,
    task: Option<String>,
    page: Weak<WorkPage>,
) -> Rich {
    let mut head = Rich::new(transcript::head(
        Some(Icon::Mark(Mark::NeedsYou)),
        if task.is_some() { "agent " } else { asker },
        None,
        None,
    ));
    if let Some(id) = task.clone() {
        let named = Some((id, asker.to_string(), WorkState::Running));
        task_tag(&mut head, &named, &page);
    }
    transcript::meta_tail(&mut head.doc, Some(q.at.as_str()), None);

    let mut body = Rich::default();
    body.doc.open_frame(Look::Card);
    let asked = match (answer, &task) {
        (Some(a), _) if a.answered_by_director() => "Asked the director".to_string(),
        (_, Some(_)) => format!("{asker} asks you"),
        (_, None) => "Asked you".to_string(),
    };
    body.doc.text(&asked, Style::LABEL);
    for item in q.asked_items() {
        body.doc.para(4, false);
        body.doc.text(&item.question, Style::CALLOUT);
        for choice in &item.choices {
            body.doc.para(2, false);
            body.doc.ornament("○ ", Style::CALLOUT.ink(Ink::Text2));
            body.doc.text(kit::unmarked(choice), Style::CALLOUT.ink(Ink::Text2));
        }
    }
    match answer {
        Some(a) if a.answered_by_director() => {
            body.doc.open_frame(Look::Settled);
            body.doc.para(8, false);
            body.doc.push(
                transcript::Content::Text("Director answered".into()),
                Style::META.weight(Weight::Medium).ink(Ink::Settled),
            );
            body.doc.text(&format!(" · {}", a.answer_text()), Style::CALLOUT);
            body.doc.close_frame();
            for part in decided.iter().filter(|d| !d.why.is_empty()) {
                let why = if decided.len() > 1 {
                    format!("{} · {}: {}", part.question, part.answer.as_deref().unwrap_or(""), part.why)
                } else {
                    format!("Why: {}", part.why)
                };
                body.doc.para(6, false);
                body.doc.text(&kit::sentence(&why), Style::META.ink(Ink::Text2));
            }
        }
        Some(a) => {
            let mut yours = yours_with_files(&a.answer_text(), "You answered", Some(a.at.as_str()), files);
            if let Some(first) = yours.doc.paras.first_mut() {
                first.above = 8;
            }
            body.append(yours);
        }
        None => {
            body.doc.para(6, false);
            body.doc.text("Not answered yet", Style::META.ink(Ink::Text3));
        }
    }
    body.doc.close_frame();
    turn(Some(head), body)
}

/// A supervisor's reply: what it said, then each agent it told, started or
/// filed, live, and what was refused.
pub(super) fn supervisor_turn(
    e: &Event,
    reply: &kit::SupervisorReply,
    tasks: &[(Option<Work>, Phase, Option<String>)],
    goal: &str,
    page: Weak<WorkPage>,
) -> Rich {
    let mut head = Rich::default();
    let (weak, goal) = (page.clone(), goal.to_string());
    let act = head.act(move || {
        if let Some(page) = weak.upgrade() {
            page.focus_task(&goal);
        }
    });
    head.doc.para(0, true).avatar(Icon::Named("security-high-symbolic", Ink::Text2));
    head.doc.paras[0].indent = BODY;
    head.doc.push(transcript::Content::Text("Supervisor".into()), Style::SPEAKER.ink(Ink::Link).act(act));
    transcript::meta_tail(&mut head.doc, Some(e.at.as_str()), None);
    turn(Some(head), supervisor_body(reply, tasks, page))
}

/// What a supervisor said, the agents it told, started or filed, and what was refused.
pub(super) fn supervisor_body(
    reply: &kit::SupervisorReply,
    tasks: &[(Option<Work>, Phase, Option<String>)],
    page: Weak<WorkPage>,
) -> Rich {
    let mut body = Rich::new(transcript::markdown(&reply.said));
    for (i, (action, (task, phase, now))) in reply.actions.iter().zip(tasks).enumerate() {
        let gap = if i == 0 && reply.said.is_empty() {
            0
        } else if i == 0 {
            12
        } else {
            8
        };
        match task {
            Some(task) => {
                let mut chip = agent_chip(task, *phase, &action.line, now.as_deref(), page.clone());
                if let Some(first) = chip.doc.paras.first_mut() {
                    first.above += gap;
                }
                body.append(chip);
            }
            None => {
                body.doc.para(gap, false);
                body.doc.icon(Icon::Named("object-select-symbolic", Ink::Text2));
                body.doc.ornament(" ", Style::CALLOUT);
                body.doc.text(&action.line, Style::CALLOUT.ink(Ink::Text2));
            }
        }
    }
    for line in &reply.refused {
        body.doc.para(8, false);
        body.doc.icon(Icon::Named("window-close-symbolic", Ink::Failure));
        body.doc.ornament(" ", Style::CALLOUT);
        body.doc.text(line, Style::CALLOUT.ink(Ink::Failure));
    }
    body
}

/// An agent inside the conversation: where its session is, what it is
/// doing, and what it was told; a click shows its conversation.
pub(super) fn agent_chip(task: &Work, phase: Phase, told: &str, now: Option<&str>, page: Weak<WorkPage>) -> Rich {
    let mut chip = Rich::default();
    let id = task.id.clone();
    let act = chip.act(move || {
        if let Some(page) = page.upgrade() {
            page.focus_task(&id);
        }
    });
    chip.doc.open_frame(Look::Chip);
    chip.doc.para(0, false);
    chip.embed("", move || phase_mark(phase));
    chip.doc.ornament("  ", Style::CALLOUT);
    chip.doc
        .push(transcript::Content::Text(task.title.clone()), Style::CALLOUT.weight(Weight::Semibold).act(act.clone()));
    chip.doc.ornament(" ", Style::CALLOUT);
    chip.doc.icon(Icon::State(task.state));
    chip.doc.para(2, false);
    chip.doc.text(told, Style::META.ink(Ink::Text2).act(act.clone()));
    if let Some(now) = now.filter(|n| !n.is_empty()) {
        chip.doc.para(2, false);
        chip.doc.text(now, Style::META.ink(Ink::Text3).act(act));
    }
    chip.doc.close_frame();
    chip
}

/// A task that stopped without a report, and what it last said.
pub(super) fn stopped_turn(
    e: &Event,
    said: &str,
    task: Option<(String, String, WorkState)>,
    page: Weak<WorkPage>,
) -> Rich {
    let avatar = task.as_ref().map(|t| Icon::State(t.2));
    let mut head = Rich::new(transcript::head(avatar, if task.is_some() { "" } else { "A task" }, None, None));
    task_tag(&mut head, &task, &page);
    transcript::meta_tail(&mut head.doc, Some(e.at.as_str()), Some("stopped without a report"));
    let mut body = Doc::new();
    transcript::folded(&mut body, said, 3, Style::BODY.ink(Ink::Text2));
    turn(Some(head), Rich::new(body))
}

/// A question the session asked and how it was answered.
fn answered_question(question: &Question) -> Rich {
    let mut doc = Doc::new();
    doc.indent(BODY);
    doc.open_frame(Look::Card);
    doc.para(TURN_GAP, true);
    doc.text("Asked", Style::LABEL);
    for (i, item) in question.items.iter().enumerate() {
        let answer = question.answers.as_ref().and_then(|a| a.get(i));
        let parts: Vec<&str> = answer.map(|a| a.split(", ").collect()).unwrap_or_default();
        doc.para(8, false);
        doc.text(&item.question, Style::CALLOUT.weight(Weight::Medium));
        for option in &item.options {
            let chosen = parts.contains(&option.label.as_str());
            doc.para(2, false);
            doc.ornament(
                if chosen { "◉ " } else { "○ " },
                Style::CALLOUT.ink(if chosen { Ink::Accent } else { Ink::Text3 }),
            );
            let style = if chosen { Style::CALLOUT.weight(Weight::Medium) } else { Style::CALLOUT.ink(Ink::Text2) };
            doc.text(kit::unmarked(&option.label), style);
        }
        if let Some(answer) = answer.filter(|a| !item.options.iter().any(|o| a.split(", ").any(|p| p == o.label))) {
            doc.para(4, false);
            doc.text(answer, Style::CALLOUT);
        }
    }
    if let Some(reply) = &question.reply {
        doc.para(8, false);
        doc.text(reply, Style::CALLOUT.ink(Ink::Text2));
    }
    doc.close_frame();
    Rich::new(doc)
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

/// A message on its way: your bubble with where it is, and when wd refused
/// it, why, with Retry and Discard.
pub(super) fn outgoing_bubble(app: &App, m: &Outgoing, waiting: &str) -> Rich {
    let status = match &m.state {
        OutgoingState::Sending => "Sending…".to_string(),
        OutgoingState::Sent => waiting.to_string(),
        OutgoingState::Failed { .. } => "not sent".to_string(),
    };
    let mut rich = Rich::new(transcript::told("You", None, &m.text));
    if let Some(meta) = rich.doc.paras.first_mut() {
        meta.spans.push(transcript::Span {
            content: transcript::Content::Text(format!(" · {status}")),
            style: Style::META.ink(Ink::Text3),
        });
    }
    if !m.files.is_empty() {
        rich.doc.para(HEAD_GAP, false).align(Align::End);
        for (i, path) in m.files.iter().enumerate() {
            let name =
                std::path::Path::new(path).file_name().map_or(path.clone(), |n| n.to_string_lossy().into_owned());
            if i > 0 {
                rich.doc.ornament(" ", Style::META);
            }
            rich.doc.pill(Look::Tag, &name, Style::META.ink(Ink::Text2));
        }
    }
    if let OutgoingState::Failed { why, .. } = &m.state {
        let (a, id) = (app.clone(), m.id);
        let retry = rich.act(move || a.store.retry(id));
        let a = app.clone();
        let discard = rich.act(move || a.store.discard(id));
        rich.doc.para(HEAD_GAP, false).align(Align::End);
        rich.doc.text(why, Style::META.ink(Ink::Failure));
        rich.doc.ornament("   ", Style::META);
        rich.doc.ornament("Retry", Style::META.ink(Ink::Failure).weight(Weight::Medium).act(retry));
        rich.doc.ornament("   ", Style::META);
        rich.doc.ornament("Discard", Style::META.ink(Ink::Failure).weight(Weight::Medium).act(discard));
    }
    rich
}

/// A report in parts: what it said, then its files as tags with any note,
/// then each named section; the first two show and the rest open with
/// "Show N more".
pub(super) fn report_view(r: &Report) -> Rich {
    let mut doc = Doc::new();
    let mut gap = 0;
    if !r.said.is_empty() {
        doc.para(0, false);
        doc.text(&r.said, Style::BODY);
        gap = 12;
    }
    if !r.files.is_empty() || !r.file_note.is_empty() {
        doc.para(gap, true);
        doc.text("Files", Style::META.ink(Ink::Text2));
        if !r.files.is_empty() {
            doc.para(4, false);
            for (i, file) in r.files.iter().enumerate() {
                if i > 0 {
                    doc.ornament("  ", Style::META);
                }
                doc.pill(Look::Tag, file, Style::of(Size::Code).ink(Ink::Step));
            }
        }
        if !r.file_note.is_empty() {
            doc.para(4, false);
            doc.text(&r.file_note, Style::CALLOUT.ink(Ink::Text2));
        }
        gap = 12;
    }
    let mut fold = None;
    for (i, section) in r.sections.iter().enumerate() {
        if i == 2 {
            fold = Some(doc.open_fold(false));
        }
        doc.para(gap, true);
        doc.text(&title_case(&section.name), Style::META.ink(Ink::Text2));
        let mut text = transcript::markdown(&section.text);
        if let Some(first) = text.paras.first_mut() {
            first.above += 4;
            first.starts = false;
        }
        doc.append(text);
        gap = 12;
    }
    if let Some(fold) = fold {
        doc.close_fold();
        let more = r.sections.len() - 2;
        doc.para(8, false);
        let link = Style::META.ink(Ink::Link).act(Act::Toggle(fold));
        doc.ornament(&format!("Show {more} more"), link.clone().show(transcript::Show::Closed(fold)));
        doc.ornament("Show less", link.show(transcript::Show::Open(fold)));
    }
    Rich::new(doc)
}

/// VERIFY as Verify.
pub(super) fn title_case(name: &str) -> String {
    let lower = name.to_lowercase();
    let mut chars = lower.chars();
    chars.next().map(|c| c.to_uppercase().chain(chars).collect()).unwrap_or_default()
}

/// A run of updates folded into one line that opens.
pub(super) fn updates_row(count: usize, at: &str, inner: Vec<Turn>) -> Rich {
    let mut rich = Rich::default();
    let fold = rich.doc.folds.len();
    rich.doc.para(TURN_GAP, true);
    let act = Act::Toggle(fold);
    let style = Style::META.ink(Ink::Text3).act(act);
    rich.doc.ornament("▸ ", style.clone().show(transcript::Show::Closed(fold)));
    rich.doc.ornament("▾ ", style.clone().show(transcript::Show::Open(fold)));
    rich.doc.text(&format!("{count} updates · {}", kit::time::when(at)), style);
    rich.doc.open_fold(false);
    for turn in inner {
        if let Some(inner) = turn.rich() {
            rich.append(inner);
        }
    }
    rich.doc.close_fold();
    rich
}
