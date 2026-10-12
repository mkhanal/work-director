//! A message a person sent to work, shown from the moment it is sent until the
//! work's own record holds it.

use chrono::{DateTime, Utc};

use crate::conversation::{Step, StepKind};
use crate::ledger::GoalDetail;
use crate::plan::Plan;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum OutgoingState {
    Sending,
    /// `wd` took it; it shows here until the work's record does.
    Sent,
    /// Refused, with why. `sent` says whether `wd` took the message itself
    /// and only what ran after it failed.
    Failed {
        why: String,
        sent: bool,
    },
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Outgoing {
    pub id: u64,
    pub work: String,
    /// What was said, as sent.
    pub text: String,
    pub at: DateTime<Utc>,
    pub state: OutgoingState,
    pub(crate) plan: Plan,
    /// What runs once it is sent, as part of sending it: keeping it as feedback.
    pub(crate) then: Option<Plan>,
}

impl Outgoing {
    /// Whether the goal's record now holds it: a decision said in so many words.
    pub fn arrived_in_goal(&self, goal: &GoalDetail) -> bool {
        goal.events.iter().any(|e| e.work == self.work && e.kind == "decision" && e.body == self.text)
    }

    /// Whether a session's conversation now holds it: a prompt that contains it.
    pub fn arrived_in_steps(&self, steps: &[Step]) -> bool {
        steps.iter().any(|s| s.kind == StepKind::Prompt && s.text().contains(&self.text))
    }
}
