//! Whether a conversation keeps its newest message in view: it does while the
//! reader is at the bottom, stops when they scroll up to read, and starts
//! again once they are back.

/// How far above the very bottom still counts as at the bottom, so a reader a
/// few pixels short of it is not left behind.
pub const SLACK: f64 = 48.0;

pub fn at_bottom(offset: f64, visible: f64, content: f64) -> bool {
    content <= visible || offset + visible >= content - SLACK
}
