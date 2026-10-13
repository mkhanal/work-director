//! A goal's record as wd serves it: what was decided and done at every depth
//! under it, newest first, with its counts.

use serde::Deserialize;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum RecordKind {
    Decision,
    Finish,
    Course,
    Question,
    Answer,
    Judged,
    Report,
    Delivery,
    Chore,
    Superseded,
    Asked,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum RecordBy {
    You,
    Director,
}

impl RecordBy {
    pub fn name(self) -> &'static str {
        match self {
            RecordBy::You => "you",
            RecordBy::Director => "director",
        }
    }
}

/// One thing on the record. `event` is the ledger event it was read from, 0
/// for the goal's first ask.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct RecordEntry {
    pub event: i64,
    pub at: String,
    pub kind: RecordKind,
    pub what: String,
    #[serde(rename = "where")]
    pub place: String,
    pub by: RecordBy,
}

/// The record's counts; `yours` are the decisions the person made,
/// `corrections` the course checks that changed course.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default, Deserialize)]
pub struct RecordSummary {
    pub decisions: i64,
    pub yours: i64,
    pub course: i64,
    pub corrections: i64,
    pub questions: i64,
    pub answered_by_director: i64,
    pub chores: i64,
    pub judged: i64,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct RecordView {
    pub summary: RecordSummary,
    pub entries: Vec<RecordEntry>,
}
