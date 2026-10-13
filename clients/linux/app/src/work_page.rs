//! One goal or task, read again whenever the board is. A goal reads as its
//! overview, its conversation and its record; a task leads with its
//! session's conversation, its record one toggle away. The agents inspector
//! sits beside it, and choosing an agent shows its conversation in the centre.

mod change;
mod conversation;
mod overview;
mod proposal;
mod record;

use std::cell::{Cell, RefCell};
use std::rc::{Rc, Weak};
use std::time::{Duration, Instant};

use adw::prelude::*;
use gtk::{gdk, glib};

use std::collections::HashMap;

use kit::{
    follow, AgentCard, AgentRoster, ChannelError, EscapeTaps, Event, Feed, Gate, GoalDetail, Plan, RecordFilter,
    RecordView, SessionStatus, Step, ThreadFilter, Watch, Work, WorkState,
};

use self::conversation::director_answers;
use self::record::record_turns;
use crate::agents::AgentsPanel;
use crate::app::{work_menu, App, Place};
use crate::composer::Composer;
use crate::side_panel::SidePanel;
use crate::turns::{Turn, Turns};
use crate::ui::{self, SPACE_1, SPACE_2, SPACE_3, SPACE_4, SPACE_6};

/// Consecutive turns by one speaker within this many seconds read as one.
const MERGE_SECONDS: i64 = 120;

enum Loaded {
    Goal(Box<GoalDetail>, Box<RecordView>),
    Work(Box<Work>, Vec<Event>),
}

impl Loaded {
    fn work(&self) -> &Work {
        match self {
            Loaded::Goal(g, _) => &g.goal,
            Loaded::Work(w, _) => w,
        }
    }
}

/// The views of a work page. A goal has all three; a task its conversation and record.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Tab {
    Overview,
    Conversation,
    Record,
}

impl Tab {
    pub fn name(self) -> &'static str {
        match self {
            Tab::Overview => "overview",
            Tab::Conversation => "conversation",
            Tab::Record => "record",
        }
    }

    pub fn named(name: &str) -> Option<Tab> {
        [Tab::Overview, Tab::Conversation, Tab::Record].into_iter().find(|t| t.name() == name)
    }
}

struct State {
    loaded: Option<Loaded>,
    failure: Option<String>,
    gate: Option<Gate>,
    /// A task's own session, as its page reads it.
    feed: Option<Rc<Feed>>,
    /// The agent whose conversation the centre shows instead of this page's own.
    focus: Option<AgentCard>,
    focus_feed: Option<Rc<Feed>>,
    /// The view chosen on this page; until one is, a goal opens on the last
    /// view chosen on any goal, a task on its conversation.
    tab: Option<Tab>,
    filter: ThreadFilter,
    /// Sub-goal rows whose steps were folded open or closed by hand.
    folds: HashMap<String, bool>,
    /// What the record is narrowed to: a kind, and a sub-goal's path.
    record_filter: RecordFilter,
    record_place: Option<String>,
    /// The sub-goal a goal's message goes to; none for the whole goal.
    to: Option<String>,
    /// Who the composer offers to send to, as last offered.
    addressees: Vec<(String, String)>,
    /// Where each of your messages is, to jump back to: what you said, and its turn.
    yours: Vec<(String, String)>,
}

struct Header {
    widget: gtk::Box,
    crumbs: gtk::Box,
    titles: gtk::Stack,
    title: gtk::Label,
    entry: gtk::Entry,
    /// The state badge beside the title, and the agent's details in focus.
    meta: gtk::Box,
    problem: gtk::Label,
    tabs: gtk::Box,
    overview: gtk::ToggleButton,
    conversation: gtk::ToggleButton,
    record: gtk::ToggleButton,
    /// What the view shown is narrowed by: the conversation's or the record's chips.
    filters: gtk::Box,
}

pub struct WorkPage {
    app: App,
    id: String,
    me: Weak<WorkPage>,
    pub widget: adw::ToolbarView,
    /// The agents panel beside the page.
    _side: Rc<SidePanel>,
    scroller: gtk::ScrolledWindow,
    column: gtk::Box,
    header: Header,
    turns: Turns,
    composer: Rc<Composer>,
    composer_box: gtk::Box,
    jump: gtk::Button,
    gate: gtk::Button,
    pause: gtk::Button,
    agents_toggle: gtk::ToggleButton,
    actions: gtk::MenuButton,
    yours: gtk::MenuButton,
    yours_list: gtk::ListBox,
    agents: AgentsPanel,
    state: RefCell<State>,
    at_bottom: Cell<bool>,
    escape: Cell<EscapeTaps>,
    loading: Cell<bool>,
    stale: Cell<bool>,
    /// Set while the page marks the view it shows, so the tabs do not take it as a choice.
    syncing: Cell<bool>,
    watch: RefCell<Option<Watch>>,
    /// The conversation's chips as last shown, and which was chosen.
    shown_filters: RefCell<Option<(Vec<kit::FilterChip>, ThreadFilter)>>,
    /// What the Pause or Resume button runs, for the work as last read.
    pause_plan: RefCell<Option<Plan>>,
    /// What the director last said of each question left to it, by the work
    /// asking: what it decided, or why it declined.
    director_said: RefCell<HashMap<String, kit::DirectorAnswer>>,
}

impl WorkPage {
    pub fn new(app: &App, id: &str) -> Rc<WorkPage> {
        let page = Rc::new_cyclic(|me: &Weak<WorkPage>| build(app, id, me.clone()));
        page.wire();
        page.app.spawn(Rc::clone(&page).load());
        let weak = Rc::downgrade(&page);
        page.app.spawn(async move {
            let mut tick: u32 = 0;
            loop {
                glib::timeout_future(Duration::from_millis(1500)).await;
                let Some(page) = weak.upgrade() else { return };
                tick = tick.wrapping_add(1);
                page.beat(tick).await;
            }
        });
        page
    }

    pub fn id(&self) -> &str {
        &self.id
    }

    /// Makes the title an inline field; Enter renames, Escape cancels.
    pub fn start_rename(&self) {
        self.header.entry.set_text(&self.header.title.label());
        self.header.titles.set_visible_child_name("entry");
        self.header.entry.grab_focus();
    }

    fn wire(self: &Rc<Self>) {
        let weak = self.me.clone();
        self.composer.to.connect_selected_notify(move |d| {
            let Some(page) = weak.upgrade().filter(|p| !p.syncing.get()) else { return };
            let mut state = page.state.borrow_mut();
            let chosen = usize::try_from(d.selected()).ok().and_then(|i| state.addressees.get(i)).map(|a| a.0.clone());
            state.to = chosen.filter(|id| *id != page.id);
        });

        let weak = self.me.clone();
        *self.watch.borrow_mut() = Some(self.app.store.watch(move |change| {
            let Some(page) = weak.upgrade() else { return };
            match change {
                kit::Change::Board => page.app.spawn(Rc::clone(&page).load()),
                kit::Change::Outbox | kit::Change::Writes => page.render(),
                kit::Change::Projects | kit::Change::Failure => {}
            }
        }));

        let adjustment = self.scroller.vadjustment();
        let weak = self.me.clone();
        adjustment.connect_value_changed(move |a| {
            if let Some(page) = weak.upgrade() {
                let bottom = follow::at_bottom(a.value(), a.page_size(), a.upper());
                page.at_bottom.set(bottom);
                page.jump.set_visible(!bottom && page.following());
            }
        });
        let weak = self.me.clone();
        adjustment.connect_changed(move |a| {
            if let Some(page) = weak.upgrade() {
                if !page.following() {
                    page.jump.set_visible(false);
                } else if page.at_bottom.get() {
                    // This runs inside the viewport's allocation, which would
                    // not move its child for a value set now; set it after.
                    // By then the view shown may no longer follow.
                    let (a, weak) = (a.clone(), page.me.clone());
                    glib::idle_add_local_once(move || {
                        if weak.upgrade().is_some_and(|p| p.following()) {
                            a.set_value(a.upper() - a.page_size());
                        }
                    });
                } else {
                    page.jump.set_visible(true);
                }
            }
        });
        let weak = self.me.clone();
        self.jump.connect_clicked(move |_| {
            if let Some(page) = weak.upgrade() {
                page.to_bottom();
            }
        });

        let weak = self.me.clone();
        self.composer.connect_send(move |text| {
            if let Some(page) = weak.upgrade() {
                page.send(text);
            }
        });
        let weak = self.me.clone();
        self.composer.connect_stop(move || {
            if let Some(page) = weak.upgrade() {
                page.stop();
            }
        });

        let weak = self.me.clone();
        self.agents.connect_choose(move |card| {
            if let Some(page) = weak.upgrade() {
                page.choose(card.clone());
            }
        });

        let weak = self.me.clone();
        self.gate.connect_clicked(move |_| {
            let Some(page) = weak.upgrade() else { return };
            let plan = page.state.borrow().gate.as_ref().and_then(Plan::passing);
            if let Some(plan) = plan {
                page.app.run(plan);
            }
        });
        let weak = self.me.clone();
        self.pause.connect_clicked(move |_| {
            let Some(page) = weak.upgrade() else { return };
            let plan = page.pause_plan.borrow().clone();
            if let Some(plan) = plan {
                page.app.run(plan);
            }
        });

        let weak = self.me.clone();
        self.yours_list.connect_row_activated(move |_, row| {
            let Some(page) = weak.upgrade() else { return };
            let turn =
                usize::try_from(row.index()).ok().and_then(|i| page.state.borrow().yours.get(i).map(|y| y.1.clone()));
            if let Some(turn) = turn {
                page.yours.popdown();
                page.scroll_to(&turn);
            }
        });

        let weak = self.me.clone();
        self.header.entry.connect_activate(move |entry| {
            let Some(page) = weak.upgrade() else { return };
            page.header.titles.set_visible_child_name("title");
            match Plan::rename(&page.id, &entry.text()) {
                Ok(plan) => page.app.run(plan),
                Err(e) => page.app.complain(&e.to_string()),
            }
        });
        let keys = gtk::EventControllerKey::new();
        let weak = self.me.clone();
        keys.connect_key_pressed(move |_, key, _, _| {
            if key != gdk::Key::Escape {
                return glib::Propagation::Proceed;
            }
            if let Some(page) = weak.upgrade() {
                page.header.titles.set_visible_child_name("title");
            }
            glib::Propagation::Stop
        });
        self.header.entry.add_controller(keys);
        let click = gtk::GestureClick::new();
        let weak = self.me.clone();
        click.connect_pressed(move |_, presses, _, _| {
            if presses == 2 {
                if let Some(page) = weak.upgrade() {
                    if page.state.borrow().focus.is_none() {
                        page.start_rename();
                    }
                }
            }
        });
        self.header.title.add_controller(click);

        let h = &self.header;
        for (button, tab) in
            [(&h.overview, Tab::Overview), (&h.conversation, Tab::Conversation), (&h.record, Tab::Record)]
        {
            let weak = self.me.clone();
            button.connect_toggled(move |b| {
                if b.is_active() {
                    if let Some(page) = weak.upgrade().filter(|p| !p.syncing.get()) {
                        page.show(tab);
                    }
                }
            });
        }

        // Escape twice in quick succession stops the work on screen.
        let keys = gtk::EventControllerKey::new();
        keys.set_propagation_phase(gtk::PropagationPhase::Capture);
        let weak = self.me.clone();
        keys.connect_key_pressed(move |_, key, _, _| {
            if key != gdk::Key::Escape {
                return glib::Propagation::Proceed;
            }
            if let Some(page) = weak.upgrade() {
                let mut taps = page.escape.get();
                let twice = taps.tap(Instant::now());
                page.escape.set(taps);
                if twice && page.working() {
                    page.stop();
                }
            }
            glib::Propagation::Proceed
        });
        self.widget.add_controller(keys);
    }

    /// Reads the work again. Reads asked for while one runs coalesce into one more.
    async fn load(self: Rc<Self>) {
        if self.loading.get() {
            self.stale.set(true);
            return;
        }
        self.loading.set(true);
        loop {
            self.stale.set(false);
            let read = self.read().await;
            {
                let mut state = self.state.borrow_mut();
                match read {
                    Ok((loaded, gate)) => {
                        let work = loaded.work();
                        if !work.kind.is_goal() && work.has_session() && state.feed.is_none() {
                            state.feed = Some(Rc::new(Feed::new(Rc::clone(&self.app.store), &self.id, None)));
                        }
                        state.loaded = Some(loaded);
                        state.gate = Some(gate);
                        state.failure = None;
                    }
                    Err(e) => state.failure = Some(e.to_string()),
                }
            }
            let fresh_feed = {
                let state = self.state.borrow();
                state.feed.clone().filter(|f| !f.state().loaded)
            };
            if let Some(feed) = fresh_feed {
                feed.read().await;
            }
            self.render();
            let chain = match &self.state.borrow().loaded {
                Some(Loaded::Goal(g, _)) => g.chain.clone(),
                _ => Vec::new(),
            };
            self.app.show_path(&chain);
            if !self.stale.get() {
                break;
            }
        }
        self.loading.set(false);
    }

    async fn read(&self) -> Result<(Loaded, Gate), ChannelError> {
        let store = &self.app.store;
        let work = store.work(&self.id).await?;
        let gate = store.gate(&self.id).await?;
        let loaded = if work.kind.is_goal() {
            Loaded::Goal(Box::new(store.goal(&self.id).await?), Box::new(store.record(&self.id).await?))
        } else {
            Loaded::Work(Box::new(work), store.events_of(&self.id).await?)
        };
        Ok((loaded, gate))
    }

    /// One beat: a session in view is read often while it works and seldom
    /// while it rests; a goal with work still going is read every other beat,
    /// because what its tasks do lives in their sessions, which tell the
    /// ledger nothing.
    async fn beat(self: &Rc<Self>, tick: u32) {
        let (feed, goal_live) = {
            let state = self.state.borrow();
            let feed = state.focus_feed.clone().or_else(|| match self.tab(&state) {
                Tab::Conversation => state.feed.clone(),
                _ => None,
            });
            let goal_live = match &state.loaded {
                Some(Loaded::Goal(g, _)) => !g.goal.at_rest() || g.tasks.iter().any(|t| !t.at_rest()),
                _ => false,
            };
            (feed, goal_live)
        };
        if let Some(feed) = feed {
            let running = feed.state().status == SessionStatus::Running;
            if running || tick % 3 == 0 {
                feed.read().await;
                self.render();
            }
        }
        if goal_live && tick % 2 == 0 {
            Rc::clone(self).load().await;
        }
    }

    /// A conversation keeps its newest step in view as it grows; the other
    /// views stay where the person put them.
    fn following(&self) -> bool {
        let state = self.state.borrow();
        state.focus.is_some() || self.tab(&state) == Tab::Conversation
    }

    fn to_bottom(&self) {
        self.at_bottom.set(true);
        self.jump.set_visible(false);
        let a = self.scroller.vadjustment();
        a.set_value(a.upper() - a.page_size());
    }

    fn scroll_to(&self, turn: &str) {
        let Some(widget) = self.turns.find(turn) else { return };
        if let Some(point) = widget.compute_point(&self.column, &gtk::graphene::Point::new(0.0, 0.0)) {
            self.scroller.vadjustment().set_value(f64::from(point.y()) - f64::from(SPACE_4));
        }
    }

    fn task(&self, id: &str) -> Option<Work> {
        let state = self.state.borrow();
        match state.loaded.as_ref()? {
            Loaded::Goal(g, _) if g.goal.id == id => Some(g.goal.clone()),
            Loaded::Goal(g, _) => g.tasks.iter().find(|t| t.id == id).cloned(),
            Loaded::Work(w, _) => (w.id == id).then(|| (**w).clone()),
        }
    }

    /// Who a message goes to: the agent in focus's task, else this work.
    fn target(&self) -> Option<Work> {
        let focus = self.state.borrow().focus.clone();
        match focus {
            Some(card) => self.task(&card.task),
            None => {
                let state = self.state.borrow();
                match (&state.loaded, &state.to) {
                    (Some(Loaded::Goal(g, _)), Some(to)) => g.node(to).map(|n| n.work.clone()),
                    (loaded, _) => loaded.as_ref().map(|l| l.work().clone()),
                }
            }
        }
    }

    /// Whether a session the composer speaks to has a process up to stop.
    fn working(&self) -> bool {
        let state = self.state.borrow();
        if let (Some(focus), Some(feed)) = (&state.focus, &state.focus_feed) {
            return focus.agent.is_none() && feed.state().status.live();
        }
        match &state.loaded {
            Some(Loaded::Goal(g, _)) => g.activity.values().any(|a| a.status.is_some_and(SessionStatus::live)),
            Some(Loaded::Work(..)) => state.feed.as_ref().is_some_and(|f| f.state().status.live()),
            None => false,
        }
    }

    fn roster(&self) -> AgentRoster {
        let state = self.state.borrow();
        match &state.loaded {
            Some(Loaded::Goal(g, _)) => AgentRoster::for_goal(Some(&g.goal), g.supervised(), &g.tasks, &g.activity),
            Some(Loaded::Work(w, _)) => match &state.feed {
                Some(feed) if w.has_session() => {
                    let f = feed.state();
                    AgentRoster::for_task(
                        w,
                        f.status,
                        f.asking.is_some(),
                        f.steps.last().map(Step::line),
                        f.usage.clone(),
                        &f.subagents,
                    )
                }
                _ => AgentRoster::default(),
            },
            None => AgentRoster::default(),
        }
    }

    /// Shows an agent's conversation in the centre: a goal's task, or a
    /// subagent. A task's own card is its page.
    fn choose(self: &Rc<Self>, card: AgentCard) {
        let own = {
            let state = self.state.borrow();
            state
                .loaded
                .as_ref()
                .is_some_and(|l| card.agent.is_none() && card.task == l.work().id && !l.work().kind.is_goal())
        };
        if own {
            let mut state = self.state.borrow_mut();
            state.focus = None;
            state.focus_feed = None;
        } else {
            let feed = Rc::new(Feed::new(Rc::clone(&self.app.store), &card.task, card.agent.as_deref()));
            let mut state = self.state.borrow_mut();
            state.focus = Some(card);
            state.focus_feed = Some(Rc::clone(&feed));
            drop(state);
            let page = Rc::clone(self);
            self.app.spawn(async move {
                feed.read().await;
                page.render();
            });
        }
        self.at_bottom.set(true);
        self.render();
    }

    fn unfocus(self: &Rc<Self>) {
        {
            let mut state = self.state.borrow_mut();
            state.focus = None;
            state.focus_feed = None;
        }
        self.at_bottom.set(true);
        self.render();
    }

    /// Shows a task's conversation, or opens its page when it has no session.
    fn focus_task(self: &Rc<Self>, task: &str) {
        let card = self.roster().cards.into_iter().find(|c| c.task == task && c.agent.is_none());
        match card {
            Some(card) => self.choose(card),
            None => self.app.open(Place::Work(task.to_string())),
        }
    }

    fn send(self: &Rc<Self>, text: String) {
        let Some(target) = self.target() else { return };
        if target.kind.is_goal() && target.state == WorkState::Done {
            let plan = match Plan::continue_goal(&target.id, &text) {
                Ok(plan) => plan,
                Err(e) => return self.composer.set_note(Some((&e.to_string(), true))),
            };
            self.composer.set_busy(true);
            let page = Rc::clone(self);
            self.app.spawn(async move {
                match page.app.store.start_goal(&plan).await {
                    Ok(_) => {
                        page.composer.clear();
                        page.composer.set_note(Some(("continued: planned and started again", false)));
                    }
                    Err(e) => page.composer.set_note(Some((&e.to_string(), true))),
                }
                page.composer.set_busy(false);
            });
            return;
        }
        let plan = match Plan::send(&target.id, &text) {
            Ok(plan) => plan,
            Err(e) => return self.composer.set_note(Some((&e.to_string(), true))),
        };
        let remember = if self.composer.remember() { Plan::remember(&text, &target).ok() } else { None };
        // The field is free again at once; the message shows in the
        // conversation until the work's record holds it.
        self.app.store.post(plan, &target.id, remember);
        self.composer.clear();
        self.composer.set_note(None);
        let view = self.tab(&self.state.borrow());
        if view != Tab::Conversation {
            self.show(Tab::Conversation);
        }
        self.to_bottom();
    }

    fn stop(self: &Rc<Self>) {
        let Some(target) = self.target() else { return };
        let page = Rc::clone(self);
        self.app.spawn(async move {
            match page.app.store.stop_work(&Plan::stop(&target.id)).await {
                Ok(back) if back.is_empty() => {
                    page.composer.set_note(Some(("stopped: your next message continues it", false)));
                }
                Ok(back) => {
                    let typed = page.composer.text();
                    page.composer.set_text(&if typed.trim().is_empty() { back } else { format!("{back}\n\n{typed}") });
                    page.composer
                        .set_note(Some(("stopped: what it had not read is back here to edit and send", false)));
                    page.composer.focus();
                }
                Err(e) => page.composer.set_note(Some((&e.to_string(), true))),
            }
        });
    }

    /// Draws the page from what was last read.
    fn render(self: &Rc<Self>) {
        let (turns, yours) = {
            let state = self.state.borrow();
            let Some(loaded) = &state.loaded else {
                let turns = match &state.failure {
                    Some(why) => {
                        let (id, why) = (self.id.clone(), why.clone());
                        vec![Turn::key("failure", &why).with(move || {
                            ui::empty_state("dialog-warning-symbolic", &format!("Cannot read {id}"), &why).upcast()
                        })]
                    }
                    None => vec![Turn::key("loading", &()).with(|| ui::waiting("Reading…").upcast())],
                };
                self.turns.update(turns);
                return;
            };
            self.render_header(loaded, &state);
            // A read that failed after one that worked keeps the page, and says so.
            self.header.problem.set_label(state.failure.as_deref().unwrap_or(""));
            self.header.problem.set_visible(state.failure.is_some());
            match (&state.focus, &state.focus_feed) {
                (Some(focus), Some(feed)) => {
                    let task = match loaded {
                        Loaded::Goal(g, _) => {
                            if g.goal.id == focus.task {
                                Some(g.goal.clone())
                            } else {
                                g.tasks.iter().find(|t| t.id == focus.task).cloned()
                            }
                        }
                        Loaded::Work(w, _) => Some((**w).clone()),
                    };
                    match task {
                        Some(task) => {
                            (self.session_turns(&task, feed, &director_answers(loaded, &task.id)), Vec::new())
                        }
                        None => (Vec::new(), Vec::new()),
                    }
                }
                _ => match (loaded, self.tab(&state)) {
                    (Loaded::Goal(g, record), Tab::Overview) => {
                        (self.overview_turns(g, record, &state.folds), Vec::new())
                    }
                    (Loaded::Goal(g, _), Tab::Conversation) => self.goal_turns(g, state.filter.clone()),
                    (Loaded::Goal(g, record), Tab::Record) => (
                        self.record_view_turns(g, record, state.record_filter, state.record_place.as_deref()),
                        Vec::new(),
                    ),
                    (Loaded::Work(w, _), Tab::Conversation) if state.feed.is_some() => match &state.feed {
                        Some(feed) => (self.session_turns(w, feed, &director_answers(loaded, &w.id)), Vec::new()),
                        None => (Vec::new(), Vec::new()),
                    },
                    (Loaded::Work(_, events), _) => (record_turns(events), Vec::new()),
                },
            }
        };
        self.state.borrow_mut().yours = yours;
        self.turns.update(turns);
        self.render_bars();
        let focused = {
            let state = self.state.borrow();
            state.focus.as_ref().map(|f| f.id.clone()).or_else(|| match &state.loaded {
                Some(Loaded::Work(w, _)) => Some(w.id.clone()),
                _ => None,
            })
        };
        self.agents.update(&self.roster(), focused.as_deref());
    }

    fn render_header(self: &Rc<Self>, loaded: &Loaded, state: &State) {
        let h = &self.header;
        ui::clear(&h.crumbs);
        ui::clear(&h.meta);
        let work = loaded.work();
        if let Some(focus) = &state.focus {
            let back = crumb_link(&work.title);
            let weak = self.me.clone();
            back.connect_clicked(move |_| {
                if let Some(page) = weak.upgrade() {
                    page.unfocus();
                }
            });
            h.crumbs.append(&back);
            if focus.agent.is_some() {
                if let Some(task) = self.task_in(loaded, &focus.task) {
                    h.crumbs.append(&crumb_sep());
                    h.crumbs.append(&ui::line(&task.title, &["meta", "text3"]));
                }
            }
            h.title.set_label(&focus.title);
            if focus.agent.is_none() {
                if let Some(task) = self.task_in(loaded, &focus.task) {
                    h.meta.append(&ui::state_badge(task.state));
                }
            }
            h.meta.append(&ui::line(&focus.kind, &["meta", "code", "text2"]));
            h.meta.append(&ui::line(focus.agent.as_deref().unwrap_or(&focus.task), &["meta", "code", "text3"]));
            h.tabs.set_visible(false);
            h.filters.set_visible(false);
            return;
        }
        h.crumbs.append(&ui::line(&work.project, &["meta", "text3"]));
        let chain: Vec<(String, String)> = match loaded {
            Loaded::Goal(g, _) => g.chain.iter().map(|l| (l.id.clone(), l.title.clone())).collect(),
            Loaded::Work(w, _) => w
                .parent
                .iter()
                .map(|p| (p.clone(), format!("Goal {p}")))
                .chain([(w.id.clone(), w.title.clone())])
                .collect(),
        };
        for (i, (id, title)) in chain.iter().enumerate() {
            h.crumbs.append(&crumb_sep());
            if i + 1 == chain.len() {
                h.crumbs.append(&ui::line(title, &["meta"]));
            } else {
                let link = crumb_link(title);
                let (app, id) = (self.app.clone(), id.clone());
                link.connect_clicked(move |_| app.open(Place::Work(id.clone())));
                h.crumbs.append(&link);
            }
        }
        if h.titles.visible_child_name().as_deref() != Some("entry") {
            h.title.set_label(&work.title);
            h.title.set_tooltip_text(Some(&work.title));
        }
        h.meta.append(&ui::state_badge(work.state));
        let tab = self.tab(state);
        match loaded {
            Loaded::Goal(..) => {
                h.tabs.set_visible(true);
                h.overview.set_visible(true);
            }
            Loaded::Work(w, _) => {
                h.tabs.set_visible(w.has_session());
                h.overview.set_visible(false);
            }
        }
        h.filters.set_visible(matches!(loaded, Loaded::Goal(..)) && tab == Tab::Conversation);
        let button = match tab {
            Tab::Overview => &h.overview,
            Tab::Conversation => &h.conversation,
            Tab::Record => &h.record,
        };
        if !button.is_active() {
            // Marking the view shown is not choosing it.
            self.syncing.set(true);
            button.set_active(true);
            self.syncing.set(false);
        }
    }

    /// The view in effect: the one chosen here, else the default for the work.
    fn tab(&self, state: &State) -> Tab {
        state.tab.unwrap_or_else(|| match &state.loaded {
            Some(Loaded::Goal(..)) => {
                self.app.settings.goal_tab().and_then(|t| Tab::named(&t)).unwrap_or(Tab::Overview)
            }
            _ => Tab::Conversation,
        })
    }

    /// Shows one view; a goal's choice is remembered for the next goal opened.
    fn show(self: &Rc<Self>, tab: Tab) {
        let is_goal = {
            let mut state = self.state.borrow_mut();
            if self.tab(&state) == tab && state.tab.is_some() {
                return;
            }
            state.tab = Some(tab);
            matches!(state.loaded, Some(Loaded::Goal(..)))
        };
        if is_goal {
            if let Err(e) = self.app.settings.set_goal_tab(tab.name()) {
                self.app.complain(&format!("cannot remember the view: {e}"));
            }
        }
        self.at_bottom.set(tab == Tab::Conversation);
        if tab != Tab::Conversation {
            self.scroller.vadjustment().set_value(0.0);
        }
        self.render();
    }

    fn set_filter(self: &Rc<Self>, filter: ThreadFilter) {
        if self.state.borrow().filter == filter {
            return;
        }
        self.state.borrow_mut().filter = filter;
        self.render();
    }

    fn set_record_filter(self: &Rc<Self>, filter: RecordFilter) {
        self.state.borrow_mut().record_filter = filter;
        self.render();
    }

    fn set_record_place(self: &Rc<Self>, place: Option<String>) {
        self.state.borrow_mut().record_place = place;
        self.render();
    }

    /// Folds a sub-goal's steps open or closed.
    fn set_fold(self: &Rc<Self>, id: &str, open: bool) {
        self.state.borrow_mut().folds.insert(id.to_string(), open);
        self.render();
    }

    fn task_in(&self, loaded: &Loaded, id: &str) -> Option<Work> {
        match loaded {
            Loaded::Goal(g, _) if g.goal.id == id => Some(g.goal.clone()),
            Loaded::Goal(g, _) => g.tasks.iter().find(|t| t.id == id).cloned(),
            Loaded::Work(w, _) => (w.id == id).then(|| (**w).clone()),
        }
    }

    /// The toolbar and the composer, for the work as last read.
    fn render_bars(self: &Rc<Self>) {
        let state = self.state.borrow();
        let Some(loaded) = &state.loaded else { return };
        let pass = state.gate.as_ref().and_then(Plan::passing);
        match (&pass, &state.gate) {
            (Some(plan), Some(gate)) => {
                self.gate.set_label(&plan.title);
                self.gate.set_tooltip_text(Some(&format!(
                    "{} — {}",
                    gate.why.as_deref().unwrap_or(""),
                    plan.command_line()
                )));
                self.gate.set_visible(true);
            }
            _ => self.gate.set_visible(false),
        }
        let work = loaded.work();
        self.actions.set_menu_model(Some(&work_menu(work, true)));
        let toggle = if matches!(work.state, WorkState::Paused | WorkState::Dropped) {
            Some(Plan::resume(&work.id, ""))
        } else if work.live() {
            Some(Plan::pause(&work.id))
        } else {
            None
        };
        self.pause.set_visible(toggle.is_some());
        if let Some(plan) = &toggle {
            self.pause.set_label(&plan.title);
            self.pause.set_tooltip_text(Some(&plan.command_line()));
        }
        *self.pause_plan.borrow_mut() = toggle;
        drop(state);
        let cards = self.roster().cards.len();
        self.agents_toggle.set_label(&if cards > 0 { format!("Agents · {cards}") } else { "Agents".to_string() });

        let state = self.state.borrow();
        self.yours.set_visible(state.focus.is_none() && !state.yours.is_empty());
        while let Some(row) = self.yours_list.row_at_index(0) {
            self.yours_list.remove(&row);
        }
        for (said, _) in &state.yours {
            let label = ui::line(said, &["callout"]);
            label.set_max_width_chars(48);
            label.set_margin_top(SPACE_1);
            label.set_margin_bottom(SPACE_1);
            self.yours_list.append(&label);
        }
        drop(state);

        let target = self.target();
        // A goal is told things from its conversation; its overview and record only read.
        let reading = {
            let state = self.state.borrow();
            state.focus.is_none()
                && matches!(state.loaded, Some(Loaded::Goal(..)))
                && self.tab(&state) != Tab::Conversation
        };
        let shown = !reading
            && target.as_ref().is_some_and(|t| !t.at_rest() || (t.kind.is_goal() && t.state == WorkState::Done));
        self.composer_box.set_visible(shown);
        let Some(target) = target else { return };
        let continuing = target.kind.is_goal() && target.state == WorkState::Done;
        self.composer.set_placeholder(if continuing {
            "Continue this goal… say what is being worked on; it is planned and started again"
        } else if target.kind.is_goal() {
            "Message the director about this goal…"
        } else {
            "Message this task… it reads it when it is ready"
        });
        self.composer.set_remember_offered(!continuing);
        self.composer.set_working(!continuing && self.working());
        self.composer.set_chips(&self.chips(&target));
        self.render_addressees();
    }

    /// Offers the whole goal and each sub-goal as who a message goes to.
    fn render_addressees(&self) {
        let offered = {
            let state = self.state.borrow();
            match (&state.loaded, &state.focus) {
                (Some(Loaded::Goal(g, _)), None) => g.addressees(),
                _ => Vec::new(),
            }
        };
        self.composer.to.set_visible(offered.len() > 1);
        if self.state.borrow().addressees == offered {
            return;
        }
        let labels: Vec<String> = offered.iter().map(|(_, path)| format!("to: {path}")).collect();
        let strs: Vec<&str> = labels.iter().map(String::as_str).collect();
        let to = self.state.borrow().to.clone();
        let at = to.and_then(|id| offered.iter().position(|(o, _)| *o == id)).unwrap_or(0);
        self.syncing.set(true);
        self.composer.to.set_model(Some(&gtk::StringList::new(&strs)));
        self.composer.to.set_selected(at as u32);
        self.syncing.set(false);
        self.state.borrow_mut().addressees = offered;
    }

    /// What a message reaches: runner, model, the folder it works in.
    fn chips(&self, target: &Work) -> Vec<String> {
        let state = self.state.borrow();
        let mut chips: Vec<String> = target.runner.iter().cloned().collect();
        let usage = match (&state.focus, &state.focus_feed, &state.loaded) {
            (Some(focus), Some(feed), _) if focus.agent.is_none() => feed.state().usage.clone(),
            (_, _, Some(Loaded::Goal(g, _))) => g.activity.get(&target.id).and_then(|a| a.usage.clone()),
            _ => state.feed.as_ref().and_then(|f| f.state().usage.clone()),
        };
        if let Some(model) = usage.and_then(|u| u.model).filter(|m| !m.is_empty()) {
            chips.push(model.trim_start_matches("claude-").to_string());
        }
        let cwd = target.cwd.clone().or_else(|| match &state.loaded {
            Some(Loaded::Goal(g, _)) => g.tasks.iter().find_map(|t| t.cwd.clone()),
            _ => None,
        });
        if let Some(folder) = cwd.as_deref().and_then(|c| c.trim_end_matches('/').rsplit('/').next()) {
            chips.push(folder.to_string());
        }
        chips
    }

    /// Keeps what the director said of the question `work` asks, then reads
    /// the page again to show it.
    fn heard(&self, work: &str) -> Rc<dyn Fn(kit::DirectorAnswer)> {
        let (weak, work) = (self.me.clone(), work.to_string());
        Rc::new(move |said| {
            if let Some(page) = weak.upgrade() {
                page.director_said.borrow_mut().insert(work.clone(), said);
                page.app.spawn(Rc::clone(&page).load());
            }
        })
    }

    fn reload(&self) -> Rc<dyn Fn()> {
        let weak = self.me.clone();
        Rc::new(move || {
            if let Some(page) = weak.upgrade() {
                page.app.spawn(Rc::clone(&page).load());
            }
        })
    }
}

fn build(app: &App, id: &str, me: Weak<WorkPage>) -> WorkPage {
    let header = build_header();
    let turns = Turns::new(20);
    // The header and its views stay put while the page scrolls under them.
    let top = ui::vbox(0);
    top.set_margin_top(20);
    top.set_margin_bottom(SPACE_3);
    top.set_margin_start(SPACE_6);
    top.set_margin_end(SPACE_6);
    top.append(&header.widget);
    let column = ui::vbox(20);
    column.set_margin_top(20);
    column.set_margin_bottom(20);
    column.set_margin_start(SPACE_6);
    column.set_margin_end(SPACE_6);
    column.append(&turns.widget);
    // A page opens at its top, wherever focus lands inside it.
    let viewport = gtk::Viewport::builder().scroll_to_focus(false).child(&column).build();
    let scroller =
        gtk::ScrolledWindow::builder().hscrollbar_policy(gtk::PolicyType::Never).vexpand(true).child(&viewport).build();
    let jump = gtk::Button::from_icon_name("go-down-symbolic");
    jump.add_css_class("circular");
    jump.add_css_class("osd");
    jump.add_css_class("jump");
    jump.set_tooltip_text(Some("Jump to the latest"));
    jump.set_halign(gtk::Align::End);
    jump.set_valign(gtk::Align::End);
    jump.set_margin_end(SPACE_6);
    jump.set_margin_bottom(SPACE_3);
    jump.set_visible(false);
    let overlay = gtk::Overlay::new();
    overlay.set_child(Some(&scroller));
    overlay.add_overlay(&jump);

    let composer = Composer::new(Rc::clone(&app.settings), 1, 10);
    let composer_box = ui::vbox(0);
    composer_box.set_margin_start(SPACE_6);
    composer_box.set_margin_end(SPACE_6);
    composer_box.set_margin_top(SPACE_3);
    composer_box.set_margin_bottom(22);
    composer_box.append(&composer.widget);

    let body = ui::vbox(0);
    body.add_css_class("page");
    body.append(&top);
    body.append(&ui::hairline());
    body.append(&overlay);
    body.append(&composer_box);

    let bar = adw::HeaderBar::new();
    bar.set_show_title(false);
    let sidebar =
        gtk::ToggleButton::builder().icon_name("sidebar-show-symbolic").tooltip_text("Show the goals").build();
    sidebar.add_css_class("flat");
    app.sidebar_split.bind_property("show-sidebar", &sidebar, "active").bidirectional().sync_create().build();
    bar.pack_start(&sidebar);
    let agents_toggle = gtk::ToggleButton::builder().label("Agents").tooltip_text("The agents on this work").build();
    agents_toggle.add_css_class("wd-button");
    let actions = gtk::MenuButton::builder().label("Actions").build();
    actions.add_css_class("wd-button");
    ui::announce(&actions, "Actions");
    let pause = ui::button("Pause", false);
    pause.set_visible(false);
    let yours_list = gtk::ListBox::new();
    yours_list.add_css_class("navigation-sidebar");
    let yours_popover = gtk::Popover::builder().child(&yours_list).build();
    let yours = gtk::MenuButton::builder()
        .label("Your messages")
        .tooltip_text("What you said on this goal, to jump back to")
        .popover(&yours_popover)
        .build();
    yours.add_css_class("wd-button");
    ui::announce(&yours, "Your messages");
    let gate = ui::button("", true);
    gate.set_visible(false);
    bar.pack_end(&agents_toggle);
    bar.pack_end(&yours);
    bar.pack_end(&actions);
    bar.pack_end(&pause);
    bar.pack_end(&gate);

    let agents = AgentsPanel::build();
    let shown = app.settings.agents_shown();
    let a = app.clone();
    let side = SidePanel::new(&body, &agents.widget, app.settings.agents_width(), shown, move |width| {
        if let Err(e) = a.settings.set_agents_width(width) {
            a.complain(&format!("cannot keep the agents panel's width: {e}"));
        }
    });
    side.set_over(app.sidebar_split.is_collapsed());
    let weak = Rc::downgrade(&side);
    let narrowed = app.sidebar_split.connect_collapsed_notify(move |split| {
        if let Some(side) = weak.upgrade() {
            side.set_over(split.is_collapsed());
        }
    });
    let (split, narrowed) = (app.sidebar_split.clone(), Cell::new(Some(narrowed)));
    side.widget.connect_destroy(move |_| {
        if let Some(id) = narrowed.take() {
            split.disconnect(id);
        }
    });
    agents_toggle.set_active(shown);
    let (a, s) = (app.clone(), Rc::downgrade(&side));
    agents_toggle.connect_toggled(move |t| {
        if let Some(side) = s.upgrade() {
            side.set_shown(t.is_active());
        }
        if let Err(e) = a.settings.set_agents_shown(t.is_active()) {
            a.complain(&format!("cannot keep the agents panel shown or hidden: {e}"));
        }
    });
    // The panel sits under the header bar, so its toggle is never under it.
    let widget = adw::ToolbarView::new();
    widget.add_top_bar(&bar);
    widget.set_content(Some(&side.widget));

    WorkPage {
        app: app.clone(),
        id: id.to_string(),
        me,
        widget,
        _side: side,
        scroller,
        column,
        header,
        turns,
        composer,
        composer_box,
        jump,
        gate,
        pause,
        agents_toggle,
        actions,
        yours,
        yours_list,
        agents,
        state: RefCell::new(State {
            loaded: None,
            failure: None,
            gate: None,
            feed: None,
            focus: None,
            focus_feed: None,
            tab: None,
            filter: ThreadFilter::All,
            folds: HashMap::new(),
            record_filter: RecordFilter::Everything,
            record_place: None,
            to: None,
            addressees: Vec::new(),
            yours: Vec::new(),
        }),
        at_bottom: Cell::new(true),
        escape: Cell::new(EscapeTaps::default()),
        loading: Cell::new(false),
        stale: Cell::new(false),
        syncing: Cell::new(false),
        watch: RefCell::new(None),
        shown_filters: RefCell::new(None),
        pause_plan: RefCell::new(None),
        director_said: RefCell::new(HashMap::new()),
    }
}

fn build_header() -> Header {
    let widget = ui::vbox(0);
    let crumbs = ui::hbox(6);
    let title = ui::line("", &["display"]);
    let entry = gtk::Entry::new();
    entry.add_css_class("display");
    let titles = gtk::Stack::builder().vhomogeneous(false).hhomogeneous(false).build();
    titles.add_named(&title, Some("title"));
    titles.add_named(&entry, Some("entry"));
    titles.set_visible_child_name("title");
    let meta = ui::hbox(SPACE_2);
    meta.set_valign(gtk::Align::Center);
    let heading = ui::hbox(SPACE_3);
    heading.set_margin_top(6);
    heading.append(&titles);
    heading.append(&meta);
    let tab = |label: &str, group: Option<&gtk::ToggleButton>| {
        let b = gtk::ToggleButton::builder().label(label).build();
        b.add_css_class("tab");
        if let Some(group) = group {
            b.set_group(Some(group));
        }
        b
    };
    let overview = tab("Overview", None);
    let conversation = tab("Conversation", Some(&overview));
    let record = tab("Record", Some(&overview));
    let tabs = ui::hbox(SPACE_1);
    tabs.set_halign(gtk::Align::Start);
    tabs.set_margin_top(14);
    tabs.append(&overview);
    tabs.append(&conversation);
    tabs.append(&record);
    tabs.set_visible(false);
    let filters = ui::hbox(6);
    filters.set_margin_top(SPACE_3);
    filters.set_visible(false);
    widget.append(&crumbs);
    widget.append(&heading);
    let problem = ui::said("", &["callout", "failure"]);
    problem.set_visible(false);
    widget.append(&problem);
    widget.append(&tabs);
    widget.append(&filters);
    Header { widget, crumbs, titles, title, entry, meta, problem, tabs, overview, conversation, record, filters }
}

/// A level of the breadcrumb that opens its page.
fn crumb_link(title: &str) -> gtk::Button {
    let button = gtk::Button::with_label(title);
    button.add_css_class("crumb-link");
    button
}

fn crumb_sep() -> gtk::Label {
    ui::line("›", &["meta", "text3"])
}
