//! One goal or task, read again whenever the board is. A goal reads as one
//! conversation with its supervisor and agents; a task leads with its
//! session's conversation, its record one toggle away. The agents inspector
//! sits beside it, and choosing an agent shows its conversation in the centre.

use std::cell::{Cell, RefCell};
use std::rc::{Rc, Weak};
use std::time::{Duration, Instant};

use adw::prelude::*;
use gtk::{gdk, glib};

use kit::{
    follow, AgentCard, AgentRoster, ChannelError, EscapeTaps, Event, Feed, Gate, GoalDetail, Outgoing, OutgoingState,
    Phase, Plan, Report, SessionStatus, Step, StepKind, TaskProgress, ThreadFilter, ThreadItem, ThreadRow, ThreadStep,
    ToolCall, Watch, Work, WorkState,
};

use crate::agents::{phase_mark, AgentsPanel};
use crate::app::{work_menu, App, Place};
use crate::composer::Composer;
use crate::question;
use crate::turns::{Turn, Turns};
use crate::ui::{self, SPACE_1, SPACE_2, SPACE_3, SPACE_4, SPACE_5, SPACE_6};

/// Consecutive turns by one speaker within this many seconds read as one.
const MERGE_SECONDS: i64 = 120;

enum Loaded {
    Goal(Box<GoalDetail>),
    Work(Box<Work>, Vec<Event>),
}

impl Loaded {
    fn work(&self) -> &Work {
        match self {
            Loaded::Goal(g) => &g.goal,
            Loaded::Work(w, _) => w,
        }
    }
}

/// The views of a work page. A goal has all four; a task its conversation and record.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Tab {
    Overview,
    Conversation,
    Tasks,
    Record,
}

impl Tab {
    pub fn name(self) -> &'static str {
        match self {
            Tab::Overview => "overview",
            Tab::Conversation => "conversation",
            Tab::Tasks => "tasks",
            Tab::Record => "record",
        }
    }

    pub fn named(name: &str) -> Option<Tab> {
        [Tab::Overview, Tab::Conversation, Tab::Tasks, Tab::Record].into_iter().find(|t| t.name() == name)
    }
}

/// How the tasks table is ordered.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Sort {
    Plan,
    State,
    Recent,
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
    sort: Sort,
    /// Where each of your messages is, to jump back to: what you said, and its turn.
    yours: Vec<(String, String)>,
}

struct Header {
    widget: gtk::Box,
    crumbs: gtk::Box,
    titles: gtk::Stack,
    title: gtk::Label,
    entry: gtk::Entry,
    meta: gtk::Box,
    problem: gtk::Label,
    tabs: gtk::Box,
    overview: gtk::ToggleButton,
    conversation: gtk::ToggleButton,
    tasks: gtk::ToggleButton,
    record: gtk::ToggleButton,
}

pub struct WorkPage {
    app: App,
    id: String,
    me: Weak<WorkPage>,
    pub widget: adw::OverlaySplitView,
    scroller: gtk::ScrolledWindow,
    column: gtk::Box,
    header: Header,
    turns: Turns,
    composer: Rc<Composer>,
    composer_box: gtk::Box,
    jump: gtk::Button,
    gate: gtk::Button,
    open: gtk::Button,
    pause: gtk::Button,
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
    /// What the Pause or Resume button runs, for the work as last read.
    pause_plan: RefCell<Option<Plan>>,
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
        *self.watch.borrow_mut() = Some(self.app.store.watch(move |change| {
            let Some(page) = weak.upgrade() else { return };
            match change {
                kit::Change::Board => page.app.spawn(Rc::clone(&page).load()),
                kit::Change::Outbox => page.render(),
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
                    let a = a.clone();
                    glib::idle_add_local_once(move || a.set_value(a.upper() - a.page_size()));
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
        self.open.connect_clicked(move |_| {
            if let Some(page) = weak.upgrade() {
                if let Some(target) = page.open_target() {
                    page.app.run(Plan::open(&target));
                }
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
        for (button, tab) in [
            (&h.overview, Tab::Overview),
            (&h.conversation, Tab::Conversation),
            (&h.tasks, Tab::Tasks),
            (&h.record, Tab::Record),
        ] {
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
            Loaded::Goal(Box::new(store.goal(&self.id).await?))
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
                Some(Loaded::Goal(g)) => !g.goal.at_rest() || g.tasks.iter().any(|t| !t.at_rest()),
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
            Loaded::Goal(g) if g.goal.id == id => Some(g.goal.clone()),
            Loaded::Goal(g) => g.tasks.iter().find(|t| t.id == id).cloned(),
            Loaded::Work(w, _) => (w.id == id).then(|| (**w).clone()),
        }
    }

    /// Who a message goes to: the agent in focus's task, else this work.
    fn target(&self) -> Option<Work> {
        let focus = self.state.borrow().focus.clone();
        match focus {
            Some(card) => self.task(&card.task),
            None => self.state.borrow().loaded.as_ref().map(|l| l.work().clone()),
        }
    }

    /// Whether a session the composer speaks to has a process up to stop.
    fn working(&self) -> bool {
        let state = self.state.borrow();
        if let (Some(focus), Some(feed)) = (&state.focus, &state.focus_feed) {
            return focus.agent.is_none() && feed.state().status.live();
        }
        match &state.loaded {
            Some(Loaded::Goal(g)) => g.activity.values().any(|a| a.status.is_some_and(SessionStatus::live)),
            Some(Loaded::Work(..)) => state.feed.as_ref().is_some_and(|f| f.state().status.live()),
            None => false,
        }
    }

    fn roster(&self) -> AgentRoster {
        let state = self.state.borrow();
        match &state.loaded {
            Some(Loaded::Goal(g)) => AgentRoster::for_goal(Some(&g.goal), g.supervised(), &g.tasks, &g.activity),
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

    /// Where the work is done, to open in the editor: the task in focus, this
    /// task, or a goal's first task with a session.
    fn open_target(&self) -> Option<String> {
        let state = self.state.borrow();
        if let Some(focus) = &state.focus {
            return Some(focus.task.clone());
        }
        match state.loaded.as_ref()? {
            Loaded::Goal(g) => g.tasks.iter().find(|t| t.has_session()).map(|t| t.id.clone()),
            Loaded::Work(w, _) => w.has_session().then(|| w.id.clone()),
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
                        Loaded::Goal(g) => {
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
                    (Loaded::Goal(g), Tab::Overview) => (self.overview_turns(g, state.gate.as_ref()), Vec::new()),
                    (Loaded::Goal(g), Tab::Conversation) => self.goal_turns(g, state.filter),
                    (Loaded::Goal(g), Tab::Tasks) => (self.task_turns(g, state.sort), Vec::new()),
                    (Loaded::Goal(g), Tab::Record) => (record_turns(&g.events), Vec::new()),
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
            let back = ui::link(&format!("‹ {}", work.title));
            let weak = self.me.clone();
            back.connect_clicked(move |_| {
                if let Some(page) = weak.upgrade() {
                    page.unfocus();
                }
            });
            h.crumbs.append(&back);
            if focus.agent.is_some() {
                if let Some(task) = self.task_in(loaded, &focus.task) {
                    h.crumbs.append(&ui::line(&format!("› {}", task.title), &["meta", "text2"]));
                }
            }
            h.crumbs.set_visible(true);
            h.title.set_label(&focus.title);
            if focus.agent.is_none() {
                if let Some(task) = self.task_in(loaded, &focus.task) {
                    h.meta.append(&ui::state_pill(task.state));
                }
            }
            h.meta.append(&ui::line(&focus.kind, &["meta", "code", "text2"]));
            h.meta.append(&ui::line(focus.agent.as_deref().unwrap_or(&focus.task), &["meta", "code", "text3"]));
            h.tabs.set_visible(false);
            return;
        }
        match &work.parent {
            Some(parent) => {
                let up = ui::link(&format!("‹ Goal {parent}"));
                let (app, parent) = (self.app.clone(), parent.clone());
                up.connect_clicked(move |_| app.open(Place::Work(parent.clone())));
                h.crumbs.append(&up);
                h.crumbs.set_visible(true);
            }
            None => h.crumbs.set_visible(false),
        }
        if h.titles.visible_child_name().as_deref() != Some("entry") {
            h.title.set_label(&work.title);
        }
        h.meta.append(&ui::state_pill(work.state));
        let mut parts = vec![work.project.clone()];
        if let Loaded::Goal(g) = loaded {
            if g.rollup.total > 0 {
                parts.push(format!("{} of {} tasks", g.rollup.done, g.rollup.total));
            }
            if let Some(d) = g.delivery.as_ref().filter(|d| d.status != "none") {
                parts.push(if d.detail.is_empty() {
                    d.status.clone()
                } else {
                    format!("{} · {}", d.status, d.detail)
                });
            }
        }
        h.meta.append(&ui::line(&parts.join(" · "), &["meta", "text2"]));
        let tab = self.tab(state);
        match loaded {
            Loaded::Goal(g) => {
                h.tabs.set_visible(true);
                h.overview.set_visible(true);
                h.tasks.set_visible(true);
                h.overview.set_label(if g.waiting_on_you() > 0 { "Overview ●" } else { "Overview" });
                h.tasks.set_label(&format!("Tasks {}", g.tasks.len()));
                h.record.set_label(&format!("Record {}", g.events.len()));
            }
            Loaded::Work(w, events) => {
                h.tabs.set_visible(w.has_session());
                h.overview.set_visible(false);
                h.tasks.set_visible(false);
                h.record.set_label(&format!("Record {}", events.len()));
            }
        }
        let button = match tab {
            Tab::Overview => &h.overview,
            Tab::Conversation => &h.conversation,
            Tab::Tasks => &h.tasks,
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
            Some(Loaded::Goal(_)) => self.app.settings.goal_tab().and_then(|t| Tab::named(&t)).unwrap_or(Tab::Overview),
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
            matches!(state.loaded, Some(Loaded::Goal(_)))
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
        self.state.borrow_mut().filter = filter;
        self.render();
    }

    fn set_sort(self: &Rc<Self>, sort: Sort) {
        self.state.borrow_mut().sort = sort;
        self.render();
    }

    fn task_in(&self, loaded: &Loaded, id: &str) -> Option<Work> {
        match loaded {
            Loaded::Goal(g) if g.goal.id == id => Some(g.goal.clone()),
            Loaded::Goal(g) => g.tasks.iter().find(|t| t.id == id).cloned(),
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
        self.open.set_visible(self.open_target().is_some());

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
        let shown = target.as_ref().is_some_and(|t| !t.at_rest() || (t.kind.is_goal() && t.state == WorkState::Done));
        self.composer_box.set_visible(shown);
        let Some(target) = target else { return };
        let continuing = target.kind.is_goal() && target.state == WorkState::Done;
        self.composer.set_placeholder(if continuing {
            "Continue this goal… say what is being worked on; it is planned and started again"
        } else if target.kind.is_goal() {
            "Tell this goal… its supervisor reads it, and it is kept as a decision"
        } else {
            "Message this task… it reads it when it is ready"
        });
        self.composer.set_remember_offered(!continuing);
        self.composer.set_working(!continuing && self.working());
        self.composer.set_chips(&self.chips(&target));
    }

    /// What a message reaches: runner, model, the folder it works in.
    fn chips(&self, target: &Work) -> Vec<String> {
        let state = self.state.borrow();
        let mut chips: Vec<String> = target.runner.iter().cloned().collect();
        let usage = match (&state.focus, &state.focus_feed, &state.loaded) {
            (Some(focus), Some(feed), _) if focus.agent.is_none() => feed.state().usage.clone(),
            (_, _, Some(Loaded::Goal(g))) => g.activity.get(&target.id).and_then(|a| a.usage.clone()),
            _ => state.feed.as_ref().and_then(|f| f.state().usage.clone()),
        };
        if let Some(model) = usage.and_then(|u| u.model).filter(|m| !m.is_empty()) {
            chips.push(model.trim_start_matches("claude-").to_string());
        }
        let cwd = target.cwd.clone().or_else(|| match &state.loaded {
            Some(Loaded::Goal(g)) => g.tasks.iter().find_map(|t| t.cwd.clone()),
            _ => None,
        });
        if let Some(folder) = cwd.as_deref().and_then(|c| c.trim_end_matches('/').rsplit('/').next()) {
            chips.push(folder.to_string());
        }
        chips
    }

    /// A goal as one conversation: what was asked, the plan, what its
    /// supervisor and agents did, and what waits on you, last.
    /// Also where each of your messages is, to jump back to.
    fn goal_turns(self: &Rc<Self>, g: &GoalDetail, filter: ThreadFilter) -> (Vec<Turn>, Vec<(String, String)>) {
        let mut turns = Vec::new();
        let mut yours = Vec::new();
        let counts: Vec<(ThreadFilter, usize)> = ThreadFilter::ALL
            .iter()
            .map(|f| (*f, if *f == ThreadFilter::All { g.thread().len() } else { g.items(*f).len() }))
            .collect();
        let weak = self.me.clone();
        turns.push(Turn::key("filters", &(&counts, filter)).with(move || filter_chips(&counts, filter, weak)));
        if filter == ThreadFilter::All {
            if !g.goal.detail.is_empty() {
                let (detail, at) = (g.goal.detail.clone(), g.goal.created.clone());
                turns.push(
                    Turn::key("asked", &detail)
                        .with(move || ui::you_bubble(&detail, "You asked", Some(at.as_str()), None).upcast()),
                );
            }
            let plan: Vec<(String, String, WorkState, Option<String>)> =
                g.tasks.iter().map(|t| (t.id.clone(), t.title.clone(), t.state, t.heading.clone())).collect();
            let (weak, rollup) = (self.me.clone(), (g.rollup.done, g.rollup.total));
            turns.push(Turn::key("plan", &(&plan, rollup)).with(move || plan_block(&plan, rollup, weak)));
        }

        let mut day = String::new();
        for item in g.items(filter) {
            let at = match &item {
                ThreadItem::Step(step) => step.event().at.clone(),
                ThreadItem::Updates(run) => run.first().map(|s| s.event().at.clone()).unwrap_or_default(),
            };
            let today = kit::time::day(&at);
            if today != day {
                let label = today.clone();
                turns.push(Turn::key(format!("day-{}", item.id()), &label).with(move || ui::day_line(&label).upcast()));
                day = today;
            }
            match item {
                ThreadItem::Step(step) => {
                    if let Some(turn) = self.step_turn(g, *step, &mut yours) {
                        turns.push(turn);
                    }
                }
                ThreadItem::Updates(run) => {
                    let id = format!("u{}", run.first().map_or(0, ThreadStep::id));
                    let inner: Vec<Turn> =
                        run.iter().filter_map(|s| self.step_turn(g, s.clone(), &mut yours)).collect();
                    let count = run.len();
                    turns.push(Turn::key(&id, &run).with(move || updates_row(count, &at, inner)));
                }
            }
        }

        let outgoing: Vec<Outgoing> =
            self.app.store.outbox().iter().filter(|m| m.work == g.goal.id && !m.arrived_in_goal(g)).cloned().collect();
        for m in outgoing {
            let app = self.app.clone();
            turns.push(Turn::key(format!("out{}", m.id), &m.state).with(move || outgoing_bubble(&app, &m, "sent")));
        }

        turns.extend(self.asking_turns(g, false));
        (turns, yours)
    }

    /// One step of a goal's conversation as a turn; none for a question that
    /// waits on the person, which is answered in place below.
    fn step_turn(self: &Rc<Self>, g: &GoalDetail, step: ThreadStep, yours: &mut Vec<(String, String)>) -> Option<Turn> {
        let id = format!("e{}", step.id());
        let supervised = g.supervised();
        Some(match step {
            ThreadStep::Event(e) if g.is_yours(&e) => {
                let said = e.decision.as_ref().map(|d| d.answer.clone()).unwrap_or_else(|| e.body.clone());
                yours.push((said.clone(), id.clone()));
                let label = if e.kind == "answer" { "You answered" } else { "You" };
                Turn::key(&id, &e).with(move || ui::you_bubble(&said, label, Some(e.at.as_str()), None).upcast())
            }
            ThreadStep::Event(e) => {
                let task = self.named(g, &e.work);
                let report = g.reports.get(&e.id.to_string()).cloned();
                let weak = self.me.clone();
                Turn::key(&id, &(&e, &report)).with(move || event_turn(&e, report.as_ref(), task, weak))
            }
            ThreadStep::Exchange(q, answer) => {
                if answer.is_none() && g.asking.as_ref().is_some_and(|a| a.id == format!("event-{}", q.id)) {
                    return None;
                }
                if let Some(a) = answer.as_ref().filter(|a| g.is_yours(a)) {
                    yours.push((a.answer_text(), id.clone()));
                }
                let asker = if q.work == g.goal.id {
                    (if supervised { "Supervisor" } else { "Plan" }).to_string()
                } else {
                    g.title_of(&q.work).unwrap_or(&q.work).to_string()
                };
                let weak = self.me.clone();
                let target = (q.work != g.goal.id).then(|| q.work.clone());
                Turn::key(&id, &(&q, &answer)).with(move || exchange_turn(&q, answer.as_ref(), &asker, target, weak))
            }
            ThreadStep::Look(e) => {
                let text = format!("Supervisor looking · {} · {}", e.body, kit::time::when(&e.at));
                Turn::key(&id, &text).with(move || ui::event_line(&text, &[]).upcast())
            }
            ThreadStep::Supervisor(e, reply) => {
                let tasks = self.told(g, &reply);
                let (weak, goal) = (self.me.clone(), g.goal.id.clone());
                Turn::key(&id, &(&e, &reply, &tasks)).with(move || supervisor_turn(&e, &reply, &tasks, &goal, weak))
            }
            ThreadStep::Stopped(e, said) => {
                let task = self.named(g, &e.work);
                let weak = self.me.clone();
                Turn::key(&id, &(&e, &said)).with(move || stopped_turn(&e, &said, task, weak))
            }
        })
    }

    /// Each task a supervisor's action told, started or filed, where it is and what it is doing.
    fn told(&self, g: &GoalDetail, reply: &kit::SupervisorReply) -> Vec<(Option<Work>, Phase, Option<String>)> {
        reply
            .actions
            .iter()
            .map(|a| {
                let task = a.task.as_ref().and_then(|t| g.tasks.iter().find(|w| &w.id == t)).cloned();
                let activity = task.as_ref().and_then(|t| g.activity.get(&t.id));
                let now = activity.and_then(|a| a.error.clone().or_else(|| a.now.clone()));
                (task, Phase::of(activity), now)
            })
            .collect()
    }

    /// Where a goal stands, at a glance: what waits on you, the next step,
    /// how far its tasks have come, who is working now, the supervisor's
    /// latest word, what you decided and what you asked.
    fn overview_turns(self: &Rc<Self>, g: &GoalDetail, gate: Option<&Gate>) -> Vec<Turn> {
        let mut turns = Vec::new();
        if g.waiting_on_you() > 0 {
            turns.push(Turn::key("o-needs", &()).with(|| section_title("Needs you", true)));
            turns.extend(self.asking_turns(g, true));
        }
        if let Some((gate, plan)) = gate.and_then(|gate| Plan::passing(gate).map(|p| (gate.clone(), p))) {
            let app = self.app.clone();
            turns.push(Turn::key("o-next", &(&gate, &plan)).with(move || {
                let column = ui::vbox(SPACE_3);
                column.append(&section_title("Next step", false));
                let card = ui::card("raised");
                let row = ui::hbox(SPACE_4);
                let words = ui::vbox(SPACE_1);
                words.set_hexpand(true);
                words.append(&ui::text(&plan.title, &["title"]));
                if let Some(why) = &gate.why {
                    words.append(&ui::said(why, &["callout", "text2"]));
                }
                row.append(&words);
                let pass = gtk::Button::with_label(&plan.title);
                pass.add_css_class("suggested-action");
                pass.add_css_class("pill");
                pass.set_valign(gtk::Align::Center);
                pass.set_tooltip_text(Some(&plan.command_line()));
                pass.connect_clicked(move |_| app.run(plan.clone()));
                row.append(&pass);
                card.append(&row);
                column.append(&card);
                column.upcast()
            }));
        }
        let progress = g.progress();
        let weak = self.me.clone();
        turns.push(Turn::key("o-progress", &progress).with(move || {
            let column = ui::vbox(SPACE_3);
            let head = ui::hbox(SPACE_2);
            let title = section_title("Progress", false);
            title.set_hexpand(true);
            head.append(&title);
            let all = ui::link("All tasks");
            all.connect_clicked(move |_| {
                if let Some(page) = weak.upgrade() {
                    page.show(Tab::Tasks);
                }
            });
            head.append(&all);
            column.append(&head);
            column.append(&progress_card(&progress));
            column.upcast()
        }));
        let working: Vec<(Work, Phase, Option<String>)> = g
            .working()
            .into_iter()
            .map(|t| {
                let activity = g.activity.get(&t.id);
                (t.clone(), Phase::of(activity), activity.and_then(|a| a.error.clone().or_else(|| a.now.clone())))
            })
            .collect();
        if !working.is_empty() {
            let weak = self.me.clone();
            turns.push(Turn::key("o-working", &working).with(move || {
                let column = ui::vbox(SPACE_3);
                column.append(&section_title("Working now", false));
                for (task, phase, now) in &working {
                    column.append(&agent_chip(task, *phase, now.as_deref().unwrap_or(""), None, weak.clone()));
                }
                column.upcast()
            }));
        }
        if let Some((e, reply)) = g.latest_supervision() {
            let tasks = self.told(g, &reply);
            let weak = self.me.clone();
            turns.push(Turn::key("o-latest", &(&e, &reply, &tasks)).with(move || {
                let column = ui::vbox(SPACE_3);
                let head = ui::hbox(SPACE_2);
                let title = section_title("Latest from the supervisor", false);
                title.set_hexpand(true);
                head.append(&title);
                let open = ui::link("Conversation");
                let w = weak.clone();
                open.connect_clicked(move |_| {
                    if let Some(page) = w.upgrade() {
                        page.show(Tab::Conversation);
                    }
                });
                head.append(&open);
                column.append(&head);
                let card = ui::card("raised");
                card.append(&ui::line(&kit::time::when(&e.at), &["meta", "text3"]));
                card.append(&supervisor_body(&reply, &tasks, weak));
                column.append(&card);
                column.upcast()
            }));
        }
        let decisions: Vec<Event> = g.yours().into_iter().filter(|e| e.kind == "decision").rev().collect();
        if !decisions.is_empty() {
            turns.push(Turn::key("o-decided", &decisions).with(move || {
                let column = ui::vbox(SPACE_3);
                column.append(&section_title("What you decided", false));
                let card = ui::card("raised");
                for e in &decisions {
                    let row = ui::hbox(SPACE_3);
                    row.append(&ui::icon("emblem-ok-symbolic", &["accent-text"]));
                    let said = ui::said(&e.body, &["callout"]);
                    said.set_hexpand(true);
                    row.append(&said);
                    row.append(&ui::line(&kit::time::when(&e.at), &["meta", "text3"]));
                    card.append(&row);
                }
                column.append(&card);
                column.upcast()
            }));
        }
        if !g.goal.detail.is_empty() {
            let detail = g.goal.detail.clone();
            turns.push(Turn::key("o-asked", &detail).with(move || {
                let column = ui::vbox(SPACE_3);
                column.append(&section_title("What you asked", false));
                let card = ui::card("raised");
                card.append(&ui::folded(&detail, 10, &["body"]));
                column.append(&card);
                column.upcast()
            }));
        }
        turns
    }

    /// Every question waiting on the person, answered in place: the goal's
    /// own, each task's session question, and each task in needs-input with
    /// what it asked or reported.
    fn asking_turns(self: &Rc<Self>, g: &GoalDetail, overview: bool) -> Vec<Turn> {
        let prefix = if overview { "o-" } else { "" };
        let mut turns = Vec::new();
        if let Some(q) = &g.asking {
            let title = if g.supervised() { "The supervisor asks you" } else { "Waiting on you" };
            let (app, goal, q, answered) = (self.app.clone(), g.goal.id.clone(), q.clone(), self.reload());
            turns.push(
                Turn::key(format!("{prefix}asking"), &q)
                    .with(move || question::pending(&app, &goal, &q, title, answered)),
            );
        }
        let sessions =
            g.tasks.iter().filter_map(|t| g.activity.get(&t.id).and_then(|a| a.asking.clone()).map(|q| (t.clone(), q)));
        let waiting = g.waiting_tasks().into_iter().map(|w| (w.task, w.question));
        for (task, q) in sessions.chain(waiting) {
            let (app, answered, weak) = (self.app.clone(), self.reload(), self.me.clone());
            turns.push(Turn::key(format!("{prefix}asking-{}", task.id), &(&task, &q)).with(move || {
                let column = ui::vbox(SPACE_2);
                let link = ui::link(&task.title);
                let id = task.id.clone();
                link.connect_clicked(move |_| {
                    if let Some(page) = weak.upgrade() {
                        page.focus_task(&id);
                    }
                });
                column.append(&link);
                column.append(&question::pending(&app, &task.id, &q, "Waiting on you", answered));
                column.upcast()
            }));
        }
        turns
    }

    /// The goal's tasks as a table: state and title, what its agent is doing
    /// now, its last report's verdict, when it last moved, tokens.
    fn task_turns(self: &Rc<Self>, g: &GoalDetail, sort: Sort) -> Vec<Turn> {
        let mut rows: Vec<TaskLine> = g
            .tasks
            .iter()
            .map(|t| {
                let activity = g.activity.get(&t.id);
                TaskLine {
                    task: t.clone(),
                    now: activity.and_then(|a| a.error.clone().or_else(|| a.now.clone())),
                    asking: activity.is_some_and(|a| a.asking.is_some()),
                    verdict: g
                        .events
                        .iter()
                        .rev()
                        .find(|e| e.work == t.id && e.kind == "report")
                        .and_then(|e| g.reports.get(&e.id.to_string()))
                        .map(|r| r.verdict.clone())
                        .filter(|v| !v.is_empty()),
                    tokens: activity.and_then(|a| a.usage.as_ref()).map(|u| u.tokens),
                }
            })
            .collect();
        match sort {
            Sort::Plan => {}
            Sort::State => rows.sort_by_key(|r| rank(r.task.state)),
            Sort::Recent => rows.sort_by(|a, b| b.task.updated.cmp(&a.task.updated)),
        }
        let (app, weak) = (self.app.clone(), self.me.clone());
        vec![Turn::key("tasks", &(&rows, sort)).with(move || tasks_table(&rows, sort, &app, weak))]
    }

    /// The name of work on a goal: the task's title, or none for the goal itself.
    fn named(&self, g: &GoalDetail, id: &str) -> Option<(String, String, WorkState)> {
        g.tasks.iter().find(|t| t.id == id).map(|t| (t.id.clone(), t.title.clone(), t.state))
    }

    fn reload(&self) -> Rc<dyn Fn()> {
        let weak = self.me.clone();
        Rc::new(move || {
            if let Some(page) = weak.upgrade() {
                page.app.spawn(Rc::clone(&page).load());
            }
        })
    }

    /// A session as it happens: what it was told, said and thought, its tools
    /// grouped, and the questions it waits on, answered in place.
    fn session_turns(self: &Rc<Self>, work: &Work, feed: &Rc<Feed>, director: &[String]) -> Vec<Turn> {
        let f = feed.state();
        let mut turns = Vec::new();
        if let Some(why) = &f.failure {
            let why = why.clone();
            turns.push(Turn::key("feed-failure", &why).with(move || ui::failure(&why).upcast()));
        }
        if !f.loaded {
            turns.push(Turn::key("feed-loading", &()).with(|| ui::waiting("Reading the conversation…").upcast()));
        } else if f.steps.is_empty() {
            let text = if f.status == SessionStatus::NoSession {
                "No session yet: the conversation starts when the work is spawned."
            } else {
                "The session has said nothing yet."
            };
            turns.push(Turn::key("feed-empty", &text).with(move || ui::text(text, &["callout", "text2"]).upcast()));
        }
        let running = f.status == SessionStatus::Running;
        let last = f.steps.len().saturating_sub(1);
        // The agent's turns run together until someone else speaks or two minutes pass.
        let mut agent_since: Option<String> = None;
        for row in ThreadRow::rows(&f.steps) {
            let index = row.index();
            let id = format!("s{index}");
            let (step_at, agent) = match &row {
                ThreadRow::Step { step, .. } => {
                    (step.at.clone(), !matches!(step.kind, StepKind::Prompt | StepKind::Question))
                }
                ThreadRow::Tools { .. } => (f.steps.get(index).and_then(|s| s.at.clone()), true),
            };
            let continues = agent
                && agent_since.as_ref().is_some_and(|since| {
                    step_at.as_deref().and_then(|at| kit::time::between(since, at)).is_none_or(|s| s < MERGE_SECONDS)
                });
            if agent {
                if !continues {
                    agent_since = Some(step_at.clone().unwrap_or_default());
                }
            } else {
                agent_since = None;
            }
            let head = (!continues).then(|| (work.title.clone(), work.state, step_at.clone()));
            match row {
                ThreadRow::Step { index, step } => match step.kind {
                    StepKind::Prompt => {
                        let text = step.text().to_string();
                        if director.iter().any(|d| text.contains(d.as_str())) {
                            let at = step.at.clone();
                            turns.push(Turn::key(&id, &step).with(move || director_answer(&text, at.as_deref(), None)));
                        } else {
                            let label = if index == 0 { "Brief" } else { "Told" };
                            let at = step.at.clone();
                            turns.push(
                                Turn::key(&id, &step)
                                    .with(move || ui::you_bubble(&text, label, at.as_deref(), None).upcast()),
                            );
                        }
                    }
                    StepKind::Question => {
                        let Some(q) = step.question.clone() else { continue };
                        if q.pending() {
                            let (app, work_id, answered) = (self.app.clone(), work.id.clone(), self.reload_feed(feed));
                            turns.push(
                                Turn::key(&id, &q)
                                    .with(move || question::pending(&app, &work_id, &q, "Waiting on you", answered)),
                            );
                        } else {
                            turns.push(Turn::key(&id, &q).with(move || question::answered(&q)));
                        }
                    }
                    _ => {
                        let open = index == last && running;
                        turns.push(
                            Turn::key(&id, &(&step, open, &head))
                                .with(move || agent_turn(head, agent_body(&step, open))),
                        );
                    }
                },
                ThreadRow::Tools { calls, .. } => {
                    let working = running && index + calls.len() > last;
                    turns.push(
                        Turn::key(&id, &(&calls, working, &head)).with(move || agent_turn(head, tool_group(&calls))),
                    );
                }
            }
        }
        drop(f);
        if feed.agent.is_none() {
            let steps = feed.state().steps.clone();
            let outgoing: Vec<Outgoing> = self
                .app
                .store
                .outbox()
                .iter()
                .filter(|m| m.work == work.id && !m.arrived_in_steps(&steps))
                .cloned()
                .collect();
            let waiting = if running { "queued: read once it is ready" } else { "sent" };
            for m in outgoing {
                let app = self.app.clone();
                turns.push(
                    Turn::key(format!("out{}", m.id), &(&m.state, waiting))
                        .with(move || outgoing_bubble(&app, &m, waiting)),
                );
            }
        }
        if running {
            turns.push(Turn::key("working", &()).with(|| ui::waiting("Working…").upcast()));
        }
        turns
    }

    fn reload_feed(&self, feed: &Rc<Feed>) -> Rc<dyn Fn()> {
        let (weak, feed) = (self.me.clone(), Rc::downgrade(feed));
        Rc::new(move || {
            let (Some(page), Some(feed)) = (weak.upgrade(), feed.upgrade()) else { return };
            let p = Rc::clone(&page);
            page.app.spawn(async move {
                feed.read().await;
                p.render();
            });
        })
    }
}

fn build(app: &App, id: &str, me: Weak<WorkPage>) -> WorkPage {
    let header = build_header();
    let turns = Turns::new(SPACE_5);
    // The header and its views stay put while the page scrolls under them.
    let top = ui::vbox(0);
    top.set_margin_top(SPACE_4);
    top.set_margin_bottom(SPACE_3);
    top.set_margin_start(SPACE_6);
    top.set_margin_end(SPACE_6);
    top.append(&header.widget);
    let column = ui::vbox(SPACE_5);
    column.set_margin_top(SPACE_5);
    column.set_margin_bottom(SPACE_5);
    column.set_margin_start(SPACE_6);
    column.set_margin_end(SPACE_6);
    column.append(&turns.widget);
    let scroller =
        gtk::ScrolledWindow::builder().hscrollbar_policy(gtk::PolicyType::Never).vexpand(true).child(&column).build();
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
    composer_box.set_margin_bottom(SPACE_5);
    composer_box.append(&composer.widget);

    let body = ui::vbox(0);
    body.append(&top);
    body.append(&gtk::Separator::new(gtk::Orientation::Horizontal));
    body.append(&overlay);
    body.append(&composer_box);

    let bar = adw::HeaderBar::new();
    bar.set_show_title(false);
    let sidebar =
        gtk::ToggleButton::builder().icon_name("sidebar-show-symbolic").tooltip_text("Show the goals").build();
    app.sidebar_split.bind_property("show-sidebar", &sidebar, "active").bidirectional().sync_create().build();
    bar.pack_start(&sidebar);
    let agents_toggle =
        gtk::ToggleButton::builder().icon_name("sidebar-show-right-symbolic").tooltip_text("Agents").build();
    let actions = gtk::MenuButton::builder().label("Actions").build();
    let open = gtk::Button::builder()
        .label("Open in Editor")
        .tooltip_text("Open where this work is done in your editor")
        .build();
    let pause = gtk::Button::new();
    pause.set_visible(false);
    let command = gtk::Button::builder()
        .label("Command")
        .action_name("win.command")
        .tooltip_text("Run a wd command (Ctrl+K)")
        .build();
    let yours_list = gtk::ListBox::new();
    yours_list.add_css_class("navigation-sidebar");
    let yours_popover = gtk::Popover::builder().child(&yours_list).build();
    let yours = gtk::MenuButton::builder()
        .label("Your messages")
        .tooltip_text("What you said on this goal, to jump back to")
        .popover(&yours_popover)
        .build();
    let gate = gtk::Button::new();
    gate.add_css_class("suggested-action");
    gate.set_visible(false);
    bar.pack_end(&agents_toggle);
    bar.pack_end(&command);
    bar.pack_end(&open);
    bar.pack_end(&actions);
    bar.pack_end(&pause);
    bar.pack_end(&yours);
    bar.pack_end(&gate);

    let toolbar = adw::ToolbarView::new();
    toolbar.add_top_bar(&bar);
    toolbar.set_content(Some(&body));

    let agents = AgentsPanel::build();
    let widget = adw::OverlaySplitView::builder()
        .sidebar_position(gtk::PackType::End)
        .min_sidebar_width(260.0)
        .max_sidebar_width(480.0)
        .sidebar_width_fraction(0.28)
        .show_sidebar(app.settings.agents_shown())
        .content(&toolbar)
        .sidebar(&agents.widget)
        .build();
    widget.bind_property("show-sidebar", &agents_toggle, "active").bidirectional().sync_create().build();
    let a = app.clone();
    agents_toggle.connect_toggled(move |t| {
        if let Err(e) = a.settings.set_agents_shown(t.is_active()) {
            a.complain(&format!("cannot keep the agents panel shown or hidden: {e}"));
        }
    });

    WorkPage {
        app: app.clone(),
        id: id.to_string(),
        me,
        widget,
        scroller,
        column,
        header,
        turns,
        composer,
        composer_box,
        jump,
        gate,
        open,
        pause,
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
            sort: Sort::Plan,
            yours: Vec::new(),
        }),
        at_bottom: Cell::new(true),
        escape: Cell::new(EscapeTaps::default()),
        loading: Cell::new(false),
        stale: Cell::new(false),
        syncing: Cell::new(false),
        watch: RefCell::new(None),
        pause_plan: RefCell::new(None),
    }
}

fn build_header() -> Header {
    let widget = ui::vbox(SPACE_2);
    let crumbs = ui::hbox(SPACE_2);
    crumbs.set_visible(false);
    let title = ui::text("", &["display"]);
    let entry = gtk::Entry::new();
    entry.add_css_class("display");
    let titles = gtk::Stack::builder().vhomogeneous(false).hhomogeneous(false).build();
    titles.add_named(&title, Some("title"));
    titles.add_named(&entry, Some("entry"));
    titles.set_visible_child_name("title");
    let meta = ui::hbox(SPACE_2);
    let overview = gtk::ToggleButton::builder().label("Overview").build();
    let conversation = gtk::ToggleButton::builder().label("Conversation").group(&overview).build();
    let tasks = gtk::ToggleButton::builder().label("Tasks").group(&overview).build();
    let record = gtk::ToggleButton::builder().label("Record").group(&overview).build();
    let tabs = ui::hbox(0);
    tabs.add_css_class("linked");
    tabs.set_halign(gtk::Align::Start);
    tabs.set_margin_top(SPACE_2);
    tabs.append(&overview);
    tabs.append(&conversation);
    tabs.append(&tasks);
    tabs.append(&record);
    tabs.set_visible(false);
    widget.append(&crumbs);
    widget.append(&titles);
    widget.append(&meta);
    let problem = ui::said("", &["callout", "failure"]);
    problem.set_visible(false);
    widget.append(&problem);
    widget.append(&tabs);
    Header { widget, crumbs, titles, title, entry, meta, problem, tabs, overview, conversation, tasks, record }
}

/// What the director answered work in the person's place.
fn director_answers(loaded: &Loaded, id: &str) -> Vec<String> {
    let events: &[Event] = match loaded {
        Loaded::Goal(g) => &g.events,
        Loaded::Work(_, e) => e,
    };
    events.iter().filter(|e| e.work == id && e.answered_by_director()).map(|e| e.body.clone()).collect()
}

/// The ledger's record of work, newest first.
fn record_turns(events: &[Event]) -> Vec<Turn> {
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

/// The tasks a goal was planned into, grouped as planned, each a way into
/// its conversation.
fn plan_block(
    plan: &[(String, String, WorkState, Option<String>)],
    rollup: (i64, i64),
    page: Weak<WorkPage>,
) -> gtk::Widget {
    if plan.is_empty() {
        return ui::text("Not planned yet.", &["callout", "text2"]).upcast();
    }
    let card = ui::card("raised");
    let head = ui::hbox(SPACE_2);
    head.append(&ui::line("Plan", &["meta", "strong", "text2"]));
    head.append(&ui::line(&format!("{} of {} done", rollup.0, rollup.1), &["meta", "text3"]));
    card.append(&head);
    let mut headings: Vec<&str> = Vec::new();
    for (_, _, _, h) in plan {
        let h = h.as_deref().unwrap_or("");
        if !headings.contains(&h) {
            headings.push(h);
        }
    }
    for heading in headings {
        if !heading.is_empty() && heading != "(no heading)" {
            card.append(&ui::line(heading, &["meta", "strong", "text2"]));
        }
        for (id, title, state, _) in plan.iter().filter(|t| t.3.as_deref().unwrap_or("") == heading) {
            let row = ui::hbox(SPACE_2);
            row.append(&ui::state_icon(*state));
            row.append(&ui::line(title, &["callout"]));
            let button = gtk::Button::builder().child(&row).tooltip_text("Show this task's conversation").build();
            button.add_css_class("flat");
            let (page, id) = (page.clone(), id.clone());
            button.connect_clicked(move |_| {
                if let Some(page) = page.upgrade() {
                    page.focus_task(&id);
                }
            });
            card.append(&button);
        }
    }
    card.upcast()
}

/// A task's name as a link into its conversation, or plain when it is the goal.
fn task_link(task: &Option<(String, String, WorkState)>, page: &Weak<WorkPage>) -> Option<gtk::Widget> {
    let (id, title, _) = task.as_ref()?;
    let link = ui::link(title);
    link.add_css_class("speaker");
    let (page, id) = (page.clone(), id.clone());
    link.connect_clicked(move |_| {
        if let Some(page) = page.upgrade() {
            page.focus_task(&id);
        }
    });
    Some(link.upcast())
}

/// One thing that happened on a goal or a task, as its kind reads.
fn event_turn(
    e: &Event,
    report: Option<&Report>,
    task: Option<(String, String, WorkState)>,
    page: Weak<WorkPage>,
) -> gtk::Widget {
    let on = task.as_ref().map(|t| t.1.clone());
    match e.kind.as_str() {
        "report" => {
            let card = ui::card("raised");
            let head = ui::hbox(SPACE_2);
            head.append(&ui::line("Report", &["callout", "strong"]));
            if let Some(link) = task_link(&task, &page) {
                head.append(&link);
            }
            let verdict = report.map(|r| r.verdict.clone()).filter(|v| !v.is_empty()).or_else(|| verdict(&e.body));
            if let Some(verdict) = verdict {
                head.append(&verdict_pill(&verdict));
            }
            let at = ui::line(&kit::time::when(&e.at), &["meta", "text3"]);
            at.set_hexpand(true);
            at.set_xalign(1.0);
            head.append(&at);
            card.append(&head);
            match report {
                Some(r) => card.append(&report_view(r)),
                None => card.append(&folded_markdown(&e.body, 12)),
            }
            card.upcast()
        }
        "sent" => {
            let speaker = match task_link(&task, &page) {
                Some(link) => {
                    let row = ui::hbox(SPACE_1);
                    row.append(&ui::line("Sent to", &["speaker"]));
                    row.append(&link);
                    row.upcast()
                }
                None => ui::speaker("Sent"),
            };
            let meta = ui::meta(&speaker, Some(e.at.as_str()), None);
            ui::turn(
                Some(&ui::avatar("mail-send-symbolic", &["text2"])),
                Some(&meta),
                &ui::folded(&e.body, 4, &["body"]),
            )
            .upcast()
        }
        "decision" | "answer" => director_answer(&e.answer_text(), Some(e.at.as_str()), on.as_deref()),
        "verify" => {
            let passed = e.body.starts_with("pass");
            let text = format!(
                "{}{} · {}",
                if passed { "Verified" } else { "Verify failed" },
                on.map(|t| format!(" · {t}")).unwrap_or_default(),
                kit::time::when(&e.at)
            );
            let classes: &[&str] = if passed { &[] } else { &["failure"] };
            ui::event_line(&text, classes).upcast()
        }
        "pr" => ui::event_line(&format!("Landed · {} · {}", e.body, kit::time::when(&e.at)), &[]).upcast(),
        "state" => {
            let who = on.unwrap_or_else(|| "The goal".to_string());
            let failed = matches!(e.body.as_str(), "blocked" | "abandoned");
            let classes: &[&str] = if failed { &["failure"] } else { &[] };
            ui::event_line(&format!("{who} {} · {}", e.body, kit::time::when(&e.at)), classes).upcast()
        }
        _ => {
            let meta = ui::meta(&ui::speaker(&e.kind), Some(e.at.as_str()), on.as_deref());
            ui::turn(None, Some(&meta), &ui::folded(&e.body, 6, &["body"])).upcast()
        }
    }
}

/// A report's verdict: the value of its `STATUS:` line.
fn verdict(body: &str) -> Option<String> {
    body.lines().find_map(|l| l.trim().strip_prefix("STATUS:")).map(|v| v.trim().to_string()).filter(|v| !v.is_empty())
}

/// Markdown that folds after `lines` lines, with Show all to read the rest.
fn folded_markdown(body: &str, lines: usize) -> gtk::Widget {
    let total = body.lines().count();
    if total <= lines {
        return ui::markdown(body).upcast();
    }
    let column = ui::vbox(SPACE_2);
    let short: String = body.lines().take(lines).collect::<Vec<_>>().join("\n");
    let shown = gtk::Stack::builder().vhomogeneous(false).build();
    shown.add_named(&ui::markdown(&short), Some("short"));
    shown.add_named(&ui::markdown(body), Some("all"));
    shown.set_visible_child_name("short");
    let more = ui::link("Show all");
    let s = shown.clone();
    more.connect_clicked(move |b| {
        let all = s.visible_child_name().as_deref() == Some("all");
        s.set_visible_child_name(if all { "short" } else { "all" });
        b.set_label(if all { "Show all" } else { "Show less" });
    });
    column.append(&shown);
    column.append(&more);
    column.upcast()
}

/// The director answering in your place, settled: what it answered, and that it was the director.
fn director_answer(text: &str, at: Option<&str>, on: Option<&str>) -> gtk::Widget {
    let card = ui::card("settled");
    let detail = on.map(|t| format!("on {t}"));
    card.append(&ui::meta(&ui::speaker("Director answered"), at, detail.as_deref()));
    card.append(&ui::said(text, &["body"]));
    ui::turn(Some(&ui::avatar("emblem-ok-symbolic", &["settled-text"])), None, &card).upcast()
}

/// A question and its answer, read together.
fn exchange_turn(
    q: &Event,
    answer: Option<&Event>,
    asker: &str,
    task: Option<String>,
    page: Weak<WorkPage>,
) -> gtk::Widget {
    let body = ui::vbox(SPACE_3);
    for item in q.asked_items() {
        body.append(&ui::said(&item.question, &["body"]));
        if !item.choices.is_empty() {
            let choices = ui::vbox(SPACE_1);
            for choice in &item.choices {
                let chip = ui::tag(choice, &["callout"]);
                chip.set_halign(gtk::Align::Start);
                choices.append(&chip);
            }
            body.append(&choices);
        }
    }
    match answer {
        Some(a) if a.answered_by_director() => {
            let detail = a
                .decision
                .as_ref()
                .map(|d| d.model.clone())
                .filter(|m| !m.is_empty())
                .unwrap_or_else(|| "from what was already decided".into());
            let card = ui::card("settled");
            card.append(&ui::meta(&ui::speaker("Director answered"), Some(a.at.as_str()), Some(detail.as_str())));
            card.append(&ui::said(&a.answer_text(), &["body"]));
            body.append(&card);
        }
        Some(a) => body.append(&ui::you_bubble(&a.answer_text(), "You answered", Some(a.at.as_str()), None)),
        None => body.append(&ui::line("Not answered yet", &["meta", "text3"])),
    }
    let speaker: gtk::Widget = match task {
        Some(id) => {
            let link = ui::link(asker);
            link.add_css_class("speaker");
            link.connect_clicked(move |_| {
                if let Some(page) = page.upgrade() {
                    page.focus_task(&id);
                }
            });
            link.upcast()
        }
        None => ui::speaker(asker),
    };
    let meta = ui::meta(&speaker, Some(q.at.as_str()), Some("asked"));
    ui::turn(Some(&ui::avatar("dialog-question-symbolic", &["text2"])), Some(&meta), &body).upcast()
}

/// A supervisor's reply: what it said, then each agent it told, started or
/// filed, live, and what was refused.
fn supervisor_turn(
    e: &Event,
    reply: &kit::SupervisorReply,
    tasks: &[(Option<Work>, Phase, Option<String>)],
    goal: &str,
    page: Weak<WorkPage>,
) -> gtk::Widget {
    let body = supervisor_body(reply, tasks, page.clone());
    let speaker = ui::link("Supervisor");
    speaker.add_css_class("speaker");
    let goal = goal.to_string();
    speaker.connect_clicked(move |_| {
        if let Some(page) = page.upgrade() {
            page.focus_task(&goal);
        }
    });
    let meta = ui::meta(&speaker.upcast(), Some(e.at.as_str()), None);
    ui::turn(Some(&ui::avatar("security-high-symbolic", &["text2"])), Some(&meta), &body).upcast()
}

/// What a supervisor said, the agents it told, started or filed, and what was refused.
fn supervisor_body(
    reply: &kit::SupervisorReply,
    tasks: &[(Option<Work>, Phase, Option<String>)],
    page: Weak<WorkPage>,
) -> gtk::Box {
    let body = ui::vbox(SPACE_3);
    if !reply.said.is_empty() {
        body.append(&ui::markdown(&reply.said));
    }
    let chips = ui::vbox(SPACE_2);
    for (action, (task, phase, now)) in reply.actions.iter().zip(tasks) {
        match task {
            Some(task) => chips.append(&agent_chip(task, *phase, &action.line, now.as_deref(), page.clone())),
            None => {
                let row = ui::hbox(SPACE_2);
                row.append(&ui::icon("object-select-symbolic", &["text2"]));
                row.append(&ui::text(&action.line, &["callout", "text2"]));
                chips.append(&row);
            }
        }
    }
    if chips.first_child().is_some() {
        body.append(&chips);
    }
    for line in &reply.refused {
        let row = ui::hbox(SPACE_2);
        row.append(&ui::icon("window-close-symbolic", &["failure"]));
        row.append(&ui::said(line, &["callout", "failure"]));
        body.append(&row);
    }
    body
}

/// An agent inside the conversation: where its session is, what it is
/// doing, and what it was told.
fn agent_chip(task: &Work, phase: Phase, told: &str, now: Option<&str>, page: Weak<WorkPage>) -> gtk::Widget {
    let row = ui::hbox(SPACE_2);
    let mark = phase_mark(phase);
    mark.set_valign(gtk::Align::Start);
    mark.set_margin_top(SPACE_1);
    row.append(&mark);
    let column = ui::vbox(2);
    column.set_hexpand(true);
    let top = ui::hbox(SPACE_2);
    top.append(&ui::line(&task.title, &["callout", "strong"]));
    top.append(&ui::state_icon(task.state));
    column.append(&top);
    column.append(&ui::line(told, &["meta", "text2"]));
    if let Some(now) = now.filter(|n| !n.is_empty()) {
        column.append(&ui::line(now, &["meta", "text3"]));
    }
    row.append(&column);
    row.append(&ui::icon("go-next-symbolic", &["text3"]));
    let button = gtk::Button::builder().child(&row).tooltip_text("Show this agent's conversation").build();
    button.add_css_class("agent-chip");
    let id = task.id.clone();
    button.connect_clicked(move |_| {
        if let Some(page) = page.upgrade() {
            page.focus_task(&id);
        }
    });
    button.upcast()
}

/// A task that stopped without a report, and what it last said.
fn stopped_turn(e: &Event, said: &str, task: Option<(String, String, WorkState)>, page: Weak<WorkPage>) -> gtk::Widget {
    let avatar = task.as_ref().map(|t| ui::state_icon(t.2).upcast::<gtk::Widget>());
    let speaker = task_link(&task, &page).unwrap_or_else(|| ui::speaker("A task"));
    let meta = ui::meta(&speaker, Some(e.at.as_str()), Some("stopped without a report"));
    ui::turn(avatar.as_ref(), Some(&meta), &ui::folded(said, 3, &["body", "text2"])).upcast()
}

/// An agent's turn: its mark and name once, then what it said and did.
fn agent_turn(head: Option<(String, WorkState, Option<String>)>, body: gtk::Widget) -> gtk::Widget {
    match head {
        Some((title, state, at)) => {
            let meta = ui::meta(&ui::speaker(&title), at.as_deref(), None);
            ui::turn(Some(&ui::state_icon(state).upcast()), Some(&meta), &body).upcast()
        }
        None => ui::turn(None, None, &body).upcast(),
    }
}

fn agent_body(step: &Step, open: bool) -> gtk::Widget {
    match step.kind {
        StepKind::Thinking => {
            let expander = gtk::Expander::new(None);
            expander.set_label_widget(Some(&ui::line("Thinking", &["callout", "text3"])));
            expander.set_child(Some(&ui::said(step.text(), &["callout", "text2"])));
            expander.set_expanded(open);
            expander.upcast()
        }
        StepKind::Tool => match &step.tool {
            Some(call) => tool_row(call),
            None => ui::line("a tool call", &["callout", "text2"]).upcast(),
        },
        _ => ui::markdown(step.text()).upcast(),
    }
}

/// One tool call in a line; its input and result on disclosure.
fn tool_row(call: &ToolCall) -> gtk::Widget {
    let head = ui::hbox(SPACE_2);
    head.append(&ui::line(&call.name, &["callout", "strong", "text2"]));
    let summary = ui::line(&call.summary, &["callout", "text2"]);
    summary.set_hexpand(true);
    head.append(&summary);
    if call.failed {
        head.append(&ui::icon("dialog-error-symbolic", &["failure"]));
    } else if call.result.is_none() {
        let spinner = gtk::Spinner::new();
        spinner.start();
        head.append(&spinner);
    }
    let detail = ui::vbox(SPACE_2);
    if !call.input.is_empty() {
        detail.append(&ui::code_block(&call.input));
    }
    if let Some(result) = &call.result {
        detail.append(&ui::code_block(result));
    }
    let expander = gtk::Expander::new(None);
    expander.set_label_widget(Some(&head));
    expander.set_child(Some(&detail));
    expander.upcast()
}

/// A run of tool calls: one line each, a run of more than three folded into
/// "N tool calls".
fn tool_group(calls: &[ToolCall]) -> gtk::Widget {
    let column = ui::vbox(SPACE_1);
    for call in calls {
        column.append(&tool_row(call));
    }
    if calls.len() <= 3 {
        return column.upcast();
    }
    let expander = gtk::Expander::new(None);
    expander.set_label_widget(Some(&ui::line(&format!("{} tool calls", calls.len()), &["callout", "text2"])));
    expander.set_child(Some(&column));
    expander.upcast()
}

/// A message on its way: your bubble with where it is, and when wd refused
/// it, why, with Retry and Discard.
fn outgoing_bubble(app: &App, m: &Outgoing, waiting: &str) -> gtk::Widget {
    let status = match &m.state {
        OutgoingState::Sending => "· Sending…".to_string(),
        OutgoingState::Sent => format!("· {waiting}"),
        OutgoingState::Failed { .. } => "· not sent".to_string(),
    };
    let status: gtk::Widget = ui::line(&status, &["text3"]).upcast();
    let column = ui::you_bubble(&m.text, "You", None, Some(&status));
    if let OutgoingState::Failed { why, .. } = &m.state {
        let row = ui::hbox(SPACE_2);
        row.set_halign(gtk::Align::End);
        let why = ui::said(why, &["meta", "failure"]);
        why.set_max_width_chars(60);
        row.append(&why);
        let retry = ui::link("Retry");
        retry.add_css_class("failure");
        let discard = ui::link("Discard");
        discard.add_css_class("failure");
        let (a, id) = (app.clone(), m.id);
        retry.connect_clicked(move |_| a.store.retry(id));
        let a = app.clone();
        discard.connect_clicked(move |_| a.store.discard(id));
        row.append(&retry);
        row.append(&discard);
        column.append(&row);
    }
    column.upcast()
}

/// A report in parts: what it said with its verdict, then its files as path
/// chips with any note, then each named section; the first two show and the
/// rest open with "Show N more".
fn report_view(r: &Report) -> gtk::Widget {
    let column = ui::vbox(SPACE_3);
    if !r.said.is_empty() {
        column.append(&ui::said(&r.said, &["body"]));
    }
    if !r.files.is_empty() || !r.file_note.is_empty() {
        let value = ui::vbox(SPACE_1);
        let chips = gtk::FlowBox::builder()
            .selection_mode(gtk::SelectionMode::None)
            .column_spacing(SPACE_1 as u32)
            .row_spacing(SPACE_1 as u32)
            .max_children_per_line(12)
            .build();
        for file in &r.files {
            chips.insert(&ui::tag(file, &["code"]), -1);
        }
        value.append(&chips);
        if !r.file_note.is_empty() {
            value.append(&ui::said(&r.file_note, &["callout", "text2"]));
        }
        column.append(&field("Files", &value));
    }
    let rest = ui::vbox(SPACE_3);
    for (i, section) in r.sections.iter().enumerate() {
        let shown = field(&title_case(&section.name), &ui::markdown(&section.text));
        if i < 2 {
            column.append(&shown);
        } else {
            rest.append(&shown);
        }
    }
    let more = r.sections.len().saturating_sub(2);
    if more > 0 {
        let revealer = gtk::Revealer::builder().child(&rest).reveal_child(false).build();
        let open = ui::link(&format!("Show {more} more"));
        let r = revealer.clone();
        open.connect_clicked(move |b| {
            let now = !r.reveals_child();
            r.set_reveal_child(now);
            let label = if now { "Show less".to_string() } else { format!("Show {more} more") };
            b.set_label(&label);
        });
        column.append(&revealer);
        column.append(&open);
    }
    column.upcast()
}

/// A labelled field: the name in a 64 px column, the value beside it.
fn field(name: &str, value: &impl IsA<gtk::Widget>) -> gtk::Box {
    let row = ui::hbox(SPACE_3);
    let label = ui::line(name, &["meta", "text2"]);
    label.set_size_request(64, -1);
    label.set_valign(gtk::Align::Start);
    row.append(&label);
    value.set_hexpand(true);
    row.append(value);
    row
}

/// VERIFY as Verify.
fn title_case(name: &str) -> String {
    let lower = name.to_lowercase();
    let mut chars = lower.chars();
    chars.next().map(|c| c.to_uppercase().chain(chars).collect()).unwrap_or_default()
}

/// A verdict as a pill: done green, blocked red, needs-input orange.
fn verdict_pill(verdict: &str) -> gtk::Label {
    let class = match verdict.to_lowercase().as_str() {
        "done" => "settled-text",
        "blocked" => "failure",
        "needs-input" => "attention-text",
        _ => "text2",
    };
    ui::tag(verdict, &["meta", class])
}

/// The chips a goal's conversation is narrowed with, each with its count.
fn filter_chips(counts: &[(ThreadFilter, usize)], chosen: ThreadFilter, page: Weak<WorkPage>) -> gtk::Widget {
    let row = ui::hbox(SPACE_2);
    let mut first: Option<gtk::ToggleButton> = None;
    for (filter, n) in counts {
        let chip =
            gtk::ToggleButton::builder().label(format!("{} {n}", filter.label())).active(*filter == chosen).build();
        chip.add_css_class("chip");
        match &first {
            Some(group) => chip.set_group(Some(group)),
            None => first = Some(chip.clone()),
        }
        let (page, filter) = (page.clone(), *filter);
        chip.connect_toggled(move |c| {
            if c.is_active() {
                if let Some(page) = page.upgrade() {
                    page.set_filter(filter);
                }
            }
        });
        row.append(&chip);
    }
    row.upcast()
}

/// A run of updates folded into one line that opens.
fn updates_row(count: usize, at: &str, inner: Vec<Turn>) -> gtk::Widget {
    let column = ui::vbox(SPACE_3);
    for turn in inner {
        column.append(&turn.build());
    }
    let expander = gtk::Expander::new(None);
    expander
        .set_label_widget(Some(&ui::line(&format!("{count} updates · {}", kit::time::when(at)), &["meta", "text3"])));
    expander.set_child(Some(&column));
    expander.upcast()
}

/// A section's small title on the overview; orange for what waits on you.
fn section_title(title: &str, attention: bool) -> gtk::Widget {
    ui::micro(title, &[if attention { "attention-text" } else { "text3" }]).upcast()
}

/// Done of total, a bar segmented by where each task stands, and a legend.
fn progress_card(p: &TaskProgress) -> gtk::Widget {
    let card = ui::card("raised");
    let head = ui::hbox(SPACE_2);
    head.append(&ui::line(&format!("{} of {}", p.done, p.total()), &["title", "numeric"]));
    head.append(&ui::line("tasks done", &["callout", "text2"]));
    card.append(&head);
    let parts = [
        ("Done", p.done, "bar-done"),
        ("Working", p.working, "bar-working"),
        ("Review", p.review, "bar-review"),
        ("Blocked", p.blocked, "bar-blocked"),
        ("Waiting", p.waiting, "bar-waiting"),
        ("Dropped", p.dropped, "bar-dropped"),
    ];
    // One equal cell per task, coloured by where it stands, so each state's
    // share of the bar is its share of the tasks.
    let bar = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    bar.set_homogeneous(true);
    bar.add_css_class("progress-bar");
    for (_, n, class) in parts {
        for _ in 0..n {
            let cell = gtk::Box::new(gtk::Orientation::Horizontal, 0);
            cell.add_css_class(class);
            bar.append(&cell);
        }
    }
    if p.total() > 0 {
        card.append(&bar);
    }
    let legend = ui::hbox(SPACE_4);
    for (name, n, class) in parts.into_iter().filter(|part| part.1 > 0) {
        let item = ui::hbox(SPACE_1);
        let dot = gtk::Box::new(gtk::Orientation::Horizontal, 0);
        dot.add_css_class("dot");
        dot.add_css_class(class);
        dot.set_valign(gtk::Align::Center);
        item.append(&dot);
        item.append(&ui::line(&format!("{name} {n}"), &["meta", "text2"]));
        legend.append(&item);
    }
    card.append(&legend);
    card.upcast()
}

/// One row of the tasks table.
#[derive(Debug, Clone)]
struct TaskLine {
    task: Work,
    now: Option<String>,
    asking: bool,
    verdict: Option<String>,
    tokens: Option<i64>,
}

/// Needs input and blocked first, then what is moving, then what rests.
fn rank(state: WorkState) -> usize {
    [
        WorkState::NeedsInput,
        WorkState::Blocked,
        WorkState::Running,
        WorkState::Review,
        WorkState::Queued,
        WorkState::Briefed,
        WorkState::Paused,
        WorkState::SoftDone,
        WorkState::Done,
        WorkState::Dropped,
        WorkState::Abandoned,
    ]
    .iter()
    .position(|s| *s == state)
    .unwrap_or(usize::MAX)
}

fn tasks_table(rows: &[TaskLine], sort: Sort, app: &App, page: Weak<WorkPage>) -> gtk::Widget {
    let column = ui::vbox(SPACE_3);
    let head = ui::hbox(SPACE_2);
    let count = ui::line(&format!("{} tasks", rows.len()), &["meta", "text2"]);
    count.set_hexpand(true);
    head.append(&count);
    let sorts = ui::hbox(0);
    sorts.add_css_class("linked");
    let mut first: Option<gtk::ToggleButton> = None;
    for (label, by) in [("Plan", Sort::Plan), ("State", Sort::State), ("Recent", Sort::Recent)] {
        let b = gtk::ToggleButton::builder().label(label).active(by == sort).build();
        match &first {
            Some(group) => b.set_group(Some(group)),
            None => first = Some(b.clone()),
        }
        let page = page.clone();
        b.connect_toggled(move |b| {
            if b.is_active() {
                if let Some(page) = page.upgrade() {
                    page.set_sort(by);
                }
            }
        });
        sorts.append(&b);
    }
    head.append(&sorts);
    column.append(&head);

    let list = gtk::ListBox::new();
    list.add_css_class("boxed-list");
    list.set_selection_mode(gtk::SelectionMode::None);
    let now = chrono::Utc::now();
    for r in rows {
        let row = ui::hbox(SPACE_3);
        row.set_margin_top(SPACE_2);
        row.set_margin_bottom(SPACE_2);
        row.set_margin_start(SPACE_3);
        row.set_margin_end(SPACE_3);
        let name = ui::hbox(SPACE_2);
        name.set_hexpand(true);
        name.append(&ui::state_icon(r.task.state));
        name.append(&ui::line(&r.task.title, &["callout"]));
        row.append(&name);
        let doing =
            ui::line(r.now.as_deref().unwrap_or("—"), &["callout", if r.asking { "attention-text" } else { "text2" }]);
        doing.set_width_chars(28);
        doing.set_max_width_chars(28);
        row.append(&doing);
        let verdict: gtk::Widget = match &r.verdict {
            Some(v) => verdict_pill(v).upcast(),
            None => ui::line("—", &["text3"]).upcast(),
        };
        verdict.set_size_request(76, -1);
        row.append(&verdict);
        let moved = ui::line(&kit::time::ago(&r.task.updated, now), &["meta", "numeric", "text2"]);
        moved.set_width_chars(5);
        moved.set_xalign(1.0);
        row.append(&moved);
        let tokens =
            ui::line(&r.tokens.map(kit::token_count).unwrap_or_else(|| "—".into()), &["meta", "numeric", "text2"]);
        tokens.set_width_chars(6);
        tokens.set_xalign(1.0);
        row.append(&tokens);
        let item = gtk::ListBoxRow::builder().child(&row).activatable(true).build();
        list.append(&item);
    }
    let (app, ids) = (app.clone(), rows.iter().map(|r| r.task.id.clone()).collect::<Vec<_>>());
    list.connect_row_activated(move |_, row| {
        if let Some(id) = usize::try_from(row.index()).ok().and_then(|i| ids.get(i)) {
            app.open(Place::Work(id.clone()));
        }
    });
    column.append(&list);
    column.upcast()
}
