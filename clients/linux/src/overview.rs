//! A goal page's overview as it reads: the cards that say what done means,
//! and each sub-goal as a row. Every fact comes from the goal view; this only
//! words it.

use std::collections::HashSet;

use crate::conversation::Question;
use crate::ledger::{GoalDetail, WorkState};
use crate::look::{Mark, Tone};
use crate::plan::{Plan, PlanError};
use crate::record::{RecordBy, RecordEntry, RecordView};
use crate::tree::{ChangeKind, Course, FinishLine, NeedYou, Node, PlanStep, Shape, StepState};

/// One sub-goal as its row reads.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct SubGoalRow {
    pub id: String,
    pub title: String,
    pub state: WorkState,
    pub mark: Mark,
    /// Done-when, owner, what it is doing or why it is not moving.
    pub meta: String,
    pub badge: (String, Tone),
    /// The goal page the row opens; a task's row folds its steps instead.
    pub opens: Option<String>,
    /// Superseded: struck through, with why.
    pub struck: bool,
    pub steps: Vec<PlanStep>,
    /// Whether its steps start shown.
    pub open: bool,
    /// Whether a session works on it or under it now.
    pub working: bool,
    /// What tells it to carry on, while its session has stopped without finishing.
    pub carry_on: Option<Plan>,
}

/// Ledger text read as a sentence: its first letter capitalised.
pub fn sentence(text: &str) -> String {
    capitalised(text)
}

fn capitalised(text: &str) -> String {
    let mut chars = text.chars();
    chars.next().map(|c| c.to_uppercase().chain(chars).collect()).unwrap_or_default()
}

fn plural(n: i64, one: &str, many: &str) -> String {
    format!("{n} {}", if n == 1 { one } else { many })
}

impl Node {
    fn steps_done(&self) -> (usize, usize) {
        let kept = self.steps.iter().filter(|s| s.state != StepState::Dropped);
        let total = kept.clone().count();
        (kept.filter(|s| s.state == StepState::Done).count(), total)
    }

    /// The row's meta line.
    fn meta(&self) -> String {
        let state = self.work.state;
        if state == WorkState::Superseded {
            let why = self.why_not_moving.as_deref().map(|w| w.strip_prefix("superseded: ").unwrap_or(w));
            let why = why.filter(|w| *w != "superseded");
            return crate::words::replaced_because(why);
        }
        if self.opens().is_some() || self.group {
            let p = self.progress.unwrap_or_default();
            let mut parts = vec![crate::words::sub_goals(p.total), format!("{} of {} done", p.done, p.total)];
            if p.superseded > 0 {
                parts.push(crate::words::replaced_count(p.superseded));
            }
            return parts.join(" · ");
        }
        let mut parts: Vec<String> = Vec::new();
        match state.mark() {
            Mark::Done => {
                if let Some(d) = &self.done_when {
                    parts.push(format!("Done when {d}"));
                }
                parts.extend(self.owner.iter().map(|o| o.runner.clone()));
                if !self.steps.is_empty() {
                    parts.push(plural(self.steps.len() as i64, "step", "steps"));
                }
            }
            Mark::Working => {
                parts.extend(self.owner.iter().map(|o| o.runner.clone()));
                if let Some(stop) = &self.stopped {
                    parts.push(format!("Stopped without finishing: {}", stop.first_line()));
                } else if let Some(doing) = &self.doing {
                    parts.push(crate::words::now(doing));
                }
            }
            _ => {
                parts.extend(self.owner.iter().map(|o| o.runner.clone()));
                match self
                    .blocked_because
                    .as_ref()
                    .filter(|_| state == WorkState::Blocked)
                    .or(self.why_not_moving.as_ref())
                {
                    Some(why) => parts.push(capitalised(why)),
                    None if state.mark() == Mark::Waiting => parts.push("Not started".to_string()),
                    None => parts.push(state.look().label.to_string()),
                }
            }
        }
        if let Some(j) = &self.judged {
            parts.push(crate::words::checked(&j.verdict).to_string());
        }
        parts.join(" · ")
    }

    fn badge(&self) -> (String, Tone) {
        if self.needs_you > 0 && !self.children.is_empty() {
            return (format!("{} needs you", self.needs_you), Tone::Attention);
        }
        let (label, tone) = self.work.state.badge();
        let (done, total) = self.steps_done();
        if self.work.state.mark() == Mark::Working && total > 0 {
            return (format!("{label} · {done} of {total}"), tone);
        }
        (label.to_string(), tone)
    }

    /// The node as its row reads.
    pub fn row(&self) -> SubGoalRow {
        SubGoalRow {
            id: self.work.id.clone(),
            title: self.work.title.clone(),
            state: self.work.state,
            mark: self.work.state.mark(),
            meta: self.meta(),
            badge: self.badge(),
            opens: self.opens().map(str::to_string),
            struck: self.work.state == WorkState::Superseded,
            steps: self.steps.clone(),
            open: false,
            working: false,
            carry_on: self.stopped.as_ref().map(|_| Plan::carry_on(&self.work.id)),
        }
    }
}

impl Course {
    /// The verdict as a badge.
    pub fn badge(&self) -> (String, Tone) {
        match self.verdict.as_str() {
            "on course" => ("On course".into(), Tone::Settled),
            "correct" => ("Corrected".into(), Tone::Live),
            "pivot" => ("Pivot proposed".into(), Tone::Attention),
            _ => ("Checked".into(), Tone::Waiting),
        }
    }
}

impl GoalDetail {
    /// Each sub-goal as a row, in plan order. A task's steps are folded,
    /// except the first working task's.
    pub fn sub_goal_rows(&self) -> Vec<SubGoalRow> {
        let mut rows: Vec<SubGoalRow> =
            self.tree.iter().map(|n| SubGoalRow { working: self.under_way(&n.work.id), ..n.row() }).collect();
        if let Some(first) =
            rows.iter_mut().find(|r| r.opens.is_none() && r.mark == Mark::Working && !r.steps.is_empty())
        {
            first.open = true;
        }
        rows
    }

    /// Done of total, and how many were replaced.
    pub fn progress_line(&self) -> String {
        let p = self.progress;
        let mut line = format!("{} of {} done", p.done, p.total);
        if p.superseded > 0 {
            line.push_str(&format!(" · {}", crate::words::replaced_count(p.superseded)));
        }
        line
    }

    /// The finish line card: what it is and where it came from; a sub-goal
    /// has none of its own.
    pub fn finish_card(&self) -> Option<(String, String)> {
        if self.nested() {
            return None;
        }
        Some(match self.finish {
            Some(f) => (f.line.title().to_string(), format!("{}: {}.", f.source.sentence(), f.line.means())),
            None => ("Not decided yet".to_string(), "Decided when the goal is planned.".to_string()),
        })
    }
}

impl RecordView {
    /// What the director did itself, newest first.
    pub fn director_did(&self) -> Vec<&RecordEntry> {
        self.entries.iter().filter(|e| e.by == RecordBy::Director).collect()
    }
}

/// One sub-goal of a proposed breakdown, as its card reads.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct ProposedCard {
    pub id: String,
    pub number: usize,
    pub title: String,
    /// What makes it done.
    pub meta: String,
    /// When it starts, and whether one agent does it or it is planned further.
    pub chips: Vec<String>,
    /// Whether it can be split: a task can, a goal already is.
    pub splits: bool,
    /// What it waits on before it starts.
    pub after: Vec<String>,
}

impl ProposedCard {
    /// What one of its actions is announced as: the action and the card it
    /// acts on, since every card has one.
    pub fn announced(&self, action: &str, then: &str) -> String {
        format!("{action} {} · {}{then}", self.number, self.title)
    }

    /// Folds two cards into one: the earlier is kept.
    pub fn merge(&self, other: &ProposedCard) -> Plan {
        let (keep, gone) = if self.number < other.number { (self, other) } else { (other, self) };
        Plan::merge(&keep.id, &gone.id)
    }
}

fn numbers(ns: &[usize]) -> String {
    let ns: Vec<String> = ns.iter().map(usize::to_string).collect();
    match ns.split_last() {
        Some((last, rest)) if !rest.is_empty() => format!("{} and {last}", rest.join(", ")),
        _ => ns.join(""),
    }
}

/// Each node after every sibling it waits on, else in the order served; a
/// cycle cannot be ordered and keeps the order served.
fn wait_order(nodes: Vec<&Node>) -> Vec<&Node> {
    let ids: HashSet<&str> = nodes.iter().map(|n| n.work.id.as_str()).collect();
    let mut placed: Vec<&Node> = Vec::new();
    let mut done: HashSet<&str> = HashSet::new();
    let mut left = nodes;
    while !left.is_empty() {
        let ready =
            left.iter().position(|n| n.after.iter().all(|a| !ids.contains(a.as_str()) || done.contains(a.as_str())));
        let next = left.remove(ready.unwrap_or(0));
        done.insert(next.work.id.as_str());
        placed.push(next);
    }
    placed
}

impl GoalDetail {
    /// The breakdown waiting for approval, numbered in wait order; none once approved.
    pub fn proposal(&self) -> Option<Vec<ProposedCard>> {
        if !self.proposed {
            return None;
        }
        let shown =
            wait_order(self.tree.iter().filter(|n| !n.group && n.work.state != WorkState::Superseded).collect());
        let number = |id: &str| shown.iter().position(|n| n.work.id == id).map(|i| i + 1);
        Some(
            shown
                .iter()
                .enumerate()
                .map(|(i, n)| {
                    let mut after: Vec<usize> = n.after.iter().filter_map(|a| number(a)).collect();
                    after.sort_unstable();
                    let start = if after.is_empty() {
                        "Starts first".to_string()
                    } else {
                        format!("After {}", numbers(&after))
                    };
                    let who = if n.work.kind.is_goal() { "Will be planned further" } else { "One agent" };
                    ProposedCard {
                        id: n.work.id.clone(),
                        number: i + 1,
                        title: n.work.title.clone(),
                        meta: n
                            .done_when
                            .as_ref()
                            .map_or_else(|| "Done when: not set".to_string(), |d| format!("Done when {d}")),
                        chips: vec![start, who.to_string()],
                        splits: !n.work.kind.is_goal(),
                        after: n.after.clone(),
                    }
                })
                .collect(),
        )
    }

    /// The writes that put the proposal in the order a card was dropped in,
    /// moving `id` to `index`: each sub-goal waits on the one before it and
    /// the first on none. Only waits that change are written, from the top,
    /// so no write makes a cycle.
    pub fn reorder(&self, id: &str, index: usize) -> Vec<Plan> {
        let Some(mut cards) = self.proposal() else { return Vec::new() };
        let Some(from) = cards.iter().position(|c| c.id == id) else { return Vec::new() };
        let moved = cards.remove(from);
        cards.insert(index.min(cards.len()), moved);
        cards
            .iter()
            .enumerate()
            .filter_map(|(i, card)| {
                let wants: Vec<String> = if i == 0 { Vec::new() } else { vec![cards[i - 1].id.clone()] };
                let has: HashSet<&String> = card.after.iter().collect();
                (has != wants.iter().collect()).then(|| Plan::after(&card.id, &wants))
            })
            .collect()
    }

    /// The finish line a top goal's pick shows, none until decided; a
    /// sub-goal has no pick.
    pub fn finish_pick(&self) -> Option<Option<FinishLine>> {
        (!self.nested()).then(|| self.finish.map(|f| f.line))
    }
}

impl NeedYou {
    /// The line that heads its card: what kind of question it is, and where.
    pub fn label(&self, goal: &str) -> String {
        let place = if self.work == goal { String::new() } else { format!(" · in {}", self.path) };
        match self.change.as_ref().map(|c| c.kind) {
            Some(ChangeKind::Course) => "Course change on the top goal · always yours to decide".to_string(),
            Some(ChangeKind::Finish) => "Before I plan it · where does this goal finish?".to_string(),
            Some(ChangeKind::Plan) => format!("Needs you · a change to the plan{place}"),
            None => format!("Needs you{place}"),
        }
    }

    /// The question as its card asks it: a change's question by its first
    /// line, the rest being shown as the change; a finish line's choices each
    /// with what it means.
    pub fn shown_question(&self) -> Question {
        let mut q = self.question.clone();
        match self.change.as_ref().map(|c| c.kind) {
            Some(ChangeKind::Course | ChangeKind::Plan) => {
                for item in &mut q.items {
                    item.question = item.question.lines().next().unwrap_or("").to_string();
                }
            }
            Some(ChangeKind::Finish) => {
                for option in q.items.iter_mut().flat_map(|i| i.options.iter_mut()) {
                    if let Some(line) = FinishLine::from_label(crate::conversation::unmarked(&option.label)) {
                        let (title, means) = line.choice();
                        option.title = Some(title.to_string());
                        if option.description.is_empty() {
                            option.description = means.to_string();
                        }
                    }
                }
            }
            None => {}
        }
        q
    }

    /// A course change as rows of what it is now and what is proposed:
    /// goal, done when, finishes at.
    pub fn compare(&self) -> Vec<(&'static str, String, String)> {
        let Some((now, proposed)) = self.change.as_ref().and_then(|c| c.now.as_ref().zip(c.proposed.as_ref())) else {
            return Vec::new();
        };
        let done = |s: &Shape| s.done_when.clone().unwrap_or_else(|| "not set".to_string());
        let finish = |s: &Shape| s.finish.map_or_else(|| "not decided".to_string(), |f| f.title().to_string());
        vec![
            ("Goal", now.title.clone(), proposed.title.clone()),
            ("Done when", done(now), done(proposed)),
            ("Finishes at", finish(now), finish(proposed)),
        ]
    }

    /// What each source of a finish line said, in the order the question
    /// card lists them; a source that said nothing is left out.
    pub fn sources(&self) -> Vec<(&'static str, String)> {
        let Some(said) = self.change.as_ref().and_then(|c| c.sources.as_ref()) else {
            return Vec::new();
        };
        [("The goal", &said.goal), ("Your taste", &said.taste), ("This repo", &said.repo)]
            .into_iter()
            .filter_map(|(who, said)| {
                said.as_deref().map(str::trim).filter(|s| !s.is_empty()).map(|s| (who, capitalised(s)))
            })
            .collect()
    }

    /// The box that keeps a finish-line answer as the product's setting; a
    /// finish-line question's only.
    pub fn remember_label(&self, project: &str) -> Option<String> {
        (self.change.as_ref().map(|c| c.kind) == Some(ChangeKind::Finish))
            .then(|| format!("Finish goals in {project} this way from now on"))
    }

    /// A change's first choice, which makes it, and its second, which keeps
    /// things as they are; none without both.
    fn choices(&self) -> Option<(String, String)> {
        let options = &self.question.items.first()?.options;
        let [change, keep, ..] = options.as_slice() else { return None };
        let bare = |o: &crate::conversation::QuestionOption| crate::conversation::unmarked(&o.label).to_string();
        Some((bare(change), bare(keep)))
    }

    /// The keep choice, which labels the button that answers with it.
    pub fn keep_label(&self) -> Option<String> {
        self.choices().map(|(_, keep)| keep)
    }

    /// The change answered by its keep choice.
    pub fn keep(&self) -> Option<Plan> {
        Plan::answer(&self.work, &self.question, &[self.choices()?.1]).ok()
    }

    /// The change answered by its first choice, which makes it.
    pub fn change_course(&self) -> Option<Plan> {
        Plan::answer(&self.work, &self.question, &[self.choices()?.0]).ok()
    }

    /// The change made with its proposed title, done-when and finish line
    /// edited: answered by its first choice, then whatever differs from the
    /// proposal set on the goal.
    pub fn edit_change(&self, title: &str, done_when: &str, finish: FinishLine) -> Result<Vec<Plan>, PlanError> {
        let proposed = self.change.as_ref().and_then(|c| c.proposed.as_ref()).ok_or(PlanError::Empty)?;
        if title.trim().is_empty() {
            return Err(PlanError::ReasonRequired("a goal's title"));
        }
        let mut plans = vec![self.change_course().ok_or(PlanError::Empty)?];
        let was_done = proposed.done_when.as_deref().unwrap_or("");
        let title = (title.trim() != proposed.title).then_some(title);
        let done_when = (done_when.trim() != was_done).then_some(done_when);
        if title.is_some() || done_when.is_some() {
            plans.push(Plan::edit_sub_goal(&self.work, title, done_when)?);
        }
        if proposed.finish != Some(finish) {
            plans.push(Plan::finish(&self.work, finish));
        }
        Ok(plans)
    }
}
