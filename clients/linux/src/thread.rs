//! A goal read as one conversation.

use std::collections::{HashMap, HashSet};

use crate::ledger::{Event, GoalDetail};

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
        self.events
            .iter()
            .any(|e| e.work == self.goal.id && e.kind == "spawn" && e.body.starts_with(SUPERVISE_PREFIX))
    }

    /// The goal as one conversation, oldest first: each answer read with the
    /// question it answers.
    pub fn thread(&self) -> Vec<ThreadStep> {
        let mut steps: Vec<ThreadStep> = Vec::new();
        let mut open: HashMap<&str, usize> = HashMap::new();
        // An answer reaches its session as a message too; it is read once, as the answer.
        let answers: HashSet<(&str, &str)> = self
            .events
            .iter()
            .filter(|e| e.kind == "answer")
            .map(|e| (e.work.as_str(), e.body.as_str()))
            .collect();
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
            "note" => e
                .body
                .strip_prefix(STOPPED_PREFIX)
                .map(|said| ThreadStep::Stopped(e.clone(), said.to_string())),
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
