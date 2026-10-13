//! The agents inspector: every agent on the work, live, each one's
//! conversation a click away.

use std::cell::RefCell;
use std::rc::Rc;

use adw::prelude::*;
use chrono::Utc;

use kit::{span, token_count, AgentCard, AgentRoster, Phase, Usage};

use crate::ui::{self, SPACE_1, SPACE_2, SPACE_3, SPACE_4};

type Chooser = Rc<dyn Fn(&AgentCard)>;

pub struct AgentsPanel {
    pub widget: gtk::Box,
    summary: gtk::Label,
    list: gtk::ListBox,
    cards: Rc<RefCell<Vec<AgentCard>>>,
    choose: Rc<RefCell<Option<Chooser>>>,
}

impl AgentsPanel {
    pub fn build() -> AgentsPanel {
        let widget = ui::vbox(SPACE_3);
        widget.add_css_class("inspector");
        let head = ui::hbox(SPACE_2);
        head.set_margin_top(SPACE_4);
        head.set_margin_start(SPACE_4);
        head.set_margin_end(SPACE_4);
        head.append(&ui::micro("Agents", &["text3"]));
        let summary = ui::line("", &["meta", "text3"]);
        summary.set_hexpand(true);
        summary.set_xalign(1.0);
        head.append(&summary);
        widget.append(&head);

        let list = gtk::ListBox::new();
        list.add_css_class("navigation-sidebar");
        list.set_selection_mode(gtk::SelectionMode::Single);
        let scroller =
            gtk::ScrolledWindow::builder().hscrollbar_policy(gtk::PolicyType::Never).vexpand(true).child(&list).build();
        widget.append(&scroller);

        let cards: Rc<RefCell<Vec<AgentCard>>> = Rc::new(RefCell::new(Vec::new()));
        let choose: Rc<RefCell<Option<Chooser>>> = Rc::new(RefCell::new(None));
        let (c, ch) = (cards.clone(), choose.clone());
        list.connect_row_activated(move |_, row| {
            let card = usize::try_from(row.index()).ok().and_then(|i| c.borrow().get(i).cloned());
            let choose = ch.borrow().clone();
            if let (Some(card), Some(choose)) = (card, choose) {
                choose(&card);
            }
        });
        AgentsPanel { widget, summary, list, cards, choose }
    }

    pub fn connect_choose(&self, f: impl Fn(&AgentCard) + 'static) {
        *self.choose.borrow_mut() = Some(Rc::new(f));
    }

    /// Shows the roster, `focused` selected.
    pub fn update(&self, roster: &AgentRoster, focused: Option<&str>) {
        let total: usize = roster.cards.iter().map(|c| 1 + c.subagents.len()).sum();
        self.summary.set_label(&if total == 0 {
            String::new()
        } else {
            format!("{} of {total} settled · {} tokens", roster.settled(), token_count(roster.tokens()))
        });
        let flat: Vec<(AgentCard, bool)> = roster
            .cards
            .iter()
            .flat_map(|c| std::iter::once((c.clone(), false)).chain(c.subagents.iter().map(|s| (s.clone(), true))))
            .collect();
        while let Some(row) = self.list.row_at_index(0) {
            self.list.remove(&row);
        }
        if flat.is_empty() {
            let none = gtk::ListBoxRow::builder().selectable(false).activatable(false).build();
            let label = ui::text("No agent has a session on this work yet.", &["callout", "text2"]);
            label.set_margin_top(SPACE_2);
            none.set_child(Some(&label));
            self.list.append(&none);
        }
        for (card, nested) in &flat {
            let row = gtk::ListBoxRow::new();
            row.set_child(Some(&card_view(card, *nested)));
            let said = [Some(card.title.as_str()), card.line.as_deref()].into_iter().flatten().collect::<Vec<_>>();
            ui::context_menu(&row, &ui::copy_menu(&said.join("\n")));
            self.list.append(&row);
            if Some(card.id.as_str()) == focused {
                self.list.select_row(Some(&row));
            }
        }
        *self.cards.borrow_mut() = flat.into_iter().map(|(c, _)| c).collect();
    }
}

fn card_view(card: &AgentCard, nested: bool) -> gtk::Box {
    let column = ui::vbox(SPACE_1);
    column.add_css_class("agent-card");
    if nested {
        column.set_margin_start(SPACE_4);
    }
    let top = ui::hbox(SPACE_2);
    top.append(&phase_mark(card.phase));
    let title = ui::line(&card.title, &["callout", "strong"]);
    title.set_hexpand(true);
    top.append(&title);
    if let Some(elapsed) = card.elapsed(Utc::now()) {
        top.append(&ui::line(&span(elapsed), &["meta", "numeric", "text2"]));
    }
    column.append(&top);
    if let Some(line) = card.line.as_deref().filter(|l| !l.is_empty()) {
        let classes: &[&str] =
            if card.phase == Phase::Asking { &["meta", "attention-text"] } else { &["meta", "text2"] };
        column.append(&ui::line(line, classes));
    }
    if let Some(usage) = &card.usage {
        column.append(&ui::line(&usage_line(usage), &["meta", "text3"]));
    }
    column
}

fn usage_line(u: &Usage) -> String {
    let mut parts = Vec::new();
    if let Some(model) = u.model.as_deref().filter(|m| !m.is_empty()) {
        parts.push(model.trim_start_matches("claude-").to_string());
    }
    parts.push(format!("{} tokens", token_count(u.tokens)));
    parts.push(format!("{} tools", u.tools));
    parts.join(" · ")
}

/// Where an agent is, as a mark: asking an orange ring, working a spinner,
/// resting a hollow circle, settled a check.
pub fn phase_mark(phase: Phase) -> gtk::Widget {
    match phase {
        Phase::Working => {
            let spinner = gtk::Spinner::new();
            spinner.start();
            spinner.set_tooltip_text(Some("Working"));
            spinner.upcast()
        }
        Phase::Asking => {
            let ring = gtk::Box::new(gtk::Orientation::Horizontal, 0);
            ring.add_css_class("dot");
            ring.add_css_class("asking");
            ring.set_valign(gtk::Align::Center);
            ring.set_tooltip_text(Some("Asking you"));
            ring.upcast()
        }
        Phase::Resting => {
            let ring = gtk::Box::new(gtk::Orientation::Horizontal, 0);
            ring.add_css_class("dot");
            ring.add_css_class("resting");
            ring.set_valign(gtk::Align::Center);
            ring.set_tooltip_text(Some("Resting"));
            ring.upcast()
        }
        Phase::Settled => {
            let check = ui::icon("object-select-symbolic", &["settled-text"]);
            check.set_tooltip_text(Some("Settled"));
            check.upcast()
        }
    }
}
