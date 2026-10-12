//! Starting a goal, the way a conversation starts: say what you want. Before
//! anything starts, one model call looks for the goals it may continue, so the
//! person can pick one up instead of filing a duplicate.

use std::cell::RefCell;
use std::rc::Rc;

use crate::ledger::{Found, Started};
use crate::plan::{Plan, PlanError};
use crate::store::Store;

#[derive(Debug, Clone, PartialEq)]
pub enum NewGoalPhase {
    Idle,
    Finding,
    Offering(Vec<Found>),
    Starting,
    Started(Started),
    Failed(String),
}

pub struct NewGoal {
    store: Rc<Store>,
    phase: RefCell<NewGoalPhase>,
}

impl NewGoal {
    pub fn new(store: Rc<Store>) -> NewGoal {
        NewGoal { store, phase: RefCell::new(NewGoalPhase::Idle) }
    }

    pub fn phase(&self) -> NewGoalPhase {
        self.phase.borrow().clone()
    }

    fn set(&self, phase: NewGoalPhase) {
        *self.phase.borrow_mut() = phase;
    }

    /// Looks for goals the request continues; with none, starts it at once.
    pub async fn submit(&self, project: &str, text: &str) {
        self.set(NewGoalPhase::Finding);
        match self.store.find(project, text).await {
            Ok(found) if found.is_empty() => self.start_new(project, text).await,
            Ok(found) => self.set(NewGoalPhase::Offering(found)),
            Err(e) => self.set(NewGoalPhase::Failed(e.to_string())),
        }
    }

    pub async fn start_new(&self, project: &str, text: &str) {
        self.run(Plan::start(project, text)).await;
    }

    pub async fn continue_goal(&self, id: &str, text: &str) {
        self.run(Plan::continue_goal(id, text)).await;
    }

    pub fn reset(&self) {
        self.set(NewGoalPhase::Idle);
    }

    async fn run(&self, plan: Result<Plan, PlanError>) {
        let plan = match plan {
            Ok(plan) => plan,
            Err(e) => return self.set(NewGoalPhase::Failed(e.to_string())),
        };
        self.set(NewGoalPhase::Starting);
        match self.store.start_goal(&plan).await {
            Ok(started) => self.set(NewGoalPhase::Started(started)),
            Err(e) => self.set(NewGoalPhase::Failed(e.to_string())),
        }
    }
}
