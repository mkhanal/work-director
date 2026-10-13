//! A goal read as one conversation.

use std::collections::{HashMap, HashSet};

use crate::ledger::{Event, GoalDetail};
use crate::plan::DirectorChoice;

/// What a goal's conversation is narrowed to: everything, the person and
/// the director, or one sub-goal and everything under it.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub enum ThreadFilter {
    All,
    YouAndDirector,
    SubGoal(String),
}

/// One filter chip: its filter, its label and how many turns it keeps.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct FilterChip {
    pub filter: ThreadFilter,
    pub label: String,
    pub count: usize,
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
    /// The director declined to answer part of a question put to the person, and why.
    Declined(Event, DirectorChoice),
}

impl ThreadStep {
    pub fn event(&self) -> &Event {
        match self {
            ThreadStep::Event(e)
            | ThreadStep::Look(e)
            | ThreadStep::Supervisor(e, _)
            | ThreadStep::Stopped(e, _)
            | ThreadStep::Exchange(e, _)
            | ThreadStep::Declined(e, _) => e,
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
const DECIDED_PREFIX: &str = "the director decided ";
const DECLINED_PREFIX: &str = "the director declined ";
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
            if e.kind == "sent" && answers.contains(&(e.work.as_str(), e.body.as_str()))
                || e.reshapes()
                || e.director_decided()
            {
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
            "attachment" => (!self.attached_alone(e).is_empty()).then(|| ThreadStep::Event(e.clone())),
            "supervised" => Some(ThreadStep::Supervisor(e.clone(), self.reply(&e.body))),
            "note" => match e.director_declined() {
                Some(declined) => Some(ThreadStep::Declined(e.clone(), declined)),
                None => {
                    e.body.strip_prefix(STOPPED_PREFIX).map(|said| ThreadStep::Stopped(e.clone(), said.to_string()))
                }
            },
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
            "attachment" => true,
            _ => false,
        }
    }

    pub fn title_of(&self, id: &str) -> Option<&str> {
        self.tasks.iter().find(|t| t.id == id).map(|t| t.title.as_str())
    }
}

impl GoalDetail {
    /// Each part the director decided for an answer it gave, with why: its
    /// decisions on the same work after the answer, before the next question.
    pub fn decided_for(&self, answer: &Event) -> Vec<DirectorChoice> {
        let next = self
            .events
            .iter()
            .filter(|e| e.work == answer.work && e.kind == "question" && e.id > answer.id)
            .map(|e| e.id)
            .min()
            .unwrap_or(i64::MAX);
        self.events
            .iter()
            .filter(|e| e.work == answer.work && e.director_decided() && e.id > answer.id && e.id < next)
            .filter_map(|e| {
                let d = e.decision.as_ref()?;
                let mark = format!(": {}, because ", d.answer);
                let why = e.body.split_once(&mark).map_or("", |(_, why)| why);
                Some(DirectorChoice {
                    question: d.question.clone(),
                    answer: Some(d.answer.clone()),
                    why: why.to_string(),
                })
            })
            .collect()
    }
}

impl Event {
    /// Whether it is the director's decision on one part of a question it
    /// answered in the person's place, which is read with that answer.
    pub fn director_decided(&self) -> bool {
        self.kind == "decision"
            && self.decision.as_ref().is_some_and(|d| d.source == "director")
            && self.body.starts_with(DECIDED_PREFIX)
    }

    /// The part of a question the director declined to answer, and why.
    pub fn director_declined(&self) -> Option<DirectorChoice> {
        if self.kind != "note" {
            return None;
        }
        let (question, rest) = go_quoted(self.body.strip_prefix(DECLINED_PREFIX)?)?;
        let why = rest.strip_prefix(": ")?;
        Some(DirectorChoice { question, answer: None, why: why.to_string() })
    }

    /// Whether it is a decision the ledger files when a goal is reshaped or
    /// settled (a sub-goal added, edited, dropped, merged, split or ordered; a
    /// done-when, finish line, approval or classification), which belongs to
    /// the record rather than the conversation.
    pub fn reshapes(&self) -> bool {
        if self.kind != "decision" {
            return false;
        }
        let b = self.body.as_str();
        let verb = ["added ", "edited ", "dropped ", "split "].iter().find_map(|v| b.strip_prefix(v));
        verb.and_then(id_then).is_some_and(|rest| rest.starts_with(": "))
            || b.strip_prefix("merged ").and_then(id_then).is_some_and(|rest| rest.starts_with(" into "))
            || id_then(b).is_some_and(|rest| rest.starts_with(" waits on: "))
            || ["done when: ", "finishes at: ", "approved: ", "classified "].iter().any(|p| b.starts_with(p))
    }

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
        for step in self.thread().into_iter().filter(|s| self.shows(s, &filter)) {
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

    fn shows(&self, step: &ThreadStep, filter: &ThreadFilter) -> bool {
        let e = step.event();
        match filter {
            ThreadFilter::All => true,
            ThreadFilter::YouAndDirector => match step {
                ThreadStep::Exchange(q, a) => q.work == self.goal.id || a.as_ref().is_some_and(|a| self.is_yours(a)),
                ThreadStep::Event(e) => self.is_yours(e) || (e.work == self.goal.id && e.kind == "decision"),
                ThreadStep::Supervisor(..) | ThreadStep::Declined(..) => true,
                _ => false,
            },
            ThreadFilter::SubGoal(id) => self.node(id).is_some_and(|n| n.holds(&e.work)),
        }
    }

    /// The chips the conversation is narrowed with: everything, the person
    /// and the director, and one per sub-goal, each with how many turns it keeps.
    pub fn filters(&self) -> Vec<FilterChip> {
        let count = |f: &ThreadFilter| self.thread().iter().filter(|s| self.shows(s, f)).count();
        let mut chips = vec![
            FilterChip { filter: ThreadFilter::All, label: "Everything".into(), count: self.thread().len() },
            FilterChip {
                filter: ThreadFilter::YouAndDirector,
                label: "You and the director".into(),
                count: count(&ThreadFilter::YouAndDirector),
            },
        ];
        for n in self.tree.iter().filter(|n| !n.group) {
            let filter = ThreadFilter::SubGoal(n.work.id.clone());
            chips.push(FilterChip { count: count(&filter), filter, label: n.work.title.clone() });
        }
        chips
    }

    /// The sub-goal a turn came from, as its path through the tree; none for
    /// the goal itself.
    pub fn tag(&self, work: &str) -> Option<&str> {
        self.paths.get(work).map(String::as_str)
    }

    /// A sub-goal finishing, as its event line reads: where, done, how it was
    /// judged. None for any other event.
    pub fn finished_line(&self, e: &Event) -> Option<String> {
        if e.kind != "state" || e.body != "done" || e.work == self.goal.id {
            return None;
        }
        let path = self.tag(&e.work)?;
        let mut parts = vec![path.to_string(), "done".to_string()];
        if let Some(j) = self.node(&e.work).and_then(|n| n.judged.as_ref()) {
            parts.push(crate::words::checked(&j.verdict).to_string());
        }
        Some(parts.join(" · "))
    }

    /// Who a message can be sent to: the whole goal, or any sub-goal in its
    /// tree, by path.
    pub fn addressees(&self) -> Vec<(String, String)> {
        let mut out = vec![(self.goal.id.clone(), "whole goal".to_string())];
        fn walk(nodes: &[crate::tree::Node], paths: &HashMap<String, String>, out: &mut Vec<(String, String)>) {
            for n in nodes {
                if !n.group {
                    let path = paths.get(&n.work.id).cloned().unwrap_or_else(|| n.work.title.clone());
                    out.push((n.work.id.clone(), path));
                }
                walk(&n.children, paths, out);
            }
        }
        walk(&self.tree, &self.paths, &mut out);
        out
    }
}

/// What follows a leading work id (lower-case hex), or none without one.
fn id_then(text: &str) -> Option<&str> {
    let end = text.find(|c: char| !matches!(c, '0'..='9' | 'a'..='f')).unwrap_or(text.len());
    (end > 0).then(|| &text[end..])
}

fn is_update(step: &ThreadStep) -> bool {
    match step {
        ThreadStep::Look(_) => true,
        ThreadStep::Event(e) => ["state", "verify", "pr", "sent"].contains(&e.kind.as_str()),
        _ => false,
    }
}

/// A Go-quoted string at the start of text, and what follows it.
fn go_quoted(text: &str) -> Option<(String, &str)> {
    let rest = text.strip_prefix('"')?;
    let mut out = String::new();
    let mut chars = rest.char_indices();
    while let Some((i, ch)) = chars.next() {
        match ch {
            '"' => return Some((out, &rest[i + 1..])),
            '\\' => match chars.next()?.1 {
                'n' => out.push('\n'),
                't' => out.push('\t'),
                other => out.push(other),
            },
            other => out.push(other),
        }
    }
    None
}
