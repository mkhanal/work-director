//! A goal's record: what was decided and done at every depth under it,
//! counted, filtered by kind and by sub-goal; a task's, as its events.

use std::rc::{Rc, Weak};

use adw::prelude::*;

use kit::{Event, GoalDetail, RecordEntry, RecordFilter, RecordView};

use super::WorkPage;
use crate::turns::Turn;
use crate::ui::{self, SPACE_1, SPACE_3};

impl WorkPage {
    /// The summary cards, the filters and the table.
    pub(super) fn record_view_turns(
        self: &Rc<Self>,
        g: &GoalDetail,
        record: &RecordView,
        filter: RecordFilter,
        place: Option<&str>,
    ) -> Vec<Turn> {
        let mut turns = Vec::new();
        let cards = record.summary.cards();
        turns.push(Turn::key("r-sum", &cards).with(move || summary(&cards)));
        let places: Vec<String> = g.tree.iter().map(|n| n.work.title.clone()).collect();
        let chosen = place.map(str::to_string);
        let weak = self.me.clone();
        turns.push(
            Turn::key("r-filters", &(&places, filter, &chosen))
                .with(move || filters(&places, filter, chosen.as_deref(), weak)),
        );
        let rows: Vec<RecordEntry> = record.shown(filter, place).into_iter().cloned().collect();
        turns.push(Turn::key("r-table", &rows).with(move || table(&rows)));
        turns
    }
}

fn summary(cards: &[(i64, String); 5]) -> gtk::Widget {
    let grid = gtk::Grid::builder().column_spacing(10).column_homogeneous(true).build();
    for (i, (n, what)) in cards.iter().enumerate() {
        let card = ui::vbox(0);
        card.add_css_class("sum-card");
        card.append(&ui::line(&n.to_string(), &["number"]));
        card.append(&ui::text(what, &["meta", "text3"]));
        grid.attach(&card, i as i32, 0, 1, 1);
    }
    grid.upcast()
}

/// The kind chips and the sub-goal picker.
fn filters(places: &[String], filter: RecordFilter, place: Option<&str>, page: Weak<WorkPage>) -> gtk::Widget {
    let row = ui::hbox(6);
    let (scroller, chips) = ui::chip_row();
    row.append(&scroller);
    let mut first: Option<gtk::ToggleButton> = None;
    for f in RecordFilter::ALL {
        let chip = ui::fchip(f.label(), f == filter, first.as_ref());
        if first.is_none() {
            first = Some(chip.clone());
        }
        let page = page.clone();
        chip.connect_toggled(move |c| {
            if c.is_active() {
                if let Some(page) = page.upgrade() {
                    page.set_record_filter(f);
                }
            }
        });
        chips.append(&chip);
    }
    let mut names = vec!["Sub-goal: all".to_string()];
    names.extend(places.iter().map(|p| format!("Sub-goal: {p}")));
    let strs: Vec<&str> = names.iter().map(String::as_str).collect();
    let picker = gtk::DropDown::from_strings(&strs);
    picker.add_css_class("flat");
    picker.add_css_class("meta");
    let at = place.and_then(|p| places.iter().position(|q| q == p)).map_or(0, |i| i + 1);
    picker.set_selected(at as u32);
    let places = places.to_vec();
    picker.connect_selected_notify(move |d| {
        let chosen = usize::try_from(d.selected()).ok().and_then(|i| i.checked_sub(1)).and_then(|i| places.get(i));
        if let Some(page) = page.upgrade() {
            page.set_record_place(chosen.cloned());
        }
    });
    picker.set_valign(gtk::Align::Start);
    row.append(&picker);
    row.upcast()
}

/// Time, kind, what, where and by whom; a day line where the day changes.
fn table(rows: &[RecordEntry]) -> gtk::Widget {
    let column = ui::vbox(0);
    let head = cells(
        ui::line("Time", &["meta", "text3"]).upcast(),
        ui::line("Kind", &["meta", "text3"]).upcast(),
        ui::line("What", &["meta", "text3"]),
        ui::line("Where", &["meta", "text3"]),
        "By",
    );
    head.add_css_class("record-head");
    column.append(&head);
    if rows.is_empty() {
        let none = ui::text("Nothing recorded here yet.", &["callout", "text3"]);
        none.set_margin_top(SPACE_3);
        column.append(&none);
    }
    let mut day = String::new();
    for e in rows {
        let today = kit::time::day(&e.at);
        if today != day {
            let line = ui::line(&today, &["record-day"]);
            column.append(&line);
            day = today;
        }
        let (text, tone) = e.badge();
        let row = cells(
            ui::line(&clock(&e.at), &["meta", "text3", "numeric"]).upcast(),
            ui::badge(&text, tone).upcast(),
            ui::said(&kit::sentence(&e.what), &["callout"]),
            ui::text(&e.place, &["meta", "text2"]),
            e.by.name(),
        );
        row.add_css_class("record-row");
        column.append(&row);
    }
    column.upcast()
}

/// A time as the table reads it: the hour and minute.
fn clock(at: &str) -> String {
    kit::time::instant(at)
        .map(|t| t.with_timezone(&chrono::Local).format("%H:%M").to_string())
        .unwrap_or_else(|| at.to_string())
}

/// One row in the table's columns: 52, 96, the rest, 220 and 90 wide.
fn cells(when: gtk::Widget, kind: gtk::Widget, what: gtk::Label, place: gtk::Label, by: &str) -> gtk::Box {
    let row = ui::hbox(SPACE_3);
    when.set_size_request(52, -1);
    when.set_valign(gtk::Align::Start);
    kind.set_valign(gtk::Align::Start);
    kind.set_halign(gtk::Align::Start);
    let kind_cell = ui::hbox(0);
    kind_cell.set_size_request(96, -1);
    kind_cell.append(&kind);
    what.set_hexpand(true);
    what.set_valign(gtk::Align::Start);
    place.set_size_request(220, -1);
    place.set_width_chars(30);
    place.set_max_width_chars(30);
    place.set_valign(gtk::Align::Start);
    let by = ui::line(by, &["meta", "text3"]);
    by.set_size_request(90, -1);
    by.set_xalign(1.0);
    by.set_valign(gtk::Align::Start);
    row.append(&when);
    row.append(&kind_cell);
    row.append(&what);
    row.append(&place);
    row.append(&by);
    row
}

/// The ledger's record of a task, newest first.
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
