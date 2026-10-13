//! What to tell the person outside the window: what happened, to which goal,
//! in plain words, worked out from two reads in a row.

use std::collections::{HashMap, HashSet};

use chrono::{DateTime, Duration, Utc};

use crate::ledger::{Band, Board, BoardGoal, BoardItem, GoalDetail, WorkState};
use crate::plan::Plan;
use crate::tree::{ChangeKind, Node};

/// The board and, for each goal still moving or asking, its goal view: what
/// two reads in a row are compared on.
#[derive(Debug, Clone, PartialEq)]
pub struct Sighting {
    pub board: Board,
    /// Goal views by top goal id; a goal at rest with nothing asked has none.
    pub goals: HashMap<String, GoalDetail>,
}

/// Which goals' views to read for a board, and the views kept from before.
pub struct Reads {
    pub read: Vec<String>,
    pub kept: HashMap<String, GoalDetail>,
}

impl Sighting {
    /// Which goals' views to read for `board`, and the views the earlier
    /// sighting already holds that nothing has changed since. A goal is read
    /// again when its board row changed or an event touched work under it.
    pub fn reads(board: &Board, earlier: Option<&Sighting>, touched: &HashSet<String>) -> Reads {
        let mut reads = Reads { read: Vec::new(), kept: HashMap::new() };
        for g in board.goals.iter().filter(|g| !g.work.at_rest() || g.needs_you > 0 || g.proposed) {
            let id = &g.work.id;
            let old = earlier.and_then(|e| {
                let same = e.board.goals.iter().find(|o| &o.work.id == id) == Some(g);
                e.goals.get(id).filter(|d| same && under(d).is_disjoint(touched))
            });
            match old {
                Some(d) => {
                    reads.kept.insert(id.clone(), d.clone());
                }
                None => reads.read.push(id.clone()),
            }
        }
        reads
    }

    fn row(&self, id: &str) -> Option<&BoardGoal> {
        self.board.goals.iter().find(|g| g.work.id == id)
    }

    /// The question ids open under a goal; none when it had questions this
    /// sighting did not read, so none can be called new.
    fn known_questions(&self, id: &str) -> Option<HashSet<String>> {
        if let Some(d) = self.goals.get(id) {
            return Some(d.needs_you.iter().map(|n| n.question.id.clone()).collect());
        }
        match self.row(id) {
            Some(g) if g.needs_you > 0 => None,
            _ => Some(HashSet::new()),
        }
    }

    /// The work blocked under a goal; none when it may have had some this
    /// sighting did not read.
    fn known_blocked(&self, id: &str) -> Option<HashSet<String>> {
        if let Some(d) = self.goals.get(id) {
            return Some(
                nodes(&d.tree)
                    .into_iter()
                    .filter(|n| n.work.state == WorkState::Blocked)
                    .map(|n| n.work.id.clone())
                    .collect(),
            );
        }
        match self.row(id) {
            Some(g) if g.rollup.counts.get("blocked").copied().unwrap_or(0) > 0 || !g.work.at_rest() => None,
            _ => Some(HashSet::new()),
        }
    }
}

/// The goal and every work under it.
fn under(d: &GoalDetail) -> HashSet<String> {
    std::iter::once(d.goal.id.clone())
        .chain(d.tasks.iter().map(|t| t.id.clone()))
        .chain(d.paths.keys().cloned())
        .chain(nodes(&d.tree).into_iter().map(|n| n.work.id.clone()))
        .collect()
}

/// Every node of a tree, at every depth.
fn nodes(tree: &[Node]) -> Vec<&Node> {
    tree.iter().flat_map(|n| std::iter::once(n).chain(nodes(&n.children))).collect()
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum NoticeKind {
    Question,
    Plan,
    Course,
    Finish,
    PlanChange,
    Blocked,
    Stopped,
    Finished,
    Review,
}

/// One thing to tell the person outside the window.
#[derive(Debug, Clone, PartialEq)]
pub struct Notice {
    pub kind: NoticeKind,
    pub title: String,
    pub body: String,
    /// The goal, or standalone work, a click opens.
    pub work: String,
    /// What it tells, so the same thing is never told twice; the first names it.
    pub keys: Vec<String>,
    /// The work and questions it is about, so a write by the person to any of
    /// them keeps it quiet.
    pub about: HashSet<String>,
}

/// The bands where work waits for the person to look it over and land it.
const FOR_REVIEW: [Band; 3] = [Band::ReadyToPush, Band::InReview, Band::ReadyToClose];

/// How long after the person's own write what it changed stays untold.
pub const QUIET_SECONDS: i64 = 5;

fn notice(kind: NoticeKind, title: &str, body: String, work: &str, keys: Vec<String>, about: &[&str]) -> Notice {
    Notice {
        kind,
        title: title.to_string(),
        body,
        work: work.to_string(),
        keys,
        about: about.iter().map(|s| s.to_string()).collect(),
    }
}

fn plural(n: usize, one: &str) -> String {
    if n == 1 {
        format!("{n} {one}")
    } else {
        format!("{n} {one}s")
    }
}

impl Notice {
    /// What happened between two sightings. With no earlier sighting there is
    /// nothing to compare, so nothing has happened. A notice whose keys were
    /// all told, or that is about anything in `quiet`, is left out.
    pub fn between(
        earlier: Option<&Sighting>,
        now: &Sighting,
        told: &HashSet<String>,
        quiet: &HashSet<String>,
    ) -> Vec<Notice> {
        let Some(earlier) = earlier else { return Vec::new() };
        let before_goals: HashMap<&str, &BoardGoal> =
            earlier.board.goals.iter().map(|g| (g.work.id.as_str(), g)).collect();
        let before_items: HashMap<&str, &BoardItem> =
            earlier.board.standalone.iter().map(|s| (s.work.id.as_str(), s)).collect();
        let reviewing = |band: Option<Band>| band.is_some_and(|b| FOR_REVIEW.contains(&b));
        let mut out = Vec::new();

        for g in &now.board.goals {
            let w = &g.work;
            let id = w.id.as_str();
            let before = before_goals.get(id).copied();
            let detail = now.goals.get(id);
            let mut said = Vec::new();

            if g.proposed && !before.is_some_and(|b| b.proposed) {
                let n = g.progress.total.max(0) as usize;
                let body = if n > 0 { format!("{} · {}", w.title, plural(n, "sub-goal")) } else { w.title.clone() };
                said.push(notice(
                    NoticeKind::Plan,
                    "Approve the plan",
                    body,
                    id,
                    vec![format!("plan:{id}:{}", w.updated)],
                    &[id],
                ));
            }

            if let (Some(detail), Some(known)) = (detail, earlier.known_questions(id)) {
                let fresh: Vec<_> = detail
                    .needs_you
                    .iter()
                    .filter(|n| !known.contains(&n.question.id) && n.question.pending())
                    .collect();
                let asked: Vec<_> = fresh.iter().filter(|n| n.change.is_none()).collect();
                let items: Vec<_> = asked.iter().flat_map(|n| &n.question.items).collect();
                if let Some(first) = items.first() {
                    let mut lines = vec![w.title.clone(), first.question.lines().next().unwrap_or("").to_string()];
                    if items.len() > 1 {
                        lines.push(plural(items.len() - 1, "more question"));
                    }
                    let mut about: Vec<&str> = vec![id];
                    about.extend(asked.iter().flat_map(|n| [n.work.as_str(), n.question.id.as_str()]));
                    said.push(notice(
                        NoticeKind::Question,
                        "Question for you",
                        lines.join("\n"),
                        id,
                        asked.iter().map(|n| format!("question:{}", n.question.id)).collect(),
                        &about,
                    ));
                }
                for ask in &fresh {
                    let Some(change) = &ask.change else { continue };
                    let (kind, title, body) = match change.kind {
                        ChangeKind::Course => (
                            NoticeKind::Course,
                            "Change of course proposed",
                            change.proposed.as_ref().map_or(w.title.clone(), |p| format!("{} — {}", w.title, p.title)),
                        ),
                        ChangeKind::Finish => (NoticeKind::Finish, "Choose where it finishes", w.title.clone()),
                        ChangeKind::Plan => (NoticeKind::PlanChange, "Plan change to approve", w.title.clone()),
                    };
                    said.push(notice(
                        kind,
                        title,
                        body,
                        id,
                        vec![format!("change:{}", ask.question.id)],
                        &[id, &ask.work, &ask.question.id],
                    ));
                }
            }

            if w.state == WorkState::Blocked && before.is_some_and(|b| b.work.state != WorkState::Blocked) {
                said.push(notice(
                    NoticeKind::Blocked,
                    "Blocked",
                    w.title.clone(),
                    id,
                    vec![format!("blocked:{id}:{}", w.updated)],
                    &[id],
                ));
            }
            if let (Some(detail), Some(known)) = (detail, earlier.known_blocked(id)) {
                for n in nodes(&detail.tree) {
                    if n.work.state == WorkState::Blocked && !known.contains(&n.work.id) {
                        let mut body = format!("{} — {}", w.title, n.work.title);
                        if let Some(why) = n.blocked_because.as_deref().filter(|b| !b.trim().is_empty()) {
                            body.push('\n');
                            body.push_str(why.trim());
                        }
                        said.push(notice(
                            NoticeKind::Blocked,
                            "Blocked",
                            body,
                            id,
                            vec![format!("blocked:{}:{}", n.work.id, n.work.updated)],
                            &[id, &n.work.id],
                        ));
                    }
                }
            }

            // A session that stopped without finishing, once per stop; one
            // already stopped at the earlier read is not news.
            if let (Some(detail), Some(was)) = (detail, earlier.goals.get(id)) {
                let stops_before: HashSet<(&str, &str)> = nodes(&was.tree)
                    .into_iter()
                    .filter_map(|n| n.stopped.as_ref().map(|s| (n.work.id.as_str(), s.at.as_str())))
                    .collect();
                for n in nodes(&detail.tree) {
                    let Some(stop) = &n.stopped else { continue };
                    if stops_before.contains(&(n.work.id.as_str(), stop.at.as_str())) {
                        continue;
                    }
                    said.push(notice(
                        NoticeKind::Stopped,
                        "Stopped without finishing",
                        format!("{} — {}\n{}", w.title, n.work.title, stop.first_line()),
                        id,
                        vec![format!("stopped:{}:{}", n.work.id, stop.at)],
                        &[id, &n.work.id],
                    ));
                }
            }

            if w.state == WorkState::Done && before.is_some_and(|b| b.work.state != WorkState::Done) {
                let body = g.finished.as_ref().map_or(w.title.clone(), |f| format!("{} — {f}", w.title));
                said.push(notice(
                    NoticeKind::Finished,
                    "Finished",
                    body,
                    id,
                    vec![format!("finished:{id}:{}", w.updated)],
                    &[id],
                ));
            }

            if reviewing(Some(g.band)) && !reviewing(before.map(|b| b.band)) {
                said.push(notice(
                    NoticeKind::Review,
                    "Ready for review",
                    w.title.clone(),
                    id,
                    vec![format!("review:{id}:{}", w.updated)],
                    &[id],
                ));
            }

            // Work came to wait on the person with nothing above to say why:
            // a session waiting without asking, or a question not yet read.
            if said.is_empty() && g.band == Band::NeedsYou && before.is_none_or(|b| b.band != Band::NeedsYou) {
                said.push(notice(
                    NoticeKind::Question,
                    "Question for you",
                    w.title.clone(),
                    id,
                    vec![format!("waiting:{id}:{}", w.updated)],
                    &[id],
                ));
            }
            out.extend(said);
        }

        for item in &now.board.standalone {
            let w = &item.work;
            let id = w.id.as_str();
            let before = before_items.get(id).copied();
            let was = before.map(|b| b.work.state);
            if w.state == WorkState::NeedsInput && was != Some(WorkState::NeedsInput) {
                out.push(notice(
                    NoticeKind::Question,
                    "Question for you",
                    w.title.clone(),
                    id,
                    vec![format!("question:{id}:{}", w.updated)],
                    &[id],
                ));
            }
            if w.state == WorkState::Blocked && was != Some(WorkState::Blocked) {
                out.push(notice(
                    NoticeKind::Blocked,
                    "Blocked",
                    w.title.clone(),
                    id,
                    vec![format!("blocked:{id}:{}", w.updated)],
                    &[id],
                ));
            }
            if reviewing(Some(item.band)) && !reviewing(before.map(|b| b.band)) {
                out.push(notice(
                    NoticeKind::Review,
                    "Ready for review",
                    w.title.clone(),
                    id,
                    vec![format!("review:{id}:{}", w.updated)],
                    &[id],
                ));
            }
        }

        out.retain(|n| !n.keys.iter().all(|k| told.contains(k)) && n.about.is_disjoint(quiet));
        out
    }

    /// The words of the person's writes still running, or ended within
    /// `QUIET_SECONDS` of `now`: the ids they name.
    pub fn quiet(running: &[Plan], ended: &[(Plan, DateTime<Utc>)], now: DateTime<Utc>) -> HashSet<String> {
        let recent = ended.iter().filter(|(_, at)| now - *at < Duration::seconds(QUIET_SECONDS)).map(|(p, _)| p);
        running.iter().chain(recent).flat_map(|p| p.argv.iter().cloned()).collect()
    }
}
