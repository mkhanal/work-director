//! A goal's record as wd serves it: what was decided and done at every depth
//! under it, newest first, with its counts.

use serde::Deserialize;

use crate::look::Tone;

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

/// What the record is narrowed to by kind.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum RecordFilter {
    Everything,
    Decisions,
    Course,
    Questions,
    Reports,
    Chores,
    Delivery,
}

impl RecordFilter {
    pub const ALL: [RecordFilter; 7] = [
        RecordFilter::Everything,
        RecordFilter::Decisions,
        RecordFilter::Course,
        RecordFilter::Questions,
        RecordFilter::Reports,
        RecordFilter::Chores,
        RecordFilter::Delivery,
    ];

    pub fn label(self) -> &'static str {
        match self {
            RecordFilter::Everything => "Everything",
            RecordFilter::Decisions => "Decisions",
            RecordFilter::Course => "Direction checks",
            RecordFilter::Questions => "Questions",
            RecordFilter::Reports => "Reports",
            RecordFilter::Chores => "Housekeeping",
            RecordFilter::Delivery => "Delivery",
        }
    }

    pub fn holds(self, kind: RecordKind) -> bool {
        use RecordKind as K;
        match self {
            RecordFilter::Everything => true,
            RecordFilter::Decisions => matches!(kind, K::Decision | K::Finish | K::Asked),
            RecordFilter::Course => matches!(kind, K::Course | K::Superseded),
            RecordFilter::Questions => matches!(kind, K::Question | K::Answer),
            RecordFilter::Reports => matches!(kind, K::Report | K::Judged),
            RecordFilter::Chores => kind == K::Chore,
            RecordFilter::Delivery => kind == K::Delivery,
        }
    }
}

impl RecordEntry {
    /// Whether it was recorded on the sub-goal at `path` or under it.
    pub fn under(&self, path: &str) -> bool {
        self.place == path || self.place.strip_prefix(path).is_some_and(|rest| rest.starts_with(" › "))
    }

    /// Its kind as a badge.
    pub fn badge(&self) -> (String, Tone) {
        let verdict = self.what.split(':').next().unwrap_or("");
        let (text, tone) = match self.kind {
            RecordKind::Decision => ("Decision", Tone::Decision),
            RecordKind::Finish => ("Finish line", Tone::Decision),
            RecordKind::Asked => ("Asked", Tone::Decision),
            RecordKind::Course => match verdict {
                "on course" => ("On course", Tone::Settled),
                "correct" => ("Corrected", Tone::Live),
                "pivot" => ("Pivot", Tone::Attention),
                _ => ("Course", Tone::Waiting),
            },
            RecordKind::Superseded => (crate::words::REPLACED, Tone::Waiting),
            RecordKind::Question => ("Question", Tone::Attention),
            RecordKind::Answer => ("Answered", Tone::Settled),
            RecordKind::Judged if verdict == "met" => (crate::words::checked(verdict), Tone::Settled),
            RecordKind::Judged => (crate::words::checked(verdict), Tone::Attention),
            RecordKind::Report => ("Report", Tone::Live),
            RecordKind::Chore => ("Housekeeping", Tone::Waiting),
            RecordKind::Delivery => ("Delivered", Tone::Settled),
        };
        (text.to_string(), tone)
    }
}

impl RecordView {
    /// The entries of one kind, on one sub-goal or under it when one is
    /// named, newest first.
    pub fn shown(&self, filter: RecordFilter, path: Option<&str>) -> Vec<&RecordEntry> {
        self.entries.iter().filter(|e| filter.holds(e.kind) && path.is_none_or(|p| e.under(p))).collect()
    }
}

impl RecordSummary {
    /// The five summary cards: a number and what it counts.
    pub fn cards(&self) -> [(i64, String); 5] {
        let plural = |n: i64, one: &str, many: &str| if n == 1 { one.to_string() } else { many.to_string() };
        [
            (self.decisions, format!("{} · {} yours", plural(self.decisions, "decision", "decisions"), self.yours)),
            (
                self.course,
                format!(
                    "{} · {} {}",
                    plural(self.course, "direction check", "direction checks"),
                    self.corrections,
                    plural(self.corrections, "correction", "corrections")
                ),
            ),
            (
                self.questions,
                format!(
                    "{} · {} answered by the director",
                    plural(self.questions, "question", "questions"),
                    self.answered_by_director
                ),
            ),
            (
                self.chores,
                format!("{} the director ran", plural(self.chores, "housekeeping task", "housekeeping tasks")),
            ),
            (self.judged, plural(self.judged, "report checked", "reports checked")),
        ]
    }
}
