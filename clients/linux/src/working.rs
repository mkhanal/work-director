//! Work under way, as a person watches it: each session working under a goal
//! and each write the app started and waits on, as who is working, what it is
//! doing now in a line, and since when.

use chrono::{DateTime, Utc};

use crate::agents::span;
use crate::conversation::{Activity, SessionStatus, StepKind};
use crate::ledger::GoalDetail;
use crate::plan::Plan;
use crate::time::instant;

/// A write `wd` takes long enough over that the person sees it under way.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Write {
    /// A message on its way to work.
    Send,
    /// A goal being planned, again or for the first time.
    Plan,
    /// A question left to the director.
    Decide,
    /// The person's answer to a question.
    Answer,
    /// A proposed breakdown approved and started.
    Approve,
}

impl Write {
    /// The write a plan makes, and the work it is on, when it is one a person waits on.
    pub fn of(plan: &Plan) -> Option<(Write, String)> {
        let argv: Vec<&str> = plan.argv.iter().map(String::as_str).collect();
        match argv.as_slice() {
            ["send", id, ..] => Some((Write::Send, id.to_string())),
            ["goal", "plan" | "continue", id, ..] => Some((Write::Plan, id.to_string())),
            ["answer", id, rest @ ..] if rest.contains(&"--director") => Some((Write::Decide, id.to_string())),
            ["answer", id, ..] => Some((Write::Answer, id.to_string())),
            ["goal", "approve", id] => Some((Write::Approve, id.to_string())),
            _ => None,
        }
    }

    pub fn doing(self) -> &'static str {
        match self {
            Write::Send => "Sending your message",
            Write::Plan => "Planning",
            Write::Decide => "Deciding for you",
            Write::Answer => "Sending your answer",
            Write::Approve => "Starting the breakdown",
        }
    }
}

/// A write the app started and has not heard back from.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct InFlight {
    pub id: u64,
    pub write: Write,
    pub work: String,
    pub at: DateTime<Utc>,
}

/// One working row: who, what it is doing now, and since when.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct WorkingRow {
    /// The work it is on, or the write's own key.
    pub key: String,
    pub who: String,
    pub doing: String,
    pub since: Option<DateTime<Utc>>,
}

impl WorkingRow {
    /// How long it has been at it, as a person reads it: 45s, 2m 5s.
    pub fn elapsed(&self, now: DateTime<Utc>) -> Option<String> {
        self.since.map(|since| span((now - since).num_seconds().max(0)))
    }
}

/// Work under way under a goal: the newest rows shown, and how many more there are.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct WorkUnderWay {
    pub rows: Vec<WorkingRow>,
    pub more: usize,
}

/// How many rows show before the rest are counted.
pub const SHOWN: usize = 3;

/// What a session is doing now, in a line, from its latest step.
pub fn doing(kind: Option<StepKind>, now: Option<&str>) -> String {
    let now = now.unwrap_or("").trim();
    if kind != Some(StepKind::Tool) || now.is_empty() {
        return "Thinking".to_string();
    }
    let (name, what) = now.split_once(" · ").map_or((now, ""), |(n, w)| (n.trim(), w.trim()));
    let with = |verb: &str| if what.is_empty() { verb.to_string() } else { format!("{verb} {what}") };
    match name.to_lowercase().as_str() {
        "bash" | "shell" | "exec" | "exec_command" | "local_shell" => {
            if what.is_empty() {
                "Running a command".to_string()
            } else {
                format!("Running `{what}`")
            }
        }
        "read" | "view" | "webfetch" | "web_fetch" => with("Reading"),
        "edit" | "write" | "multiedit" | "notebookedit" | "apply_patch" | "patch" => with("Editing"),
        "grep" | "glob" | "search" | "find" | "ls" => with("Searching"),
        "websearch" | "web_search" => with("Searching the web for"),
        "task" | "agent" => with("Starting an agent:"),
        "todowrite" | "update_plan" => "Updating its plan".to_string(),
        _ => with(&format!("Using {name}:")).trim_end_matches(':').to_string(),
    }
}

fn running(a: &Activity) -> bool {
    a.status == Some(SessionStatus::Running)
}

impl GoalDetail {
    /// Whether a session is working on this work or anywhere under it.
    pub fn under_way(&self, id: &str) -> bool {
        let working = |id: &str| self.activity.get(id).is_some_and(running);
        match self.node(id) {
            Some(node) => node_working(node, &working),
            None => working(id),
        }
    }

    /// Each session working under the goal, its own named Director, and each
    /// write on the goal or under it still in flight, a message by the work it
    /// goes to and any other by the Director, newest first.
    pub fn work_under_way(&self, writes: &[InFlight]) -> WorkUnderWay {
        let who = |id: &str| -> String {
            if id == self.goal.id {
                return "Director".to_string();
            }
            self.tasks
                .iter()
                .find(|t| t.id == id)
                .map(|t| t.title.clone())
                .or_else(|| self.node(id).map(|n| n.work.title.clone()))
                .unwrap_or_else(|| id.to_string())
        };
        let sessions = self.activity.iter().filter(|(_, a)| running(a)).map(|(id, a)| WorkingRow {
            key: id.clone(),
            who: who(id),
            doing: doing(a.kind, a.now.as_deref()),
            since: a.at.as_deref().or_else(|| a.usage.as_ref().and_then(|u| u.last.as_deref())).and_then(instant),
        });
        let mine = |w: &&InFlight| {
            w.work == self.goal.id || self.tasks.iter().any(|t| t.id == w.work) || self.node(&w.work).is_some()
        };
        let written = writes.iter().filter(mine).map(|w| WorkingRow {
            key: format!("write-{}", w.id),
            who: if matches!(w.write, Write::Send | Write::Answer) { who(&w.work) } else { "Director".to_string() },
            doing: w.write.doing().to_string(),
            since: Some(w.at),
        });
        let mut rows: Vec<WorkingRow> = sessions.chain(written).collect();
        rows.sort_by(|a, b| b.since.cmp(&a.since).then_with(|| a.key.cmp(&b.key)));
        let more = rows.len().saturating_sub(SHOWN);
        rows.truncate(SHOWN);
        WorkUnderWay { rows, more }
    }
}

fn node_working(node: &crate::tree::Node, working: &impl Fn(&str) -> bool) -> bool {
    working(&node.work.id) || node.children.iter().any(|c| node_working(c, working))
}
