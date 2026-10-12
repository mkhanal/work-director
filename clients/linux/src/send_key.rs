//! The chord a person sends a message with. Every other chord with Return
//! starts a new line, so a long message never goes out half written.

/// The modifier keys held with Return.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Chord {
    pub shift: bool,
    pub control: bool,
    pub alt: bool,
    pub super_key: bool,
}

impl Chord {
    pub const NONE: Chord = Chord { shift: false, control: false, alt: false, super_key: false };
    pub const SHIFT: Chord = Chord { shift: true, ..Chord::NONE };
    pub const CONTROL: Chord = Chord { control: true, ..Chord::NONE };
    pub const ALT: Chord = Chord { alt: true, ..Chord::NONE };
}

/// What Return does in a message: send it, or start a new line.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ReturnPress {
    Send,
    Newline,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SendKey {
    Return,
    ControlReturn,
    AltReturn,
}

impl SendKey {
    pub const STANDARD: SendKey = SendKey::Return;
    pub const ALL: [SendKey; 3] = [SendKey::Return, SendKey::ControlReturn, SendKey::AltReturn];

    pub fn chord(self) -> Chord {
        match self {
            SendKey::Return => Chord::NONE,
            SendKey::ControlReturn => Chord::CONTROL,
            SendKey::AltReturn => Chord::ALT,
        }
    }

    /// The name it is stored and offered under.
    pub fn name(self) -> &'static str {
        match self {
            SendKey::Return => "return",
            SendKey::ControlReturn => "control-return",
            SendKey::AltReturn => "alt-return",
        }
    }

    pub fn named(name: &str) -> Option<SendKey> {
        SendKey::ALL.into_iter().find(|k| k.name() == name)
    }

    pub fn title(self) -> &'static str {
        match self {
            SendKey::Return => "Enter sends, Shift+Enter for a new line",
            SendKey::ControlReturn => "Ctrl+Enter sends, Enter for a new line",
            SendKey::AltReturn => "Alt+Enter sends, Enter for a new line",
        }
    }

    /// The chord as the hint under the composer names it.
    pub fn hint(self) -> &'static str {
        match self {
            SendKey::Return => "Enter to send",
            SendKey::ControlReturn => "Ctrl+Enter to send",
            SendKey::AltReturn => "Alt+Enter to send",
        }
    }

    pub fn press(self, held: Chord) -> ReturnPress {
        if held == self.chord() {
            ReturnPress::Send
        } else {
            ReturnPress::Newline
        }
    }
}
