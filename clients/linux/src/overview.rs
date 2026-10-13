//! A goal page's overview as it reads: the cards that say what done means,
//! and each sub-goal as a row. Every fact comes from the goal view; this only
//! words it.

use crate::ledger::{GoalDetail, WorkState};
use crate::look::{Mark, Tone};
use crate::record::{RecordBy, RecordEntry, RecordKind, RecordView};
use crate::tree::{Course, Node, PlanStep, StepState};

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
            let why = self.why_not_moving.as_deref().filter(|w| *w != "superseded");
            return why.map_or_else(|| "Superseded".to_string(), |w| format!("Superseded: {w}"));
        }
        if self.opens().is_some() || self.group {
            let n = self.planned_into.unwrap_or(self.children.len() as i64);
            let mut parts = vec![format!("Planned into {}", plural(n, "sub-goal", "sub-goals"))];
            if let Some(p) = self.progress {
                parts.push(format!("{} of {} done", p.done, p.total));
            }
            if let Some(why) = &self.why_not_moving {
                parts.push(capitalised(why));
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
                if let Some(doing) = &self.doing {
                    parts.push(format!("now: {doing}"));
                }
            }
            _ => {
                parts.extend(self.owner.iter().map(|o| o.runner.clone()));
                match &self.why_not_moving {
                    Some(why) => parts.push(capitalised(why)),
                    None if state.mark() == Mark::Waiting => parts.push("Not started".to_string()),
                    None => parts.push(state.look().label.to_string()),
                }
            }
        }
        if let Some(j) = &self.judged {
            parts.push(format!("judged {}", j.verdict));
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
        let mut rows: Vec<SubGoalRow> = self.tree.iter().map(Node::row).collect();
        if let Some(first) =
            rows.iter_mut().find(|r| r.opens.is_none() && r.mark == Mark::Working && !r.steps.is_empty())
        {
            first.open = true;
        }
        rows
    }

    /// Done of total, and how many were superseded.
    pub fn progress_line(&self) -> String {
        let p = self.progress;
        let mut line = format!("{} of {} done", p.done, p.total);
        if p.superseded > 0 {
            line.push_str(&format!(" · {} superseded", p.superseded));
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
    /// What the director did itself: its course checks, chores, judgements,
    /// answers and supersessions, newest first.
    pub fn director_did(&self) -> Vec<&RecordEntry> {
        self.entries
            .iter()
            .filter(|e| e.by == RecordBy::Director)
            .filter(|e| {
                matches!(
                    e.kind,
                    RecordKind::Course
                        | RecordKind::Chore
                        | RecordKind::Judged
                        | RecordKind::Answer
                        | RecordKind::Superseded
                        | RecordKind::Finish
                )
            })
            .collect()
    }
}
