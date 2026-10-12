//! What the window shows, read from wd and nowhere else. The store keeps no
//! state of its own that matters: quit and relaunch, and the ledger rebuilds it.
//!
//! Everything here runs on one thread (GTK's main loop, or a test's local
//! executor); `Spawner` is how the store starts work that outlives a call.

use std::cell::{Cell, Ref, RefCell};
use std::rc::{Rc, Weak};

use chrono::Utc;
use futures::future::LocalBoxFuture;
use futures::StreamExt;
use serde::de::DeserializeOwned;
use serde::Deserialize;

use crate::channel::{Channel, ChannelError};
use crate::conversation::{ConversationPage, Question, SessionStatus, Step, Subagent, Usage};
use crate::ledger::{
    ActionResult, Arrival, Board, Doctor, Event, Found, Gate, GoalDetail, Project, ReviewPass, Started, Taste, Work,
};
use crate::outbox::{Outgoing, OutgoingState};
use crate::plan::Plan;

/// Runs a future on the thread the store lives on.
pub type Spawner = Rc<dyn Fn(LocalBoxFuture<'static, ()>)>;

/// What changed in the store, so a view reads only what it shows.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Change {
    Board,
    Projects,
    Outbox,
    Failure,
}

type Watcher = Rc<dyn Fn(Change)>;

pub struct Store {
    channel: Channel,
    spawn: Spawner,
    me: Weak<Store>,
    board: RefCell<Option<Board>>,
    projects: RefCell<Vec<Project>>,
    /// The last failure to read from wd, shown until the next read succeeds.
    failure: RefCell<Option<String>>,
    /// How many times the board has been read.
    reads: Cell<u64>,
    /// Messages sent to work, oldest first, from the moment they are sent.
    outbox: RefCell<Vec<Outgoing>>,
    next_message: Cell<u64>,
    refreshing: Cell<bool>,
    stale: Cell<bool>,
    watchers: RefCell<Vec<(u64, Watcher)>>,
    next_watch: Cell<u64>,
    arrived: RefCell<Option<Rc<dyn Fn(&Arrival)>>>,
}

/// A view's subscription to the store; dropping it ends the subscription.
pub struct Watch {
    store: Weak<Store>,
    id: u64,
}

impl Drop for Watch {
    fn drop(&mut self) {
        if let Some(store) = self.store.upgrade() {
            store.watchers.borrow_mut().retain(|(id, _)| *id != self.id);
        }
    }
}

#[derive(Deserialize)]
struct Stopped {
    withdrawn: Vec<String>,
}

fn decode<T: DeserializeOwned>(result: ActionResult) -> Result<T, ChannelError> {
    if result.code != 0 {
        return Err(ChannelError::Refused { kind: format!("exit {}", result.code), message: result.complaint().to_string() });
    }
    serde_json::from_str(&result.stdout)
        .map_err(|e| ChannelError::Unreadable(format!("{}: {e}", std::any::type_name::<T>())))
}

impl Store {
    pub fn new(channel: Channel, spawn: Spawner) -> Rc<Store> {
        Rc::new_cyclic(|me| Store {
            channel,
            spawn,
            me: me.clone(),
            board: RefCell::new(None),
            projects: RefCell::new(Vec::new()),
            failure: RefCell::new(None),
            reads: Cell::new(0),
            outbox: RefCell::new(Vec::new()),
            next_message: Cell::new(1),
            refreshing: Cell::new(false),
            stale: Cell::new(false),
            watchers: RefCell::new(Vec::new()),
            next_watch: Cell::new(1),
            arrived: RefCell::new(None),
        })
    }

    pub fn board(&self) -> Ref<'_, Option<Board>> {
        self.board.borrow()
    }

    pub fn projects(&self) -> Ref<'_, Vec<Project>> {
        self.projects.borrow()
    }

    pub fn failure(&self) -> Option<String> {
        self.failure.borrow().clone()
    }

    pub fn reads(&self) -> u64 {
        self.reads.get()
    }

    pub fn outbox(&self) -> Ref<'_, Vec<Outgoing>> {
        self.outbox.borrow()
    }

    /// Calls `f` after each change until the returned watch is dropped.
    pub fn watch(&self, f: impl Fn(Change) + 'static) -> Watch {
        let id = self.next_watch.get();
        self.next_watch.set(id + 1);
        self.watchers.borrow_mut().push((id, Rc::new(f)));
        Watch { store: self.me.clone(), id }
    }

    /// Told about each work that comes to wait on a person, once.
    pub fn on_arrival(&self, f: impl Fn(&Arrival) + 'static) {
        *self.arrived.borrow_mut() = Some(Rc::new(f));
    }

    fn changed(&self, change: Change) {
        let watchers: Vec<Watcher> = self.watchers.borrow().iter().map(|(_, w)| Rc::clone(w)).collect();
        for w in watchers {
            w(change);
        }
    }

    fn fail(&self, failure: Option<String>) {
        if *self.failure.borrow() != failure {
            *self.failure.borrow_mut() = failure;
            self.changed(Change::Failure);
        }
    }

    fn spawn(&self, f: impl std::future::Future<Output = ()> + 'static) {
        (self.spawn)(Box::pin(f));
    }

    /// Reads the board and the products, then reads the board again on every
    /// ledger event any wd process records.
    pub async fn start(&self) {
        self.refresh().await;
        if let Err(e) = self.reload_projects().await {
            self.fail(Some(e.to_string()));
        }
        let Some(mut events) = self.channel.events() else {
            self.fail(Some("the ledger's events are already followed".into()));
            return;
        };
        let me = self.me.clone();
        self.spawn(async move {
            while events.next().await.is_some() {
                let Some(store) = me.upgrade() else { return };
                store.refresh().await;
            }
            if let Some(store) = me.upgrade() {
                store.fail(Some("wd stopped; relaunch the app".into()));
            }
        });
    }

    /// Ends wd; what it holds is answered first.
    pub fn stop(&self) {
        self.channel.close();
    }

    /// Reads the board again. Events arriving during a read coalesce into one
    /// more read, so a burst of events never queues a burst of reads.
    pub async fn refresh(&self) {
        if self.refreshing.get() {
            self.stale.set(true);
            return;
        }
        self.refreshing.set(true);
        loop {
            self.stale.set(false);
            match self.channel.board().await {
                Ok(now) => {
                    let arrivals = now.arrivals(self.board.borrow().as_ref());
                    *self.board.borrow_mut() = Some(now);
                    self.reads.set(self.reads.get() + 1);
                    self.fail(None);
                    self.changed(Change::Board);
                    let told = self.arrived.borrow().clone();
                    if let Some(told) = told {
                        for a in &arrivals {
                            told(a);
                        }
                    }
                }
                Err(e) => self.fail(Some(e.to_string())),
            }
            if !self.stale.get() {
                break;
            }
        }
        self.refreshing.set(false);
    }

    pub async fn goal(&self, id: &str) -> Result<GoalDetail, ChannelError> {
        self.channel.goal(id).await
    }

    pub async fn work(&self, id: &str) -> Result<Work, ChannelError> {
        self.channel.work(id).await
    }

    pub async fn events_of(&self, id: &str) -> Result<Vec<Event>, ChannelError> {
        self.channel.events_of(id).await
    }

    /// The next gate on the work's way to done.
    pub async fn gate(&self, id: &str) -> Result<Gate, ChannelError> {
        self.channel.gate(id).await
    }

    pub async fn conversation(&self, id: &str, from: usize, agent: Option<&str>) -> Result<ConversationPage, ChannelError> {
        self.channel.conversation(id, from, agent).await
    }

    pub async fn review(&self, project: &str) -> Result<ReviewPass, ChannelError> {
        decode(self.run_argv(&["review", "--project", project, "--json"]).await?)
    }

    /// The rules in effect and what feedback has gathered.
    pub async fn taste(&self) -> Result<Taste, ChannelError> {
        decode(self.run_argv(&["taste", "--json"]).await?)
    }

    /// The runners this host can use.
    pub async fn doctor(&self) -> Result<Doctor, ChannelError> {
        decode(self.run_argv(&["doctor", "--json"]).await?)
    }

    /// Reads the products again, after one is added.
    pub async fn reload_projects(&self) -> Result<(), ChannelError> {
        let projects: Vec<Project> = decode(self.run_argv(&["projects", "--json"]).await?)?;
        *self.projects.borrow_mut() = projects;
        self.changed(Change::Projects);
        Ok(())
    }

    /// Work put out of sight, newest first as wd lists it.
    pub async fn archived(&self) -> Result<Vec<Work>, ChannelError> {
        decode(self.run_argv(&["status", "--archived", "--json"]).await?)
    }

    /// Asks one bounded model call which of a product's goals a request continues.
    pub async fn find(&self, project: &str, text: &str) -> Result<Vec<Found>, ChannelError> {
        decode(self.run_argv(&["goal", "find", project, text, "--json"]).await?)
    }

    /// Runs a start or continue plan and reads back the goal and its tasks.
    pub async fn start_goal(&self, plan: &Plan) -> Result<Started, ChannelError> {
        let started = decode(self.run(plan).await?)?;
        self.refresh().await;
        Ok(started)
    }

    /// Runs a stop plan and returns what it took back unread, oldest first, as
    /// one text to edit and send again; empty when nothing was waiting.
    pub async fn stop_work(&self, plan: &Plan) -> Result<String, ChannelError> {
        let stopped: Stopped = decode(self.run(plan).await?)?;
        self.refresh().await;
        Ok(stopped.withdrawn.join("\n\n"))
    }

    /// Runs a plan: its argv, unchanged.
    pub async fn run(&self, plan: &Plan) -> Result<ActionResult, ChannelError> {
        self.channel.action(&plan.argv).await
    }

    async fn run_argv(&self, argv: &[&str]) -> Result<ActionResult, ChannelError> {
        let argv: Vec<String> = argv.iter().map(|s| s.to_string()).collect();
        self.channel.action(&argv).await
    }

    /// Sends text to work without the person waiting on it: it is in the
    /// outbox at once, and `then` runs once `wd` has taken it.
    pub fn post(&self, plan: Plan, work: &str, then: Option<Plan>) -> Outgoing {
        let id = self.next_message.get();
        self.next_message.set(id + 1);
        let message = Outgoing {
            id,
            work: work.to_string(),
            text: plan.argv.last().cloned().unwrap_or_default(),
            at: Utc::now(),
            state: OutgoingState::Sending,
            plan,
            then,
        };
        self.outbox.borrow_mut().push(message.clone());
        self.changed(Change::Outbox);
        self.deliver_later(id);
        message
    }

    /// Sends a refused message again; what `wd` already took is not sent twice.
    pub fn retry(&self, id: u64) {
        let failed = self.outbox.borrow().iter().any(|m| m.id == id && matches!(m.state, OutgoingState::Failed { .. }));
        if failed {
            self.deliver_later(id);
        }
    }

    pub fn discard(&self, id: u64) {
        self.outbox.borrow_mut().retain(|m| m.id != id);
        self.changed(Change::Outbox);
    }

    fn deliver_later(&self, id: u64) {
        let me = self.me.clone();
        self.spawn(async move {
            if let Some(store) = me.upgrade() {
                store.deliver(id).await;
            }
        });
    }

    fn set_state(&self, id: u64, state: OutgoingState) {
        if let Some(m) = self.outbox.borrow_mut().iter_mut().find(|m| m.id == id) {
            m.state = state;
        }
        self.changed(Change::Outbox);
    }

    async fn deliver(&self, id: u64) {
        let Some(message) = self.outbox.borrow().iter().find(|m| m.id == id).cloned() else { return };
        let already_sent = matches!(message.state, OutgoingState::Failed { sent: true, .. });
        self.set_state(id, OutgoingState::Sending);
        let mut state = OutgoingState::Sent;
        if !already_sent {
            match self.run(&message.plan).await {
                Ok(sent) if sent.code != 0 => {
                    state = OutgoingState::Failed { why: sent.complaint().to_string(), sent: false }
                }
                Ok(_) => {}
                Err(e) => state = OutgoingState::Failed { why: e.to_string(), sent: false },
            }
        }
        if let (OutgoingState::Sent, Some(then)) = (&state, &message.then) {
            match self.run(then).await {
                Ok(kept) if kept.code != 0 => {
                    state = OutgoingState::Failed { why: format!("sent, but not remembered: {}", kept.complaint()), sent: true }
                }
                Ok(_) => {}
                Err(e) => state = OutgoingState::Failed { why: format!("sent, but not remembered: {e}"), sent: true },
            }
        }
        self.set_state(id, state);
        self.refresh().await;
    }
}

/// A session's conversation as a page reads it.
#[derive(Debug, Clone, PartialEq)]
pub struct FeedState {
    pub steps: Vec<Step>,
    pub status: SessionStatus,
    pub asking: Option<Question>,
    pub failure: Option<String>,
    pub loaded: bool,
    pub usage: Option<Usage>,
    pub subagents: Vec<Subagent>,
}

/// Follows one work item's conversation: each read asks only from the first
/// step that can still change, so a long session costs one short read a beat.
pub struct Feed {
    store: Rc<Store>,
    pub id: String,
    /// The subagent followed, or none for the session itself.
    pub agent: Option<String>,
    state: RefCell<FeedState>,
}

impl Feed {
    pub fn new(store: Rc<Store>, id: &str, agent: Option<&str>) -> Feed {
        Feed {
            store,
            id: id.to_string(),
            agent: agent.map(str::to_string),
            state: RefCell::new(FeedState {
                steps: Vec::new(),
                status: SessionStatus::NoSession,
                asking: None,
                failure: None,
                loaded: false,
                usage: None,
                subagents: Vec::new(),
            }),
        }
    }

    pub fn state(&self) -> Ref<'_, FeedState> {
        self.state.borrow()
    }

    /// The first step that can still change; everything before it is final.
    pub fn stable(&self) -> usize {
        let state = self.state.borrow();
        state.steps.iter().position(|s| !s.settled()).unwrap_or(state.steps.len())
    }

    pub async fn read(&self) {
        let from = self.stable();
        let agent = self.agent.as_deref();
        let read = async {
            let page = self.store.conversation(&self.id, from, agent).await?;
            // A store that shrank (a session moved, a file replaced) is read
            // again whole rather than spliced onto steps it no longer has.
            if page.total < from {
                let whole = self.store.conversation(&self.id, 0, agent).await?;
                Ok::<_, ChannelError>((page, whole.entries))
            } else {
                let mut steps: Vec<Step> = self.state.borrow().steps.iter().take(from).cloned().collect();
                steps.extend(page.entries.iter().cloned());
                Ok((page, steps))
            }
        };
        let result = read.await;
        let mut state = self.state.borrow_mut();
        match result {
            Ok((page, steps)) => {
                state.steps = steps;
                state.status = page.status;
                state.asking = page.asking;
                state.usage = page.usage;
                state.subagents = page.subagents.unwrap_or_default();
                state.failure = None;
            }
            Err(e) => state.failure = Some(e.to_string()),
        }
        state.loaded = true;
    }
}
