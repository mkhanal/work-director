//! A work item's session as `wd serve --stdio` serves it: what it was told,
//! what it said and thought, the tools it called and the questions it asked.

use serde::Deserialize;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum StepKind {
    Prompt,
    Text,
    Thinking,
    Tool,
    Question,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct ToolCall {
    pub id: String,
    pub name: String,
    pub summary: String,
    pub input: String,
    /// None while the tool is still running.
    pub result: Option<String>,
    pub failed: bool,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct QuestionOption {
    pub label: String,
    pub description: String,
    /// What the option is shown as when its label is terse, as a finish
    /// line's is; the label is still what is answered.
    #[serde(skip)]
    pub title: Option<String>,
}

impl QuestionOption {
    /// What the option is shown and announced as: its title, else its label
    /// without the recommended mark.
    pub fn shown(&self) -> &str {
        self.title.as_deref().unwrap_or_else(|| unmarked(&self.label))
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct QuestionItem {
    pub header: String,
    pub question: String,
    pub multi: bool,
    pub options: Vec<QuestionOption>,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Question {
    pub id: String,
    pub items: Vec<QuestionItem>,
    /// One answer per item once answered.
    pub answers: Option<Vec<String>>,
    /// What the session was told instead, when that was not a set of answers.
    pub reply: Option<String>,
}

impl Question {
    pub fn pending(&self) -> bool {
        self.answers.is_none() && self.reply.is_none()
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Step {
    pub kind: StepKind,
    pub at: Option<String>,
    pub text: Option<String>,
    pub tool: Option<ToolCall>,
    pub question: Option<Question>,
}

impl Step {
    /// Whether the step is final: a tool call with its result, a question with
    /// its answer, anything else as soon as it is said.
    pub fn settled(&self) -> bool {
        match self.kind {
            StepKind::Tool => self.tool.as_ref().is_some_and(|t| t.result.is_some()),
            StepKind::Question => self.question.as_ref().is_some_and(|q| !q.pending()),
            _ => true,
        }
    }

    pub fn text(&self) -> &str {
        self.text.as_deref().unwrap_or("")
    }

    /// The step in one line, as the agents panel shows what an agent is doing.
    pub fn line(&self) -> String {
        match self.kind {
            StepKind::Tool => self
                .tool
                .as_ref()
                .map(|t| {
                    [t.name.as_str(), t.summary.as_str()]
                        .iter()
                        .filter(|s| !s.is_empty())
                        .copied()
                        .collect::<Vec<_>>()
                        .join(" · ")
                })
                .unwrap_or_default(),
            StepKind::Question => {
                self.question.as_ref().and_then(|q| q.items.first()).map(|i| i.question.clone()).unwrap_or_default()
            }
            _ => self.text().lines().next().unwrap_or("").to_string(),
        }
    }
}

/// What a session reports about itself: working, idle, waiting on a person,
/// gone, or no session at all.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SessionStatus {
    Running,
    Idle,
    Waiting,
    Stopped,
    Exited,
    Unknown,
    #[serde(rename = "none")]
    NoSession,
}

impl SessionStatus {
    /// Whether the session's process is up, and so can be stopped.
    pub fn live(self) -> bool {
        matches!(self, SessionStatus::Running | SessionStatus::Idle | SessionStatus::Waiting)
    }
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct ConversationPage {
    pub work: String,
    pub runner: Option<String>,
    pub session: Option<String>,
    pub status: SessionStatus,
    pub from: usize,
    pub total: usize,
    pub entries: Vec<Step>,
    pub asking: Option<Question>,
    pub agent: Option<String>,
    pub usage: Option<Usage>,
    pub subagents: Option<Vec<Subagent>>,
}

/// What a session has spent and done.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Usage {
    pub model: Option<String>,
    pub tokens: i64,
    pub tools: i64,
    pub started: Option<String>,
    pub last: Option<String>,
}

/// An agent a session started for part of its work.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Subagent {
    pub id: String,
    pub description: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub done: bool,
    pub said: Option<String>,
    pub usage: Usage,
}

/// What a task's session is doing now, in a line.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct Activity {
    pub now: Option<String>,
    pub kind: Option<StepKind>,
    pub at: Option<String>,
    pub asking: Option<Question>,
    pub error: Option<String>,
    pub status: Option<SessionStatus>,
    pub usage: Option<Usage>,
    pub subagents: Option<Vec<Subagent>>,
}

/// One row of a conversation as it reads: a step on its own, or a run of tool
/// calls folded into one row.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub enum ThreadRow {
    Step { index: usize, step: Box<Step> },
    Tools { first: usize, calls: Vec<ToolCall> },
}

impl ThreadRow {
    pub fn index(&self) -> usize {
        match self {
            ThreadRow::Step { index, .. } => *index,
            ThreadRow::Tools { first, .. } => *first,
        }
    }

    /// Steps in conversation order, with every run of two or more tool calls
    /// folded into one row: the calls between two things said are one piece of
    /// work to a reader.
    pub fn rows(steps: &[Step]) -> Vec<ThreadRow> {
        let mut out = Vec::new();
        let mut run: Vec<ToolCall> = Vec::new();
        let mut run_start = 0;
        let flush = |run: &mut Vec<ToolCall>, run_start: usize, out: &mut Vec<ThreadRow>| {
            match run.len() {
                0 => {}
                1 => out.push(ThreadRow::Step { index: run_start, step: Box::new(steps[run_start].clone()) }),
                _ => out.push(ThreadRow::Tools { first: run_start, calls: std::mem::take(run) }),
            }
            run.clear();
        };
        for (i, step) in steps.iter().enumerate() {
            if let (StepKind::Tool, Some(tool)) = (step.kind, &step.tool) {
                if run.is_empty() {
                    run_start = i;
                }
                run.push(tool.clone());
                continue;
            }
            flush(&mut run, run_start, &mut out);
            out.push(ThreadRow::Step { index: i, step: Box::new(step.clone()) });
        }
        flush(&mut run, run_start, &mut out);
        out
    }
}

/// A person's answer to a question while they pick it: the options chosen
/// for each item and anything typed instead.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AnswerDraft {
    pub question: Question,
    picked: Vec<Vec<String>>,
    typed: Vec<String>,
}

/// What an item left unanswered is sent as.
pub const NO_ANSWER: &str = "(no answer)";

const RECOMMENDED: &str = " (recommended)";

/// An option's label without the mark that recommends it.
pub fn unmarked(label: &str) -> &str {
    let cut = label.len().saturating_sub(RECOMMENDED.len());
    if label.is_char_boundary(cut) && label[cut..].eq_ignore_ascii_case(RECOMMENDED) {
        label[..cut].trim_end()
    } else {
        label
    }
}

impl AnswerDraft {
    pub fn new(question: Question) -> AnswerDraft {
        let n = question.items.len();
        AnswerDraft { question, picked: vec![Vec::new(); n], typed: vec![String::new(); n] }
    }

    /// Picks an option: the only one for a single choice, one more for a
    /// multiple. Picking it again takes it back.
    pub fn toggle(&mut self, label: &str, item: usize) {
        let picked = &mut self.picked[item];
        if self.question.items[item].multi {
            if let Some(at) = picked.iter().position(|l| l == label) {
                picked.remove(at);
            } else {
                picked.push(label.to_string());
            }
        } else {
            let again = picked.len() == 1 && picked[0] == label;
            picked.clear();
            if !again {
                picked.push(label.to_string());
            }
            self.typed[item].clear();
        }
    }

    pub fn is_picked(&self, label: &str, item: usize) -> bool {
        self.picked[item].iter().any(|l| l == label)
    }

    pub fn set_typed(&mut self, item: usize, text: &str) {
        self.typed[item] = text.to_string();
    }

    pub fn typed(&self, item: usize) -> &str {
        &self.typed[item]
    }

    /// Each item's answer: its picks in the order offered, without a
    /// recommended mark, then anything typed; on a single choice, typed words
    /// replace a pick. An item left without one is `(no answer)`; None while
    /// no item has one.
    pub fn answers(&self) -> Option<Vec<String>> {
        let answers: Vec<Option<String>> = self
            .question
            .items
            .iter()
            .enumerate()
            .map(|(i, item)| {
                let words = self.typed[i].trim();
                let mut parts: Vec<&str> = item
                    .options
                    .iter()
                    .map(|o| o.label.as_str())
                    .filter(|l| self.picked[i].iter().any(|p| p == l))
                    .map(unmarked)
                    .collect();
                if !words.is_empty() {
                    if item.multi {
                        parts.push(words);
                    } else {
                        parts = vec![words];
                    }
                }
                (!parts.is_empty()).then(|| parts.join(", "))
            })
            .collect();
        answers
            .iter()
            .any(Option::is_some)
            .then(|| answers.into_iter().map(|a| a.unwrap_or_else(|| NO_ANSWER.to_string())).collect())
    }
}
