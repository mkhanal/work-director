//! The agents panel: every agent on a work item, and what they spent.

use std::collections::HashMap;

use chrono::{DateTime, Utc};

use crate::conversation::{Activity, SessionStatus, Subagent, Usage};
use crate::ledger::Work;
use crate::time::instant;

/// Where an agent is.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Phase {
    /// Its process is up and it is working.
    Working,
    /// It is waiting on a person's answer.
    Asking,
    /// Its process is up and it has nothing to do.
    Resting,
    /// It finished, or its session stopped.
    Settled,
}

impl Phase {
    /// Where a task's agent is, from its activity; settled with none.
    pub fn of(activity: Option<&Activity>) -> Phase {
        match activity {
            Some(a) => Phase::from_status(a.status, a.asking.is_some()),
            None => Phase::Settled,
        }
    }

    pub fn from_status(status: Option<SessionStatus>, asking: bool) -> Phase {
        if asking {
            return Phase::Asking;
        }
        match status {
            Some(SessionStatus::Running) => Phase::Working,
            Some(SessionStatus::Idle | SessionStatus::Waiting) => Phase::Resting,
            _ => Phase::Settled,
        }
    }
}

/// One card in the agents panel: a task's session, or a subagent one started.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct AgentCard {
    /// The task id, or "task/subagent" for a subagent.
    pub id: String,
    pub task: String,
    pub agent: Option<String>,
    pub title: String,
    pub kind: String,
    pub line: Option<String>,
    pub phase: Phase,
    pub usage: Option<Usage>,
    pub subagents: Vec<AgentCard>,
}

impl AgentCard {
    /// How long it has run, in seconds: from its first record to now while it
    /// works, to its last record once it does not.
    pub fn elapsed(&self, now: DateTime<Utc>) -> Option<i64> {
        let usage = self.usage.as_ref()?;
        let started = instant(usage.started.as_deref()?)?;
        let end = match self.phase {
            Phase::Working => now,
            _ => usage.last.as_deref().and_then(instant).unwrap_or(now),
        };
        Some((end - started).num_seconds().max(0))
    }

    fn subagent(task: &str, s: &Subagent) -> AgentCard {
        AgentCard {
            id: format!("{task}/{}", s.id),
            task: task.to_string(),
            agent: Some(s.id.clone()),
            title: if s.description.is_empty() { s.id.clone() } else { s.description.clone() },
            kind: s.kind.clone(),
            line: s.said.clone(),
            phase: if s.done { Phase::Settled } else { Phase::Working },
            usage: Some(s.usage.clone()),
            subagents: Vec::new(),
        }
    }

    fn session(work: &Work, title: &str, kind: &str, a: &Activity) -> AgentCard {
        AgentCard {
            id: work.id.clone(),
            task: work.id.clone(),
            agent: None,
            title: title.to_string(),
            kind: kind.to_string(),
            line: a.error.clone().or_else(|| a.now.clone()),
            phase: Phase::of(Some(a)),
            usage: a.usage.clone(),
            subagents: a.subagents.iter().flatten().map(|s| AgentCard::subagent(&work.id, s)).collect(),
        }
    }
}

/// Every task of a goal with a session, each with the subagents it started.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct AgentRoster {
    pub cards: Vec<AgentCard>,
}

impl AgentRoster {
    /// Cards for a goal's tasks, in the goal's order, after the goal's own
    /// session while it has one: its supervisor once it is supervised, else
    /// its planner.
    pub fn for_goal(
        goal: Option<&Work>,
        supervised: bool,
        tasks: &[Work],
        activity: &HashMap<String, Activity>,
    ) -> AgentRoster {
        let own = goal.and_then(|g| {
            activity.get(&g.id).map(|a| {
                let (title, kind) = if supervised { ("Supervisor", "supervisor") } else { ("Plan", "planner") };
                AgentCard::session(g, title, kind, a)
            })
        });
        let tasks =
            tasks.iter().filter_map(|t| activity.get(&t.id).map(|a| AgentCard::session(t, &t.title, t.kind.name(), a)));
        AgentRoster { cards: own.into_iter().chain(tasks).collect() }
    }

    /// The one card for a task's own session, from its conversation.
    pub fn for_task(
        task: &Work,
        status: SessionStatus,
        asking: bool,
        line: Option<String>,
        usage: Option<Usage>,
        subagents: &[Subagent],
    ) -> AgentRoster {
        AgentRoster {
            cards: vec![AgentCard {
                id: task.id.clone(),
                task: task.id.clone(),
                agent: None,
                title: task.title.clone(),
                kind: task.kind.name().to_string(),
                line,
                phase: Phase::from_status(Some(status), asking),
                usage,
                subagents: subagents.iter().map(|s| AgentCard::subagent(&task.id, s)).collect(),
            }],
        }
    }

    fn all(&self) -> impl Iterator<Item = &AgentCard> {
        self.cards.iter().flat_map(|c| std::iter::once(c).chain(c.subagents.iter()))
    }

    /// How many cards, subagents included, have settled.
    pub fn settled(&self) -> usize {
        self.all().filter(|c| c.phase == Phase::Settled).count()
    }

    /// The tokens every card spent, subagents included.
    pub fn tokens(&self) -> i64 {
        self.all().filter_map(|c| c.usage.as_ref().map(|u| u.tokens)).sum()
    }

    pub fn card(&self, id: &str) -> Option<&AgentCard> {
        self.all().find(|c| c.id == id)
    }
}

/// A count of tokens as a person reads it: 950, 12k, 1.1M.
pub fn token_count(n: i64) -> String {
    if n < 1000 {
        n.to_string()
    } else if n < 1_000_000 {
        format!("{}k", (n as f64 / 1000.0).round() as i64)
    } else {
        format!("{:.1}M", n as f64 / 1_000_000.0)
    }
}

/// A span as a person reads it: 45s, 11m 10s, 2h 5m.
pub fn span(seconds: i64) -> String {
    if seconds < 60 {
        format!("{seconds}s")
    } else if seconds < 3600 {
        format!("{}m {}s", seconds / 60, seconds % 60)
    } else {
        format!("{}h {}m", seconds / 3600, (seconds % 3600) / 60)
    }
}
