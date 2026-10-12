//! The ledger as `wd serve --stdio` serves it. The app runs the wd installed
//! with it, so these types track exactly one version of the wire; a value they
//! cannot read is a failure to report, not a shape to guess.

use std::collections::HashMap;

use serde::Deserialize;

use crate::conversation::{Activity, Question};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum WorkState {
    Queued,
    Briefed,
    Running,
    NeedsInput,
    Review,
    SoftDone,
    Done,
    Blocked,
    Dropped,
    Abandoned,
    Paused,
}

impl WorkState {
    pub const ALL: [WorkState; 11] = [
        WorkState::Queued,
        WorkState::Briefed,
        WorkState::Running,
        WorkState::NeedsInput,
        WorkState::Review,
        WorkState::SoftDone,
        WorkState::Done,
        WorkState::Blocked,
        WorkState::Dropped,
        WorkState::Abandoned,
        WorkState::Paused,
    ];
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum WorkKind {
    Task,
    Evolution,
    Workflow,
    Goal,
    Epic,
    Roadmap,
    Item,
}

impl WorkKind {
    pub fn is_goal(self) -> bool {
        matches!(self, WorkKind::Goal | WorkKind::Epic)
    }

    pub fn name(self) -> &'static str {
        match self {
            WorkKind::Task => "task",
            WorkKind::Evolution => "evolution",
            WorkKind::Workflow => "workflow",
            WorkKind::Goal => "goal",
            WorkKind::Epic => "epic",
            WorkKind::Roadmap => "roadmap",
            WorkKind::Item => "item",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum GoalType {
    Query,
    Build,
    Fix,
    Change,
    Review,
}

/// Where an item sits on the board: what it is waiting on.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum Band {
    NeedsYou,
    InFlight,
    Paused,
    ReadyToPush,
    InReview,
    ReadyToClose,
    AtRest,
}

impl Band {
    pub const ALL: [Band; 7] = [
        Band::NeedsYou,
        Band::InFlight,
        Band::Paused,
        Band::ReadyToPush,
        Band::InReview,
        Band::ReadyToClose,
        Band::AtRest,
    ];

    /// The bands a person is told about when work enters them.
    pub const ANNOUNCED: [Band; 3] = [Band::NeedsYou, Band::ReadyToPush, Band::ReadyToClose];
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Work {
    pub id: String,
    pub project: String,
    pub title: String,
    pub detail: String,
    pub kind: WorkKind,
    pub state: WorkState,
    pub runner: Option<String>,
    pub session: Option<String>,
    #[serde(rename = "ref")]
    pub reference: Option<String>,
    pub cwd: Option<String>,
    pub created: String,
    pub updated: String,
    pub parent: Option<String>,
    pub heading: Option<String>,
    pub claim: Option<String>,
    pub impact: Option<String>,
    pub goal_type: Option<GoalType>,
    /// When the work was put out of sight; none while it is on the board.
    pub archived: Option<String>,
}

impl Work {
    /// Work a person can still pause, tell or cancel.
    pub fn live(&self) -> bool {
        matches!(
            self.state,
            WorkState::Running
                | WorkState::NeedsInput
                | WorkState::Review
                | WorkState::Blocked
                | WorkState::Queued
                | WorkState::Briefed
        )
    }

    pub fn at_rest(&self) -> bool {
        matches!(self.state, WorkState::Done | WorkState::Dropped | WorkState::Abandoned)
    }

    /// Whether the work has a session whose conversation can be read.
    pub fn has_session(&self) -> bool {
        self.session.is_some() || self.claim.is_some()
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Decision {
    pub question: String,
    pub answer: String,
    pub source: String,
    pub runner: String,
    pub model: String,
    pub tokens: i64,
    pub reverses: i64,
    pub reversed_by: i64,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Event {
    pub id: i64,
    pub work: String,
    pub kind: String,
    pub body: String,
    pub at: String,
    pub effective: Option<String>,
    pub decision: Option<Decision>,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct Rollup {
    pub counts: HashMap<String, i64>,
    pub done: i64,
    pub total: i64,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct BoardGoal {
    pub work: Work,
    pub rollup: Rollup,
    pub band: Band,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct BoardItem {
    pub work: Work,
    pub band: Band,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct Board {
    pub goals: Vec<BoardGoal>,
    pub standalone: Vec<BoardItem>,
}

/// The sidebar's three lists.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct Threads {
    pub open: Vec<BoardThread>,
    pub queued: Vec<BoardThread>,
    pub settled: Vec<BoardThread>,
}

impl Threads {
    pub fn all(&self) -> impl Iterator<Item = &BoardThread> {
        self.open.iter().chain(&self.queued).chain(&self.settled)
    }
}

/// One goal or standalone item as the sidebar lists it.
#[derive(Debug, Clone, PartialEq)]
pub struct BoardThread {
    pub work: Work,
    pub band: Band,
    pub rollup: Option<Rollup>,
}

/// Work that has just come to wait on a person: a question or a block, or a
/// gate only a person passes.
#[derive(Debug, Clone, PartialEq)]
pub struct Arrival {
    pub work: Work,
    pub band: Band,
}

impl Board {
    /// Every item in one band, goals first, within one product when one is named.
    pub fn items(&self, band: Band, project: Option<&str>) -> Vec<&Work> {
        let mine = |w: &Work| project.is_none_or(|p| w.project == p);
        self.goals
            .iter()
            .filter(|g| g.band == band && mine(&g.work))
            .map(|g| &g.work)
            .chain(self.standalone.iter().filter(|s| s.band == band && mine(&s.work)).map(|s| &s.work))
            .collect()
    }

    /// Goals on the board, most recently active first, within one product when one is named.
    pub fn recent_goals(&self, project: Option<&str>) -> Vec<&Work> {
        let mut goals: Vec<&Work> =
            self.goals.iter().map(|g| &g.work).filter(|w| project.is_none_or(|p| w.project == p)).collect();
        goals.sort_by(|a, b| b.updated.cmp(&a.updated));
        goals
    }

    pub fn rollup_of(&self, id: &str) -> Option<&Rollup> {
        self.goals.iter().find(|g| g.work.id == id).map(|g| &g.rollup)
    }

    /// The board as threads: every goal and open standalone item within one
    /// product when one is named, in one band when one is named, most recently
    /// active first. Work not started yet is set apart as queued, and work at
    /// rest as settled.
    pub fn threads(&self, project: Option<&str>, band: Option<Band>) -> Threads {
        let mut shown: Vec<BoardThread> = self
            .goals
            .iter()
            .map(|g| BoardThread { work: g.work.clone(), band: g.band, rollup: Some(g.rollup.clone()) })
            .chain(self.standalone.iter().map(|s| BoardThread { work: s.work.clone(), band: s.band, rollup: None }))
            .filter(|t| project.is_none_or(|p| t.work.project == p))
            .filter(|t| band.is_none_or(|b| t.band == b))
            .collect();
        shown.sort_by(|a, b| b.work.updated.cmp(&a.work.updated));
        let waiting = |t: &BoardThread| matches!(t.work.state, WorkState::Queued | WorkState::Briefed);
        let mut threads = Threads::default();
        for t in shown {
            if t.band == Band::AtRest {
                threads.settled.push(t);
            } else if waiting(&t) {
                threads.queued.push(t);
            } else {
                threads.open.push(t);
            }
        }
        threads
    }

    /// Work that entered an announced band since `earlier`. With no earlier
    /// board there is nothing to compare, so nothing has arrived.
    pub fn arrivals(&self, earlier: Option<&Board>) -> Vec<Arrival> {
        let Some(earlier) = earlier else { return Vec::new() };
        let before_threads = earlier.threads(None, None);
        let mut before: HashMap<&str, Band> = HashMap::new();
        for t in before_threads.all() {
            before.entry(t.work.id.as_str()).or_insert(t.band);
        }
        self.threads(None, None)
            .all()
            .filter(|t| Band::ANNOUNCED.contains(&t.band) && before.get(t.work.id.as_str()) != Some(&t.band))
            .map(|t| Arrival { work: t.work.clone(), band: t.band })
            .collect()
    }
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct Claim {
    pub event: Event,
    pub work: Option<Work>,
    pub stands: bool,
    pub reversed: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize)]
pub enum LandingKind {
    #[serde(rename = "commit")]
    Commit,
    #[serde(rename = "pull-request")]
    PullRequest,
    #[serde(rename = "")]
    Unknown,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct Landed {
    pub work: Option<Work>,
    pub kind: LandingKind,
    pub url: String,
    pub at: String,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct Delivery {
    pub status: String,
    pub detail: String,
    pub attention: Option<Vec<String>>,
    /// What could not be read; the status says only what could.
    pub error: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct GoalDetail {
    pub goal: Work,
    pub tasks: Vec<Work>,
    pub rollup: Rollup,
    pub events: Vec<Event>,
    pub claims: Vec<Claim>,
    pub landings: Vec<Landed>,
    pub delivery: Option<Delivery>,
    /// What each task still working is doing now, by task id.
    pub activity: HashMap<String, Activity>,
    /// The question the goal waits on the person for, with its choices.
    pub asking: Option<Question>,
    /// Each report read into its parts, by the id of the event that filed it.
    pub reports: HashMap<String, Report>,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct ActionResult {
    pub code: i32,
    pub stdout: String,
    pub stderr: String,
}

impl ActionResult {
    /// What wd said about a failure: its stderr, or its stdout when stderr is empty.
    pub fn complaint(&self) -> &str {
        if self.stderr.trim().is_empty() {
            self.stdout.trim()
        } else {
            self.stderr.trim()
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct Project {
    pub name: String,
    pub path: String,
    pub runner: String,
    pub mode: String,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct Unshipped {
    pub work: Work,
    pub reason: String,
    pub at: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct PromotedCard {
    pub card: String,
    pub text: String,
    pub project: Option<String>,
    pub source: String,
    pub at: String,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct ReviewPass {
    pub project: String,
    pub since: i64,
    pub through: i64,
    pub claims: Vec<Claim>,
    pub cards: Vec<PromotedCard>,
    pub unshipped: Vec<Unshipped>,
    pub landed: Vec<Landed>,
    pub reversals: i64,
}

impl ReviewPass {
    pub fn is_empty(&self) -> bool {
        self.claims.is_empty() && self.cards.is_empty() && self.unshipped.is_empty() && self.landed.is_empty()
    }
}

/// A goal just started: filed, and planned in the background.
#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct Started {
    pub goal: Work,
    pub tasks: Vec<Work>,
}

/// A goal a request may continue, and why the model thinks so.
#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct Found {
    pub goal: Work,
    pub why: String,
}

/// The next gate on work's way to done, as wd works it out: verify, land,
/// soft-done or done, with the argv that passes it. Empty when there is none.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct Gate {
    pub work: String,
    pub gate: String,
    pub title: Option<String>,
    pub argv: Vec<String>,
    pub why: Option<String>,
}

/// What `wd doctor` says the host offers: which runners are on PATH.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct Doctor {
    pub runners: Vec<DoctorRunner>,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct DoctorRunner {
    pub runner: String,
    pub detected: bool,
}

/// The taste every brief carries, and what feedback has gathered toward new
/// rules, as `wd taste` gives it.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct Taste {
    pub cards: Vec<TasteCard>,
    pub learned: Vec<Learned>,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct TasteCard {
    pub id: String,
    pub title: String,
    pub category: String,
    pub scope: Vec<String>,
    pub statement: String,
    pub always: bool,
    /// built-in, a checkout's cards directory, or yours.
    pub source: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct Learned {
    pub key: String,
    pub count: i64,
    pub texts: Vec<String>,
}

/// A task's report read into its parts, as wd serves it.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Report {
    /// `done`, `blocked`, `needs-input`, or empty when the report gave none.
    pub verdict: String,
    /// What the report said with its verdict.
    pub said: String,
    pub files: Vec<String>,
    /// What the report said about its files beyond naming them.
    pub file_note: String,
    pub sections: Vec<ReportSection>,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct ReportSection {
    pub name: String,
    pub text: String,
}
