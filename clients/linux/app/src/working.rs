//! Work under way, live: a turning mark, who is working, what it is doing
//! now and how long it has been at it, ticking each second.

use std::time::Duration;

use adw::prelude::*;
use chrono::Utc;
use gtk::glib;

use kit::{WorkUnderWay, WorkingRow};

use crate::ui::{self, SPACE_1, SPACE_2};

/// A small turning mark, named Working; it stands still where the system
/// turns animations off, as every GTK animation does.
pub fn mark() -> gtk::Spinner {
    let spinner = gtk::Spinner::new();
    spinner.start();
    spinner.add_css_class("working-mark");
    spinner.set_valign(gtk::Align::Center);
    spinner.set_tooltip_text(Some("Working"));
    spinner.update_property(&[gtk::accessible::Property::Label("Working")]);
    spinner
}

/// The rows at the end of a conversation, one per piece of work under way,
/// then how many more there are.
pub fn rows(under_way: &WorkUnderWay) -> gtk::Widget {
    let column = ui::vbox(SPACE_1);
    column.add_css_class("under-way");
    let mut clocks = Vec::new();
    for row in &under_way.rows {
        let (line, clock) = row_view(row);
        column.append(&line);
        clocks.extend(clock);
    }
    if under_way.more > 0 {
        let more = ui::line(&format!("+{} more", under_way.more), &["meta", "text3"]);
        more.set_margin_start(24);
        column.append(&more);
    }
    let weak = column.downgrade();
    glib::timeout_add_local(Duration::from_secs(1), move || {
        if weak.upgrade().is_none() {
            return glib::ControlFlow::Break;
        }
        tick(&clocks);
        glib::ControlFlow::Continue
    });
    column.upcast()
}

type Clock = (gtk::Label, WorkingRow);

fn tick(clocks: &[Clock]) {
    let now = Utc::now();
    for (label, row) in clocks {
        label.set_label(&row.elapsed(now).unwrap_or_default());
    }
}

fn row_view(row: &WorkingRow) -> (gtk::Box, Option<Clock>) {
    let line = ui::hbox(SPACE_2);
    line.append(&mark());
    let who = ui::line(&row.who, &["callout", "strong"]);
    who.set_max_width_chars(32);
    line.append(&who);
    let doing = ui::line(&row.doing, &["callout", "text2"]);
    doing.set_hexpand(true);
    doing.set_tooltip_text(Some(&row.doing));
    line.append(&doing);
    let clock = row.since.map(|_| {
        let elapsed = ui::line(&row.elapsed(Utc::now()).unwrap_or_default(), &["meta", "numeric", "text3"]);
        elapsed.set_xalign(1.0);
        line.append(&elapsed);
        (elapsed, row.clone())
    });
    line.update_property(&[gtk::accessible::Property::Label(&format!("{} · {}", row.who, row.doing))]);
    (line, clock)
}
