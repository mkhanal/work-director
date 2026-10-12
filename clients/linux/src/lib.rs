//! What the Linux app shows and writes, without GTK: the channel to
//! `wd serve --stdio`, the ledger as it serves it, and the plans a person runs.

pub mod agents;
pub mod channel;
pub mod conversation;
pub mod escape;
pub mod follow;
pub mod ledger;
pub mod locator;
pub mod look;
pub mod markdown;
pub mod new_goal;
pub mod outbox;
pub mod plan;
pub mod send_key;
pub mod store;
pub mod thread;
pub mod time;

pub use agents::{span, token_count, AgentCard, AgentRoster, Phase};
pub use channel::{Channel, ChannelError};
pub use conversation::*;
pub use escape::EscapeTaps;
pub use ledger::*;
pub use locator::{locate_wd, LocatorError};
pub use look::{StateLook, Tint};
pub use new_goal::{NewGoal, NewGoalPhase};
pub use outbox::{Outgoing, OutgoingState};
pub use plan::{Plan, PlanError};
pub use send_key::{Chord, ReturnPress, SendKey};
pub use store::{Change, Feed, FeedState, Spawner, Store, Watch};
pub use thread::{
    AskedItem, SupervisorAction, SupervisorReply, TaskProgress, ThreadFilter, ThreadItem, ThreadStep, WaitingTask,
};
