//! Ledger times, as wd writes them (RFC 3339) and as a reader wants them.

use chrono::{DateTime, Local, Utc};

pub fn instant(stamp: &str) -> Option<DateTime<Utc>> {
    DateTime::parse_from_rfc3339(stamp).ok().map(|t| t.with_timezone(&Utc))
}

/// A time as a reader wants it: the time today, the date and time otherwise.
/// A stamp that is not a time reads as written.
pub fn when(stamp: &str) -> String {
    let Some(t) = instant(stamp) else { return stamp.to_string() };
    let local = t.with_timezone(&Local);
    if local.date_naive() == Local::now().date_naive() {
        local.format("%H:%M").to_string()
    } else {
        local.format("%-d %b %H:%M").to_string()
    }
}

/// How long ago, in the fewest characters: now, 5m, 3h, 2d.
pub fn ago(stamp: &str, now: DateTime<Utc>) -> String {
    let Some(t) = instant(stamp) else { return String::new() };
    let s = (now - t).num_seconds();
    match s {
        ..60 => "now".to_string(),
        ..3600 => format!("{}m", s / 60),
        ..86400 => format!("{}h", s / 3600),
        _ => format!("{}d", s / 86400),
    }
}

/// Seconds between two stamps, when both are times.
pub fn between(earlier: &str, later: &str) -> Option<i64> {
    Some((instant(later)? - instant(earlier)?).num_seconds())
}
