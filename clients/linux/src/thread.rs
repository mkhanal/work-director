//! A goal read as one conversation.

use std::collections::{HashMap, HashSet};

use crate::conversation::{Question, QuestionItem, QuestionOption, SessionStatus};
use crate::ledger::{Event, GoalDetail, Work, WorkState};

/// What a goal's conversation is narrowed to.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum ThreadFilter {
    All,
    You,
    Supervisor,
    Reports,
    Questions,
}

impl ThreadFilter {
    pub const ALL: [ThreadFilter; 5] = [
        ThreadFilter::All,
        ThreadFilter::You,
        ThreadFilter::Supervisor,
        ThreadFilter::Reports,
        ThreadFilter::Questions,
    ];

    pub fn label(self) -> &'static str {
        match self {
            ThreadFilter::All => "Everything",
            ThreadFilter::You => "You",
            ThreadFilter::Supervisor => "Supervisor",
            ThreadFilter::Reports => "Reports",
            ThreadFilter::Questions => "Questions",
        }
    }
}

/// One row of a goal's conversation as it reads: a step, or a run of
/// updates folded into one row that opens.
#[derive(Debug, Clone, PartialEq)]
pub enum ThreadItem {
    Step(Box<ThreadStep>),
    Updates(Vec<ThreadStep>),
}

impl ThreadItem {
    /// The id of its first step.
    pub fn id(&self) -> i64 {
        match self {
            ThreadItem::Step(s) => s.id(),
            ThreadItem::Updates(run) => run.first().map_or(0, ThreadStep::id),
        }
    }
}

/// How far a goal's tasks have come, by where each stands.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct TaskProgress {
    pub done: usize,
    pub working: usize,
    pub review: usize,
    pub blocked: usize,
    pub waiting: usize,
    pub dropped: usize,
}

impl TaskProgress {
    pub fn total(&self) -> usize {
        self.done + self.working + self.review + self.blocked + self.waiting + self.dropped
    }
}

/// A task waiting on the person with no question open in its session, and
/// what it asked.
#[derive(Debug, Clone, PartialEq)]
pub struct WaitingTask {
    pub task: Work,
    pub question: Question,
}

/// One step of a goal read as a conversation.
#[derive(Debug, Clone, PartialEq)]
pub enum ThreadStep {
    /// Something decided, said, asked, answered, reported, verified or
    /// landed, or a state change that ends work.
    Event(Event),
    /// The director asked the goal's supervisor to look, and why.
    Look(Event),
    /// The supervisor's reply, carried out.
    Supervisor(Event, SupervisorReply),
    /// A task's session stopped without a report, and what it last said.
    Stopped(Event, String),
    /// A question asked on the goal or a task, with its answer once it has one.
    Exchange(Event, Option<Event>),
}

impl ThreadStep {
    pub fn event(&self) -> &Event {
        match self {
            ThreadStep::Event(e)
            | ThreadStep::Look(e)
            | ThreadStep::Supervisor(e, _)
            | ThreadStep::Stopped(e, _)
            | ThreadStep::Exchange(e, _) => e,
        }
    }

    pub fn id(&self) -> i64 {
        self.event().id
    }
}

/// What a supervisor said, what was done because of it and what was refused.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SupervisorReply {
    /// Its words, without its action lines.
    pub said: String,
    pub actions: Vec<SupervisorAction>,
    pub refused: Vec<String>,
}

/// One thing done on a supervisor's word.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SupervisorAction {
    /// The action as the supervisor wrote it.
    pub line: String,
    /// The task it told, started or filed, if any.
    pub task: Option<String>,
}

/// One part of a question: a line asked, and the `- ` choices under it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AskedItem {
    pub question: String,
    pub choices: Vec<String>,
}

const STOPPED_PREFIX: &str = "stopped without a report: ";
const SUPERVISE_PREFIX: &str = "supervise ";
const ENDING_STATES: [&str; 5] = ["done", "dropped", "abandoned", "paused", "blocked"];

impl GoalDetail {
    /// Whether a supervisor has been started on the goal.
    pub fn supervised(&self) -> bool {
        self.events.iter().any(|e| e.work == self.goal.id && e.kind == "spawn" && e.body.starts_with(SUPERVISE_PREFIX))
    }

    /// The goal as one conversation, oldest first: each answer read with the
    /// question it answers.
    pub fn thread(&self) -> Vec<ThreadStep> {
        let mut steps: Vec<ThreadStep> = Vec::new();
        let mut open: HashMap<&str, usize> = HashMap::new();
        // An answer reaches its session as a message too; it is read once, as the answer.
        let answers: HashSet<(&str, &str)> =
            self.events.iter().filter(|e| e.kind == "answer").map(|e| (e.work.as_str(), e.body.as_str())).collect();
        let mut events: Vec<&Event> = self.events.iter().collect();
        events.sort_by(|a, b| a.at.cmp(&b.at).then(a.id.cmp(&b.id)));
        for e in events {
            if e.kind == "sent" && answers.contains(&(e.work.as_str(), e.body.as_str())) {
                continue;
            }
            if e.kind == "question" {
                open.insert(e.work.as_str(), steps.len());
                steps.push(ThreadStep::Exchange(e.clone(), None));
                continue;
            }
            if e.kind == "answer" {
                if let Some(i) = open.remove(e.work.as_str()) {
                    let asked = match &steps[i] {
                        ThreadStep::Exchange(q, None) => Some(q.clone()),
                        _ => None,
                    };
                    if let Some(q) = asked {
                        steps[i] = ThreadStep::Exchange(q, Some(e.clone()));
                        continue;
                    }
                }
            }
            if let Some(step) = self.step(e) {
                steps.push(step);
            }
        }
        steps
    }

    fn step(&self, e: &Event) -> Option<ThreadStep> {
        match e.kind.as_str() {
            "decision" | "report" | "sent" | "answer" | "verify" | "pr" => Some(ThreadStep::Event(e.clone())),
            "state" => ENDING_STATES.contains(&e.body.as_str()).then(|| ThreadStep::Event(e.clone())),
            "look" => Some(ThreadStep::Look(e.clone())),
            "supervised" => Some(ThreadStep::Supervisor(e.clone(), self.reply(&e.body))),
            "note" => e.body.strip_prefix(STOPPED_PREFIX).map(|said| ThreadStep::Stopped(e.clone(), said.to_string())),
            _ => None,
        }
    }

    /// A supervised event's body: the reply, then `Done:` and `Refused:`
    /// lists, each line `- ` and one action.
    pub fn reply(&self, body: &str) -> SupervisorReply {
        let (mut said, mut done, mut refused) = (Vec::new(), Vec::new(), Vec::new());
        let mut section = 0;
        let mut in_actions = false;
        for line in body.split('\n') {
            match line {
                "Done:" => {
                    section = 1;
                    continue;
                }
                "Refused:" => {
                    section = 2;
                    continue;
                }
                _ => {}
            }
            if section > 0 {
                if let Some(action) = line.strip_prefix("- ") {
                    if section == 1 {
                        done.push(action.to_string());
                    } else {
                        refused.push(action.to_string());
                    }
                }
                continue;
            }
            if line.trim().trim_matches('*') == "ACTIONS:" {
                in_actions = true;
            }
            if !in_actions {
                said.push(line);
            }
        }
        SupervisorReply {
            said: said.join("\n").trim().to_string(),
            actions: done.into_iter().map(|line| SupervisorAction { task: self.task_of(&line), line }).collect(),
            refused,
        }
    }

    /// The task an action line acts on: the one it names, or the one it filed.
    pub fn task_of(&self, line: &str) -> Option<String> {
        for verb in ["SEND ", "START "] {
            if let Some(rest) = line.strip_prefix(verb) {
                let id: String = rest.chars().take_while(|c| *c != ':' && *c != ' ').collect();
                return self.tasks.iter().any(|t| t.id == id).then_some(id);
            }
        }
        let title = line.strip_prefix("TASK: ")?;
        self.tasks.iter().rev().find(|t| t.title == title).map(|t| t.id.clone())
    }

    /// What the person said on the goal itself, oldest first: its decisions
    /// and answers.
    pub fn yours(&self) -> Vec<Event> {
        self.thread()
            .into_iter()
            .filter_map(|step| match step {
                ThreadStep::Event(e) if self.is_yours(&e) => Some(e),
                ThreadStep::Exchange(_, Some(a)) if self.is_yours(&a) => Some(a),
                _ => None,
            })
            .collect()
    }

    /// Whether the person said it: a decision on the goal, or an answer the
    /// director did not give.
    pub fn is_yours(&self, e: &Event) -> bool {
        match e.kind.as_str() {
            "decision" => e.work == self.goal.id,
            "answer" => !e.answered_by_director(),
            _ => false,
        }
    }

    pub fn title_of(&self, id: &str) -> Option<&str> {
        self.tasks.iter().find(|t| t.id == id).map(|t| t.title.as_str())
    }
}

impl Event {
    /// Whether the director gave this answer, from what was decided or by its
    /// judgement, rather than the person.
    pub fn answered_by_director(&self) -> bool {
        self.kind == "answer" && self.decision.as_ref().is_some_and(|d| d.source == "director")
    }

    /// An answer's words, one `question → answer` line per item when it
    /// answers several.
    pub fn answer_text(&self) -> String {
        let mut lines = self.body.split('\n');
        if lines.next() != Some("Answers to your questions:") {
            return self.body.clone();
        }
        lines.collect::<Vec<_>>().join("\n")
    }

    /// A question's parts: each line a question, the `- ` lines under it its
    /// choices.
    pub fn asked_items(&self) -> Vec<AskedItem> {
        let mut items: Vec<AskedItem> = Vec::new();
        for raw in self.body.split('\n') {
            let line = raw.trim();
            if line.is_empty() {
                continue;
            }
            match (line.strip_prefix("- "), items.last_mut()) {
                (Some(choice), Some(item)) => item.choices.push(choice.to_string()),
                _ => items.push(AskedItem { question: line.to_string(), choices: Vec::new() }),
            }
        }
        items
    }
}

impl GoalDetail {
    /// The conversation narrowed to one kind of turn. Everything folds each
    /// run of two or more updates — looks, state changes, checks, landings,
    /// messages told to a task — into one row.
    pub fn items(&self, filter: ThreadFilter) -> Vec<ThreadItem> {
        let mut out = Vec::new();
        let mut run: Vec<ThreadStep> = Vec::new();
        let flush = |run: &mut Vec<ThreadStep>, out: &mut Vec<ThreadItem>| match run.len() {
            0 => {}
            1 => out.push(ThreadItem::Step(Box::new(run.remove(0)))),
            _ => out.push(ThreadItem::Updates(std::mem::take(run))),
        };
        for step in self.thread().into_iter().filter(|s| self.shows(s, filter)) {
            if filter == ThreadFilter::All && is_update(&step) {
                run.push(step);
                continue;
            }
            flush(&mut run, &mut out);
            out.push(ThreadItem::Step(Box::new(step)));
        }
        flush(&mut run, &mut out);
        out
    }

    fn shows(&self, step: &ThreadStep, filter: ThreadFilter) -> bool {
        match (filter, step) {
            (ThreadFilter::All, _) => true,
            (ThreadFilter::You, ThreadStep::Event(e)) => self.is_yours(e),
            (ThreadFilter::You, ThreadStep::Exchange(_, Some(a))) => self.is_yours(a),
            (ThreadFilter::Supervisor, ThreadStep::Supervisor(..)) => true,
            (ThreadFilter::Supervisor, ThreadStep::Exchange(q, _)) => q.work == self.goal.id,
            (ThreadFilter::Reports, ThreadStep::Event(e)) => e.kind == "report",
            (ThreadFilter::Questions, ThreadStep::Exchange(..)) => true,
            _ => false,
        }
    }

    /// Where the goal's tasks stand.
    pub fn progress(&self) -> TaskProgress {
        let mut p = TaskProgress::default();
        for t in &self.tasks {
            match t.state {
                WorkState::Done | WorkState::SoftDone => p.done += 1,
                WorkState::Running | WorkState::NeedsInput => p.working += 1,
                WorkState::Review => p.review += 1,
                WorkState::Blocked => p.blocked += 1,
                WorkState::Dropped | WorkState::Abandoned => p.dropped += 1,
                WorkState::Queued | WorkState::Briefed | WorkState::Paused => p.waiting += 1,
            }
        }
        p
    }

    /// The supervisor's latest reply, the freshest word on where the goal is.
    pub fn latest_supervision(&self) -> Option<(Event, SupervisorReply)> {
        self.thread().into_iter().rev().find_map(|step| match step {
            ThreadStep::Supervisor(e, r) => Some((e, r)),
            _ => None,
        })
    }

    /// The tasks whose sessions are running now.
    pub fn working(&self) -> Vec<&Work> {
        self.tasks
            .iter()
            .filter(|t| self.activity.get(&t.id).and_then(|a| a.status) == Some(SessionStatus::Running))
            .collect()
    }

    /// What waits on the person: the goal's own question, each task's session
    /// question, and each task waiting on them some other way.
    pub fn waiting_on_you(&self) -> usize {
        usize::from(self.asking.is_some())
            + self.tasks.iter().filter(|t| self.activity.get(&t.id).is_some_and(|a| a.asking.is_some())).count()
            + self.waiting_tasks().len()
    }

    /// Tasks waiting on the person with no question open in their session:
    /// what they last asked, or what their report said they need.
    pub fn waiting_tasks(&self) -> Vec<WaitingTask> {
        self.tasks
            .iter()
            .filter(|t| t.state == WorkState::NeedsInput && self.activity.get(&t.id).is_none_or(|a| a.asking.is_none()))
            .filter_map(|t| {
                let question =
                    self.events.iter().rev().filter(|e| e.work == t.id).find_map(|e| match e.kind.as_str() {
                        "answer" | "decision" => Some(None),
                        "question" => {
                            let items = e
                                .asked_items()
                                .into_iter()
                                .map(|i| QuestionItem {
                                    header: String::new(),
                                    question: i.question,
                                    multi: false,
                                    options: i
                                        .choices
                                        .into_iter()
                                        .map(|label| QuestionOption { label, description: String::new() })
                                        .collect(),
                                })
                                .collect();
                            Some(Some(asked(e, items)))
                        }
                        "report" => {
                            let report = self.reports.get(&e.id.to_string());
                            let notes = report.and_then(|r| r.sections.iter().find(|s| s.name == "NOTES"));
                            let said = [report.map(|r| r.said.as_str()), notes.map(|s| s.text.as_str())]
                                .into_iter()
                                .flatten()
                                .find(|s| !s.is_empty())
                                .unwrap_or(&e.body);
                            let item = QuestionItem {
                                header: String::new(),
                                question: said.to_string(),
                                multi: false,
                                options: Vec::new(),
                            };
                            Some(Some(asked(e, vec![item])))
                        }
                        _ => None,
                    })??;
                Some(WaitingTask { task: t.clone(), question })
            })
            .collect()
    }
}

fn asked(e: &Event, items: Vec<QuestionItem>) -> Question {
    Question { id: format!("event-{}", e.id), items, answers: None, reply: None }
}

fn is_update(step: &ThreadStep) -> bool {
    match step {
        ThreadStep::Look(_) => true,
        ThreadStep::Event(e) => ["state", "verify", "pr", "sent"].contains(&e.kind.as_str()),
        _ => false,
    }
}
