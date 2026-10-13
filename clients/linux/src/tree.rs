//! A goal as wd serves it at any depth: its breadcrumb, what done means for
//! it, what holds it back, what waits on the person, and its sub-goals as a
//! tree. Computed in Go; read here, never derived.

use serde::Deserialize;

use crate::conversation::Question;
use crate::ledger::{Work, WorkState};
use crate::look::{Mark, Tone};

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
    /// Every finish line, nearest first.
    pub const ALL: [FinishLine; 6] = [
        FinishLine::Discovery,
        FinishLine::Research,
        FinishLine::LocalBuild,
        FinishLine::CompletePr,
        FinishLine::Merged,
        FinishLine::Released,
    ];

    /// What the menu that picks a goal's finish line is announced as.
    pub fn menu_name(current: Option<FinishLine>) -> String {
        format!("Finishes at: {}", current.map_or("Not decided yet", FinishLine::title))
    }

    /// The finish line as wd reads and writes it.
    pub fn wire(self) -> &'static str {
        match self {
            FinishLine::Discovery => "discovery",
            FinishLine::Research => "research",
            FinishLine::LocalBuild => "local build",
            FinishLine::CompletePr => "complete PR",
            FinishLine::Merged => "merged",
            FinishLine::Released => "released",
        }
    }

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

    /// The finish line a choice's words name.
    pub fn from_label(label: &str) -> Option<FinishLine> {
        FinishLine::ALL.into_iter().find(|f| f.name().eq_ignore_ascii_case(label.trim()))
    }

    /// The finish line as a choice reads it: its title and what it hands back.
    pub fn choice(self) -> (&'static str, &'static str) {
        match self {
            FinishLine::Discovery => {
                ("Discovery: the cause found and explained", "A findings report with its proof. No code changes.")
            }
            FinishLine::Research => (
                "Research complete: a written comparison and a pick",
                "Trade-offs and a recommendation with its reasons. No code changes.",
            ),
            FinishLine::LocalBuild => ("A local build you can try", "The work on a branch, with how to run it."),
            FinishLine::CompletePr => ("A complete PR", "The change with its tests, ready to merge."),
            FinishLine::Merged => ("Merged", "The pull request merged into the main branch."),
            FinishLine::Released => ("Released", "Merged and published as a release."),
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

/// A question on work in the tree that waits on the person, and what
/// answering it with its first choice would change.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct NeedYou {
    pub work: String,
    pub path: String,
    pub question: Question,
    pub change: Option<GoalChange>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ChangeKind {
    /// The plan under a goal changes.
    Plan,
    /// The top goal itself changes: its title, done-when or finish line.
    Course,
    /// Where the goal finishes is chosen.
    Finish,
}

/// A goal's title, done-when and finish line.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Shape {
    pub title: String,
    pub done_when: Option<String>,
    pub finish: Option<FinishLine>,
}

/// What a question would change: a plan change's actions and the same in
/// sentences, the top goal now and as proposed and why, or the finish lines
/// offered.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct GoalChange {
    pub kind: ChangeKind,
    #[serde(default)]
    pub actions: Vec<String>,
    #[serde(default)]
    pub lines: Vec<String>,
    pub now: Option<Shape>,
    pub proposed: Option<Shape>,
    #[serde(default)]
    pub why: String,
    #[serde(default)]
    pub choices: Vec<String>,
    /// What each source of a finish line said; a finish-line question's only.
    #[serde(default)]
    pub sources: Option<FinishSaid>,
    /// What was found that calls for a plan or course change.
    #[serde(default)]
    pub found: Option<String>,
    /// Each sub-goal as a plan or course change would leave it.
    #[serde(default)]
    pub sub_goals: Vec<SubGoalFate>,
}

/// What the goal, the repo and taste each said of where a goal finishes; a
/// source that said nothing is none.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct FinishSaid {
    pub goal: Option<String>,
    pub repo: Option<String>,
    pub taste: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Fate {
    Kept,
    New,
    Superseded,
}

/// One sub-goal as a change would leave it: an existing one kept or
/// superseded with its state, or a new one with no work yet.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct SubGoalFate {
    pub work: Option<String>,
    pub title: String,
    pub status: Fate,
    pub state: Option<WorkState>,
    pub why: Option<String>,
}

impl SubGoalFate {
    /// What the change does to it, with its state when it is kept.
    pub fn badge(&self) -> (String, Tone) {
        match (self.status, self.state) {
            (Fate::New, _) => ("New".to_string(), Tone::Live),
            (Fate::Superseded, _) => ("Superseded".to_string(), Tone::Waiting),
            (Fate::Kept, Some(state)) if state.mark() == Mark::Done => ("Kept · done".to_string(), Tone::Settled),
            (Fate::Kept, None | Some(WorkState::Queued)) => ("Kept · not started".to_string(), Tone::Waiting),
            (Fate::Kept, Some(_)) => ("Kept · working".to_string(), Tone::Live),
        }
    }

    /// Superseded: struck through, with why.
    pub fn struck(&self) -> bool {
        self.status == Fate::Superseded
    }
}

/// Why a goal's latest planning broke it down so; a part the planner did not
/// give is none.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Reasoning {
    pub why: Option<String>,
    pub left_out: Option<String>,
    pub judge_by: Option<String>,
}

impl Reasoning {
    /// Each part given, headed as the proposal's panel heads it.
    pub fn parts(&self) -> Vec<(&'static str, &str)> {
        [("Why this breakdown", &self.why), ("Left out", &self.left_out), ("How you'd judge it", &self.judge_by)]
            .into_iter()
            .filter_map(|(head, said)| said.as_deref().filter(|s| !s.trim().is_empty()).map(|s| (head, s)))
            .collect()
    }
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
