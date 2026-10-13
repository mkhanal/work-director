//! A goal's record: what was decided and done at every depth under it,
//! counted, filtered by kind and by sub-goal; a task's, as its events.

use adw::prelude::*;

use kit::Event;

use crate::turns::Turn;
use crate::ui::{self, SPACE_1};

/// The ledger's record of work, newest first.
pub(super) fn record_turns(events: &[Event]) -> Vec<Turn> {
    events
        .iter()
        .rev()
        .map(|e| {
            let e = e.clone();
            Turn::key(format!("r{}", e.id), &e).with(move || {
                let column = ui::vbox(SPACE_1);
                column.append(&ui::meta(&ui::speaker(&e.kind), Some(e.at.as_str()), None));
                column.append(&ui::folded(&e.body, 6, &["callout"]));
                column.upcast()
            })
        })
        .collect()
}
