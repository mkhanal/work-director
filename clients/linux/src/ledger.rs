//! The ledger as `wd serve --stdio` serves it. The app runs the wd installed
//! with it, so these types track exactly one version of the wire; a value they
//! cannot read is a failure to report, not a shape to guess.

use std::collections::HashMap;

use chrono::{DateTime, Utc};
use serde::Deserialize;

use crate::attach::Attachment;
use crate::conversation::{Activity, Question};
use crate::plan::Plan;
use crate::tree::{Course, Finish, Link, NeedYou, Node, Progress, Reasoning, Superseded};

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
    Superseded,
}

impl WorkState {
    pub const ALL: [WorkState; 12] = [
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
        WorkState::Superseded,
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
        matches!(self.state, WorkState::Done | WorkState::Dropped | WorkState::Abandoned | WorkState::Superseded)
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
    pub progress: Progress,
    /// The open questions in the goal's whole tree.
    pub needs_you: i64,
    pub finish: Option<Finish>,
    /// Where a done goal finished, as a person names it.
    pub finished: Option<String>,
    /// Whether its breakdown waits for the person's approval.
    pub proposed: bool,
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

/// Where the sidebar lists a top goal or a standalone work item.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum SidebarBand {
    /// A question open anywhere under it, or a gate only a person passes.
    NeedsYou,
    Working,
    /// Done.
    Completed,
    /// Dropped, abandoned or superseded: folded away.
    Settled,
}

/// One row of the sidebar.
#[derive(Debug, Clone, PartialEq)]
pub struct SidebarRow {
    pub work: Work,
    pub band: SidebarBand,
    /// Progress, the finish line, or where it finished, and how long ago.
    pub meta: String,
    /// How many questions under it wait on the person.
    pub needs_you: i64,
    /// Whether its breakdown waits for the person's approval.
    pub proposed: bool,
    /// Whether work on it is running now: a task or sub-goal under a goal, or the item itself.
    pub working: bool,
}

/// The sidebar's bands, each most recently active first.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct SidebarBands {
    pub needs_you: Vec<SidebarRow>,
    pub working: Vec<SidebarRow>,
    pub completed: Vec<SidebarRow>,
    pub settled: Vec<SidebarRow>,
}

impl SidebarBands {
    pub fn all(&self) -> impl Iterator<Item = &SidebarRow> {
        self.needs_you.iter().chain(&self.working).chain(&self.completed).chain(&self.settled)
    }
}

fn sidebar_band(work: &Work, band: Band, needs_you: i64) -> SidebarBand {
    match work.state {
        WorkState::Done => SidebarBand::Completed,
        WorkState::Dropped | WorkState::Abandoned | WorkState::Superseded => SidebarBand::Settled,
        _ if needs_you > 0 || Band::ANNOUNCED.contains(&band) => SidebarBand::NeedsYou,
        _ => SidebarBand::Working,
    }
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

    /// Top goals and standalone work, within one product when one is named,
    /// banded by whether they need the person, work, are completed or were
    /// let go, each band most recently active first.
    pub fn bands(&self, project: Option<&str>, now: DateTime<Utc>) -> SidebarBands {
        let mine = |w: &Work| project.is_none_or(|p| w.project == p);
        let ago = |w: &Work| crate::time::ago(&w.updated, now);
        let goals = self.goals.iter().filter(|g| mine(&g.work)).map(|g| {
            let band = sidebar_band(&g.work, g.band, g.needs_you);
            let lead = if band == SidebarBand::Completed {
                Some(g.finished.clone().unwrap_or_else(|| "Done".to_string()))
            } else if g.progress.total > 0 {
                Some(format!("{} of {}", g.progress.done, g.progress.total))
            } else {
                g.finish.map(|f| f.line.name().to_string())
            };
            let meta = match lead {
                Some(lead) if band == SidebarBand::Completed => format!("{lead} · {}", ago(&g.work)),
                Some(lead) => lead,
                None => ago(&g.work),
            };
            SidebarRow {
                work: g.work.clone(),
                band,
                meta,
                needs_you: g.needs_you,
                proposed: g.proposed,
                working: !g.work.at_rest() && g.rollup.counts.get("running").is_some_and(|n| *n > 0),
            }
        });
        let standalone = self.standalone.iter().filter(|s| mine(&s.work)).map(|s| SidebarRow {
            work: s.work.clone(),
            band: sidebar_band(&s.work, s.band, 0),
            meta: format!("{} · {}", s.work.state.look().label, ago(&s.work)),
            needs_you: 0,
            proposed: false,
            working: s.work.state == WorkState::Running,
        });
        let mut rows: Vec<SidebarRow> = goals.chain(standalone).collect();
        rows.sort_by(|a, b| b.work.updated.cmp(&a.work.updated));
        let mut bands = SidebarBands::default();
        for row in rows {
            match row.band {
                SidebarBand::NeedsYou => bands.needs_you.push(row),
                SidebarBand::Working => bands.working.push(row),
                SidebarBand::Completed => bands.completed.push(row),
                SidebarBand::Settled => bands.settled.push(row),
            }
        }
        bands
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
    /// The breadcrumb, top goal first, ending at this goal.
    pub chain: Vec<Link>,
    /// The goal's sub-goals, each with everything under it.
    pub tree: Vec<Node>,
    /// Every work under the goal named by its place in the tree.
    pub paths: HashMap<String, String>,
    pub done_when: Option<String>,
    /// Where the top goal finishes, once decided.
    pub finish: Option<Finish>,
    pub progress: Progress,
    /// What holds the goal back from done; empty once done.
    pub not_done_because: Vec<String>,
    /// Every open question on the goal or under it, in tree order.
    pub needs_you: Vec<NeedYou>,
    pub course: Option<Course>,
    pub superseded: Vec<Superseded>,
    /// Whether the goal's breakdown waits for the person's approval.
    pub proposed: bool,
    /// Why its latest planning broke it down so, when that planning said.
    pub reasoning: Option<Reasoning>,
    /// Every file sent anywhere under the goal, oldest first.
    pub attachments: Vec<Attachment>,
    /// The events holding what the person said that the director has not been
    /// given yet; empty from a wd that does not say.
    #[serde(default)]
    pub waiting_for_director: Vec<i64>,
    /// That the project's own checks fail on main, and the sub-goal that
    /// fixes them; none while they do not, or from a wd that does not say.
    #[serde(default)]
    pub checks_fail_on_main: Option<ChecksOnMain>,
}

/// The concern a top goal holds while the project's own checks fail on main.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct ChecksOnMain {
    pub concern: i64,
    /// The concern, in a sentence.
    pub says: String,
    /// The sub-goal that fixes them, none while one is open.
    pub fix: Option<SubGoalPlan>,
}

/// A sub-goal the person can add: its outcome and what makes it done.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct SubGoalPlan {
    pub outcome: String,
    pub done_when: String,
}

impl GoalDetail {
    /// Whether what event `id` holds still waits for the director to finish
    /// what it is doing.
    pub fn waits_for_director(&self, id: i64) -> bool {
        self.waiting_for_director.contains(&id)
    }

    /// The plan that adds the sub-goal fixing the project's checks under the
    /// top goal, while wd offers one.
    pub fn fix_checks(&self) -> Option<Plan> {
        let fix = self.checks_fail_on_main.as_ref()?.fix.as_ref()?;
        let top = self.chain.first().map_or(self.goal.id.as_str(), |l| l.id.as_str());
        Plan::add_sub_goal(top, &fix.outcome, &fix.done_when).ok()
    }

    /// Whether this goal is a sub-goal of another.
    pub fn nested(&self) -> bool {
        self.chain.len() > 1
    }

    /// The node with this id anywhere in the tree.
    pub fn node(&self, id: &str) -> Option<&Node> {
        self.tree.iter().find_map(|n| n.find(id))
    }
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
    /// The button's words, in plain language, when wd serves them.
    #[serde(default)]
    pub label: Option<String>,
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
