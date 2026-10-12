//! Escape pressed twice in quick succession: the deliberate way to stop work,
//! which a single stray Escape never is.

use std::time::{Duration, Instant};

#[derive(Debug, Clone, Copy, Default)]
pub struct EscapeTaps {
    last: Option<Instant>,
}

impl EscapeTaps {
    pub const WINDOW: Duration = Duration::from_millis(800);

    /// Records a press and says whether it completes a quick pair. A press
    /// that completes one starts nothing, so a third quick press stops nothing.
    pub fn tap(&mut self, now: Instant) -> bool {
        match self.last {
            Some(last) if now.duration_since(last) <= Self::WINDOW => {
                self.last = None;
                true
            }
            _ => {
                self.last = Some(now);
                false
            }
        }
    }
}
