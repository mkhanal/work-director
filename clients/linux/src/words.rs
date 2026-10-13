//! The words the app shows for what wd names in its own terms. Machine values
//! (states, event kinds, gates, finish lines) stay as wd sends them; only what
//! a person reads is said here, once.

use crate::ledger::WorkState;

/// A branch a change of plan let go of, as its badge says it.
pub const REPLACED: &str = "Replaced";

/// The line under a replaced branch: why the plan let it go.
pub fn replaced_because(why: Option<&str>) -> String {
    match why {
        Some(why) => format!("Replaced by a change of plan: {why}"),
        None => "Replaced by a change of plan".to_string(),
    }
}

/// How many sub-goals were replaced, counted apart.
pub fn replaced_count(n: i64) -> String {
    format!("{n} replaced")
}

/// The director's check of a sub-goal's done-when.
pub fn checked(verdict: &str) -> &'static str {
    if verdict == "met" {
        "Checked: done"
    } else {
        "Checked: not done yet"
    }
}

/// What work is doing now.
pub fn now(doing: &str) -> String {
    format!("Now: {doing}")
}

/// How many sub-goals a goal holds.
pub fn sub_goals(n: i64) -> String {
    format!("{n} sub-goal{}", if n == 1 { "" } else { "s" })
}

/// The button that passes a gate, by the gate wd names.
pub fn gate(gate: &str) -> String {
    match gate {
        "verify" => "Check it".to_string(),
        "land" => "Deliver it".to_string(),
        "soft-done" => "Confirm it's finished".to_string(),
        "done" => "Mark it done".to_string(),
        other => capitalised(other),
    }
}

/// What an event in work's history is, by the kind wd files it under.
pub fn event(kind: &str) -> String {
    match kind {
        "state" => "Moved",
        "report" => "Report",
        "verify" => "Checks",
        "pr" => "Delivered",
        "note" => "Note",
        "sent" => "Message",
        "spawn" => "Started",
        "attach" => "Session",
        "question" => "Question",
        "answer" => "Answer",
        "decision" => "Decision",
        "abandon" => "Abandoned",
        "look" => "Looked",
        "supervised" => "Supervised",
        "course" => "Direction",
        "judged" => "Checked",
        "attachment" => "File",
        other => return capitalised(other),
    }
    .to_string()
}

/// An event's text: a state change says the state in the app's words.
pub fn event_body(kind: &str, body: &str) -> String {
    if kind != "state" {
        return body.to_string();
    }
    WorkState::ALL
        .into_iter()
        .find(|s| state_wire(*s) == body)
        .map_or_else(|| body.to_string(), |s| s.look().label.to_string())
}

/// A state as wd writes it.
fn state_wire(state: WorkState) -> &'static str {
    match state {
        WorkState::Queued => "queued",
        WorkState::Briefed => "briefed",
        WorkState::Running => "running",
        WorkState::NeedsInput => "needs-input",
        WorkState::Review => "review",
        WorkState::SoftDone => "soft-done",
        WorkState::Done => "done",
        WorkState::Blocked => "blocked",
        WorkState::Dropped => "dropped",
        WorkState::Abandoned => "abandoned",
        WorkState::Paused => "paused",
        WorkState::Superseded => "superseded",
    }
}

/// wd's own terms, which no label the app shows may carry.
pub const INTERNAL_TERMS: [&str; 9] =
    ["superseded", "supersede", "judged", "soft-done", "briefed", "needs-input", "verify", "land", "claim"];

/// Whether a label is free of wd's own terms, "PR" included.
pub fn plain(label: &str) -> bool {
    let lower = label.to_lowercase();
    if INTERNAL_TERMS.iter().any(|t| lower.contains(t)) {
        return false;
    }
    !label.split(|c: char| !c.is_alphanumeric()).any(|w| w == "PR" || w == "PRs")
}

fn capitalised(text: &str) -> String {
    let mut chars = text.chars();
    chars.next().map(|c| c.to_uppercase().chain(chars).collect()).unwrap_or_default()
}
