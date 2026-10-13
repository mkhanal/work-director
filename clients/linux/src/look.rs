//! How a state reads on screen. Each state has its own icon and label, so
//! needs-input (a question waiting) is never mistaken for blocked (a stop).

use crate::ledger::{Band, WorkState};

/// A colour the app maps to a CSS class, so this layer stays free of GTK.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Tint {
    Gray,
    Blue,
    Indigo,
    Orange,
    Red,
    Purple,
    Teal,
    Green,
    Brown,
}

impl Tint {
    pub fn css_class(self) -> &'static str {
        match self {
            Tint::Gray => "tint-gray",
            Tint::Blue => "tint-blue",
            Tint::Indigo => "tint-indigo",
            Tint::Orange => "tint-orange",
            Tint::Red => "tint-red",
            Tint::Purple => "tint-purple",
            Tint::Teal => "tint-teal",
            Tint::Green => "tint-green",
            Tint::Brown => "tint-brown",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct StateLook {
    pub label: &'static str,
    /// A symbolic icon name from the icon theme.
    pub icon: &'static str,
    pub tint: Tint,
}

impl WorkState {
    pub fn look(self) -> StateLook {
        let (label, icon, tint) = match self {
            WorkState::Queued => ("Queued", "document-open-recent-symbolic", Tint::Gray),
            WorkState::Briefed => ("Briefed", "text-x-generic-symbolic", Tint::Gray),
            WorkState::Running => ("Running", "media-playback-start-symbolic", Tint::Blue),
            WorkState::NeedsInput => ("Needs input", "dialog-question-symbolic", Tint::Orange),
            WorkState::Blocked => ("Blocked", "action-unavailable-symbolic", Tint::Red),
            WorkState::Review => ("In review", "edit-find-symbolic", Tint::Indigo),
            WorkState::SoftDone => ("Ready to close", "object-select-symbolic", Tint::Teal),
            WorkState::Done => ("Done", "emblem-ok-symbolic", Tint::Green),
            WorkState::Dropped => ("Dropped", "list-remove-symbolic", Tint::Gray),
            WorkState::Abandoned => ("Abandoned", "process-stop-symbolic", Tint::Brown),
            WorkState::Paused => ("Paused", "media-playback-pause-symbolic", Tint::Purple),
            WorkState::Superseded => ("Superseded", "edit-redo-symbolic", Tint::Gray),
        };
        StateLook { label, icon, tint }
    }
}

impl Band {
    pub fn label(self) -> &'static str {
        match self {
            Band::NeedsYou => "Needs You",
            Band::InFlight => "In Flight",
            Band::Paused => "Paused",
            Band::ReadyToPush => "Ready to Push",
            Band::InReview => "In Review",
            Band::ReadyToClose => "Ready to Close",
            Band::AtRest => "At Rest",
        }
    }

    pub fn css_class(self) -> &'static str {
        match self {
            Band::NeedsYou => "band-needs-you",
            Band::InFlight => "band-in-flight",
            Band::Paused => "band-paused",
            Band::ReadyToPush => "band-ready",
            Band::InReview => "band-in-review",
            Band::ReadyToClose => "band-ready",
            Band::AtRest => "band-at-rest",
        }
    }
}
