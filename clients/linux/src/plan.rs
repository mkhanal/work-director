//! Every write the app makes: the exact argv `wd` runs.

use std::fmt;

use serde::Deserialize;

use crate::conversation::Question;
use crate::ledger::{Gate, Work};
use crate::tree::FinishLine;

/// One item of a question as the director judged it: its answer and why, or
/// no answer and why it declined.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct DirectorChoice {
    pub question: String,
    #[serde(default)]
    pub answer: Option<String>,
    pub why: String,
}

/// What `wd answer <id> --director --json` printed: answered only when no
/// item was declined.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct DirectorAnswer {
    pub answered: bool,
    pub decided: Vec<DirectorChoice>,
    pub declined: Vec<DirectorChoice>,
}

impl DirectorAnswer {
    pub fn read(stdout: &str) -> Result<DirectorAnswer, serde_json::Error> {
        serde_json::from_str(stdout)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PlanError {
    ReasonRequired(&'static str),
    UnclosedQuote,
    Empty,
}

impl fmt::Display for PlanError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            PlanError::ReasonRequired(what) => write!(f, "{what} needs a reason"),
            PlanError::UnclosedQuote => write!(f, "a quote is not closed"),
            PlanError::Empty => write!(f, "nothing to run"),
        }
    }
}

impl std::error::Error for PlanError {}

/// One write: a title for a person and the exact argv `wd` runs.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct Plan {
    pub title: String,
    pub argv: Vec<String>,
}

fn plan(title: &str, argv: Vec<String>) -> Plan {
    Plan { title: title.to_string(), argv }
}

fn words(argv: &[&str]) -> Vec<String> {
    argv.iter().map(|s| s.to_string()).collect()
}

fn optional(text: &str) -> Vec<String> {
    let text = text.trim();
    if text.is_empty() {
        Vec::new()
    } else {
        vec![text.to_string()]
    }
}

fn required(text: &str, what: &'static str) -> Result<String, PlanError> {
    let text = text.trim();
    if text.is_empty() {
        Err(PlanError::ReasonRequired(what))
    } else {
        Ok(text.to_string())
    }
}

fn said(text: &str) -> Result<String, PlanError> {
    let text = text.trim();
    if text.is_empty() {
        Err(PlanError::Empty)
    } else {
        Ok(text.to_string())
    }
}

impl Plan {
    /// The command as a person reads it, quoted so it reads back as typed.
    pub fn command_line(&self) -> String {
        std::iter::once("wd".to_string())
            .chain(self.argv.iter().map(|w| quoted(w.as_str())))
            .collect::<Vec<_>>()
            .join(" ")
    }

    /// Text typed as a command, split into words without a shell.
    pub fn typed(text: &str) -> Result<Plan, PlanError> {
        let mut argv = split(text)?;
        if argv.first().map(String::as_str) == Some("wd") {
            argv.remove(0);
        }
        if argv.is_empty() {
            return Err(PlanError::Empty);
        }
        Ok(plan("Run", argv))
    }

    pub fn reopen(id: &str, reason: &str) -> Result<Plan, PlanError> {
        Ok(plan("Reopen", vec!["reopen".into(), id.into(), required(reason, "reopening")?]))
    }

    pub fn release(id: &str, reason: &str) -> Plan {
        plan("Release", [words(&["release", id]), optional(reason)].concat())
    }

    pub fn decide(id: &str, text: &str) -> Result<Plan, PlanError> {
        Ok(plan("Record decision", vec!["decide".into(), id.into(), required(text, "a decision")?]))
    }

    /// Starts a goal from what the person typed. Planning runs in its own
    /// process, so the goal opens while it is planned and can be watched and
    /// stopped.
    pub fn start(project: &str, text: &str) -> Result<Plan, PlanError> {
        let text = said(text)?;
        Ok(plan(
            "Start goal",
            vec!["goal".into(), "start".into(), project.into(), text, "--detach".into(), "--json".into()],
        ))
    }

    /// Picks a goal up where it stands with what the person typed.
    pub fn continue_goal(id: &str, text: &str) -> Result<Plan, PlanError> {
        let text = said(text)?;
        Ok(plan("Continue goal", vec!["goal".into(), "continue".into(), id.into(), text, "--json".into()]))
    }

    /// Archives work. Work at rest needs no reason; open work is dropped first,
    /// which is a choice someone may state.
    pub fn archive(work: &Work, reason: &str) -> Plan {
        let title = if work.at_rest() { "Archive" } else { "Drop and archive" };
        plan(title, [words(&["archive", &work.id]), optional(reason)].concat())
    }

    pub fn unarchive(id: &str) -> Plan {
        plan("Unarchive", words(&["unarchive", id]))
    }

    /// Something said to work while it runs: a goal passes it to every open
    /// task and keeps it as a decision; a running session gets it when idle.
    pub fn send(id: &str, text: &str) -> Result<Plan, PlanError> {
        Ok(plan("Send", vec!["send".into(), id.into(), said(text)?]))
    }

    /// The answers to the question work waits on, one per item in the order asked.
    pub fn answer(id: &str, question: &Question, answers: &[String]) -> Result<Plan, PlanError> {
        let answers: Vec<String> = answers.iter().map(|a| a.trim().to_string()).collect();
        if answers.len() != question.items.len() || answers.iter().any(String::is_empty) {
            return Err(PlanError::Empty);
        }
        Ok(plan("Answer", [words(&["answer", id]), answers].concat()))
    }

    /// The same answer, a finish-line choice also kept as the product's setting.
    pub fn remembering(mut self) -> Plan {
        self.argv.push("--remember".into());
        self
    }

    /// Leaves a question to the director's judgement, which answers all of
    /// it or none; what it printed is read with `DirectorAnswer::read`.
    pub fn director_decides(id: &str) -> Plan {
        plan("Let the director decide", words(&["answer", id, "--director", "--json"]))
    }

    /// Sets where a top goal finishes.
    pub fn finish(goal: &str, line: FinishLine) -> Plan {
        plan("Finishes at", words(&["goal", "finish", goal, line.wire()]))
    }

    /// Folds one sub-goal into another, which keeps both done-whens.
    pub fn merge(keep: &str, other: &str) -> Plan {
        plan("Merge", words(&["goal", "merge", keep, other]))
    }

    /// What a sub-goal waits on before it starts; none starts it first.
    pub fn after(id: &str, waits_on: &[String]) -> Plan {
        let waits = if waits_on.is_empty() { "none".to_string() } else { waits_on.join(",") };
        plan("Reorder", vec!["goal".into(), "after".into(), id.into(), waits])
    }

    /// A sub-goal added to a goal, with what makes it done.
    pub fn add_sub_goal(goal: &str, outcome: &str, done_when: &str) -> Result<Plan, PlanError> {
        let outcome = required(outcome, "a sub-goal")?;
        let done_when = required(done_when, "a sub-goal's done-when")?;
        Ok(plan(
            "Add sub-goal",
            vec!["goal".into(), "add".into(), goal.into(), outcome, "--done-when".into(), done_when],
        ))
    }

    /// Starts a proposed breakdown.
    pub fn approve(goal: &str) -> Plan {
        plan("Approve and start", words(&["goal", "approve", goal]))
    }

    /// Plans a goal again.
    pub fn replan(goal: &str) -> Plan {
        plan("Re-plan", words(&["goal", "plan", goal]))
    }

    /// Plans a goal again with a note the planner reads as a decision.
    pub fn replan_with(goal: &str, note: &str) -> Result<Vec<Plan>, PlanError> {
        let note = required(note, "re-planning")?;
        Ok(vec![plan("Note for planning", vec!["decide".into(), goal.into(), note]), Plan::replan(goal)])
    }

    /// A sub-goal's title and done-when changed; a field left as it was is not sent.
    pub fn edit_sub_goal(id: &str, title: Option<&str>, done_when: Option<&str>) -> Result<Plan, PlanError> {
        let mut argv = words(&["goal", "edit", id]);
        for (flag, value) in [("--title", title), ("--done-when", done_when)] {
            if let Some(value) = value {
                argv.push(flag.into());
                argv.push(required(value, "an edit")?);
            }
        }
        if argv.len() == 3 {
            return Err(PlanError::Empty);
        }
        Ok(plan("Edit", argv))
    }

    /// A sub-goal made a goal of its own, to be planned further.
    pub fn split_sub_goal(id: &str) -> Plan {
        plan("Split", words(&["goal", "split", id]))
    }

    /// A sub-goal let go of, with why.
    pub fn drop_sub_goal(id: &str, why: &str) -> Result<Plan, PlanError> {
        Ok(plan("Drop", vec!["goal".into(), "drop".into(), id.into(), required(why, "dropping a sub-goal")?]))
    }

    /// Interrupts what work's sessions are doing and takes back the messages
    /// they have not read; the work keeps its state.
    pub fn stop(id: &str) -> Plan {
        plan("Stop", words(&["stop", id, "--json"]))
    }

    /// The write that passes a gate, exactly as wd served it.
    pub fn passing(gate: &Gate) -> Option<Plan> {
        if gate.argv.is_empty() {
            return None;
        }
        Some(Plan { title: gate.title.clone().unwrap_or_else(|| gate.gate.clone()), argv: gate.argv.clone() })
    }

    /// Keeps something said to work as feedback, the evidence rules are
    /// distilled from.
    pub fn remember(text: &str, work: &Work) -> Result<Plan, PlanError> {
        let text = said(text)?;
        Ok(plan(
            "Remember",
            vec![
                "feedback".into(),
                "add".into(),
                text,
                "--work".into(),
                work.id.clone(),
                "--project".into(),
                work.project.clone(),
                "--source".into(),
                "director".into(),
            ],
        ))
    }

    /// Files feedback on no work in particular: a rule-to-be in a person's words.
    pub fn feedback(text: &str, project: Option<&str>) -> Result<Plan, PlanError> {
        let mut argv = vec!["feedback".to_string(), "add".to_string(), said(text)?];
        if let Some(project) = project {
            argv.extend(words(&["--project", project]));
        }
        Ok(plan("Feedback", argv))
    }

    /// Registers a folder as a product the director works in.
    pub fn add_product(name: &str, path: &str, runner: &str, mode: &str) -> Result<Plan, PlanError> {
        let name = name.trim();
        if name.is_empty() || path.is_empty() || runner.is_empty() {
            return Err(PlanError::Empty);
        }
        Ok(plan(
            "Add product",
            words(&["projects", "add", name, path, "--runner", runner, "--mode", mode, "--lazyspec", "n", "--json"]),
        ))
    }

    /// The name a product in a folder is offered: the folder's own name, in
    /// lower case, its words joined by hyphens.
    pub fn product_name(folder: &str) -> String {
        let base = folder.trim_end_matches('/').rsplit('/').next().unwrap_or("").to_lowercase();
        base.split(|c: char| !c.is_alphanumeric()).filter(|w| !w.is_empty()).collect::<Vec<_>>().join("-")
    }

    /// Gives work a new title; a goal renamed keeps the person's name.
    pub fn rename(id: &str, title: &str) -> Result<Plan, PlanError> {
        Ok(plan("Rename", vec!["rename".into(), id.into(), said(title)?]))
    }

    /// Opens the directory work runs in, in the editor wd finds.
    pub fn open(id: &str) -> Plan {
        plan("Open", words(&["open", id, "."]))
    }

    pub fn pause(id: &str) -> Plan {
        plan("Pause", words(&["pause", id]))
    }

    pub fn resume(id: &str, note: &str) -> Plan {
        plan("Resume", [words(&["resume", id]), optional(note)].concat())
    }

    /// Stops and drops work; `resume` brings it back, so it asks nothing.
    pub fn cancel(id: &str, reason: &str) -> Plan {
        plan("Cancel", [words(&["cancel", id]), optional(reason)].concat())
    }

    pub fn drive(id: &str) -> Plan {
        plan("Drive", words(&["drive", id]))
    }

    pub fn soft_done(id: &str) -> Plan {
        plan("Soft-done", words(&["soft-done", id]))
    }

    pub fn done(id: &str) -> Plan {
        plan("Done", words(&["done", id]))
    }

    pub fn verify(id: &str) -> Plan {
        plan("Verify", words(&["verify", id]))
    }

    pub fn acknowledge(project: &str) -> Plan {
        plan("Acknowledge review", words(&["review", "--ack", "--project", project]))
    }

    /// The write that takes this one back, when there is one: a cancel or a
    /// pause is resumed, an archive unarchived.
    pub fn undo(&self) -> Option<Plan> {
        let [verb, id, ..] = self.argv.as_slice() else { return None };
        match verb.as_str() {
            "cancel" | "pause" => Some(Plan::resume(id, "")),
            "archive" => Some(Plan::unarchive(id)),
            _ => None,
        }
    }
}

/// Quotes a word that would not read back as one word. A word holding both
/// quote characters is shown in single quotes and cannot be typed back in;
/// the argv it stands for is still exactly the one that runs.
fn quoted(word: &str) -> String {
    let plain = !word.is_empty() && word.chars().all(|c| c.is_alphanumeric() || "-_./:=@,+%".contains(c));
    if plain {
        word.to_string()
    } else if word.contains('\'') && !word.contains('"') {
        format!("\"{word}\"")
    } else {
        format!("'{word}'")
    }
}

/// Splits on whitespace; single or double quotes keep a phrase together.
/// Nothing is expanded.
pub fn split(text: &str) -> Result<Vec<String>, PlanError> {
    let mut words = Vec::new();
    let mut word = String::new();
    let mut in_word = false;
    let mut quote: Option<char> = None;
    for ch in text.chars() {
        if let Some(q) = quote {
            if ch == q {
                quote = None;
            } else {
                word.push(ch);
            }
        } else if ch == '"' || ch == '\'' {
            quote = Some(ch);
            in_word = true;
        } else if ch.is_whitespace() {
            if in_word {
                words.push(std::mem::take(&mut word));
                in_word = false;
            }
        } else {
            word.push(ch);
            in_word = true;
        }
    }
    if quote.is_some() {
        return Err(PlanError::UnclosedQuote);
    }
    if in_word {
        words.push(word);
    }
    Ok(words)
}
