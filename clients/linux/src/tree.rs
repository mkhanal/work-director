//! A goal as wd serves it at any depth: its breadcrumb, what done means for
//! it, what holds it back, what waits on the person, and its sub-goals as a
//! tree. Computed in Go; read here, never derived.

use serde::Deserialize;

use crate::conversation::Question;
use crate::ledger::{Work, WorkState};

/// One level of a goal's breadcrumb.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Link {
    pub id: String,
    pub title: String,
    pub state: WorkState,
}

/// Direct sub-goals done of total; superseded ones are out of the total and
/// counted apart.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default, Deserialize)]
pub struct Progress {
    pub done: i64,
    pub total: i64,
    pub superseded: i64,
}

/// The agent working a sub-goal.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Owner {
    pub runner: String,
    pub agent: Option<String>,
}

/// The latest judgement of a sub-goal's done-when.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Judgement {
    pub verdict: String,
    pub why: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum StepState {
    Todo,
    Doing,
    Done,
    Dropped,
}

/// One step of an agent's own plan for its sub-goal.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct PlanStep {
    pub n: i64,
    pub text: String,
    pub state: StepState,
}

/// One sub-goal in a goal's tree. A group is an older plan's tasks sharing a
/// heading, gathered under a goal row that is not in the ledger.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Node {
    pub work: Work,
    pub group: bool,
    pub done_when: Option<String>,
    pub after: Vec<String>,
    pub waiting_on: Vec<String>,
    pub why_not_moving: Option<String>,
    pub owner: Option<Owner>,
    pub planned_into: Option<i64>,
    pub progress: Option<Progress>,
    pub doing: Option<String>,
    pub steps: Vec<PlanStep>,
    pub judged: Option<Judgement>,
    pub needs_you: i64,
    pub children: Vec<Node>,
}

impl Node {
    /// The goal page this node opens: a goal in the ledger does; a task and
    /// a group do not.
    pub fn opens(&self) -> Option<&str> {
        (self.work.kind.is_goal() && !self.group).then_some(self.work.id.as_str())
    }

    /// The node with this id, here or anywhere below.
    pub fn find(&self, id: &str) -> Option<&Node> {
        if self.work.id == id {
            return Some(self);
        }
        self.children.iter().find_map(|c| c.find(id))
    }

    /// Whether work with this id is this node or under it.
    pub fn holds(&self, id: &str) -> bool {
        self.find(id).is_some()
    }
}

/// What a goal hands back when it is done.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
pub enum FinishLine {
    #[serde(rename = "discovery")]
    Discovery,
    #[serde(rename = "research")]
    Research,
    #[serde(rename = "local build")]
    LocalBuild,
    #[serde(rename = "complete PR")]
    CompletePr,
    #[serde(rename = "merged")]
    Merged,
    #[serde(rename = "released")]
    Released,
}

impl FinishLine {
    /// The finish line as a card names it.
    pub fn title(self) -> &'static str {
        match self {
            FinishLine::Discovery => "Discovery",
            FinishLine::Research => "Research",
            FinishLine::LocalBuild => "A local build",
            FinishLine::CompletePr => "A complete PR",
            FinishLine::Merged => "Merged",
            FinishLine::Released => "Released",
        }
    }

    /// The finish line in a word or two, as the sidebar shows it.
    pub fn name(self) -> &'static str {
        match self {
            FinishLine::Discovery => "Discovery",
            FinishLine::Research => "Research",
            FinishLine::LocalBuild => "Local build",
            FinishLine::CompletePr => "Complete PR",
            FinishLine::Merged => "Merged",
            FinishLine::Released => "Released",
        }
    }

    /// What it hands back, in a sentence's words.
    pub fn means(self) -> &'static str {
        match self {
            FinishLine::Discovery => "a findings report, no code changes",
            FinishLine::Research => "a written comparison and a pick, no code changes",
            FinishLine::LocalBuild => "built and runnable, with how to run it",
            FinishLine::CompletePr => "a PR open with checks green and the done-when met",
            FinishLine::Merged => "the PR merged",
            FinishLine::Released => "released",
        }
    }
}

/// Where a finish line came from.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum FinishSource {
    Goal,
    Repo,
    Taste,
    Person,
}

impl FinishSource {
    pub fn sentence(self) -> &'static str {
        match self {
            FinishSource::Goal => "From what the goal asks",
            FinishSource::Repo => "From how this repo finishes goals",
            FinishSource::Taste => "From your taste",
            FinishSource::Person => "You chose it",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
pub struct Finish {
    pub line: FinishLine,
    pub source: FinishSource,
}

/// A question on work in the tree that waits on the person.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct NeedYou {
    pub work: String,
    pub path: String,
    pub question: Question,
}

/// A supervisor's latest verdict on whether its goal is on course.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Course {
    pub verdict: String,
    pub why: String,
    pub at: String,
}

/// A branch a course change let go of.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Superseded {
    pub work: String,
    pub title: String,
    pub why: String,
    pub at: String,
}
