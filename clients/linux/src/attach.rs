//! Files a person sends with what they say: waiting on the composer until
//! sent, then shown on the message they went with, as the goal view serves them.

use std::fmt;
use std::path::Path;

use serde::Deserialize;

use crate::ledger::{Event, GoalDetail};

/// A file as the goal view serves it: stored by `wd`, with the event that
/// recorded it and the work and moment it was sent to.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Deserialize)]
pub struct Attachment {
    pub event: i64,
    pub work: String,
    pub at: String,
    pub name: String,
    /// Where `wd` keeps its copy.
    pub path: String,
    pub size: i64,
    pub media_type: String,
}

impl Attachment {
    pub fn is_image(&self) -> bool {
        self.media_type.starts_with("image/")
    }

    pub fn size_label(&self) -> String {
        size_label(self.size)
    }
}

/// A size as a person reads it: 512 B, 12 KB, 3.4 MB.
pub fn size_label(bytes: i64) -> String {
    const K: f64 = 1024.0;
    let b = bytes as f64;
    if b < K {
        format!("{bytes} B")
    } else if b < K * K {
        format!("{} KB", (b / K).round() as i64)
    } else if b < K * K * K {
        format!("{:.1} MB", b / (K * K))
    } else {
        format!("{:.1} GB", b / (K * K * K))
    }
}

/// A file waiting on the composer to be sent.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct PendingFile {
    pub path: String,
    pub name: String,
    pub size: i64,
    /// Whether it shows as a thumbnail rather than a name.
    pub image: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AttachError {
    /// A folder: its files are attached, not it.
    Folder(String),
    Unreadable(String, String),
}

impl fmt::Display for AttachError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AttachError::Folder(p) => write!(f, "cannot attach {p}: it is a folder; attach the files in it"),
            AttachError::Unreadable(p, why) => write!(f, "cannot attach {p}: {why}"),
        }
    }
}

impl std::error::Error for AttachError {}

const IMAGES: [&str; 8] = ["png", "jpg", "jpeg", "gif", "webp", "bmp", "svg", "tiff"];

impl PendingFile {
    /// A file on this computer to send; a folder, or a file that cannot be read, is refused.
    pub fn at(path: &Path) -> Result<PendingFile, AttachError> {
        let shown = path.display().to_string();
        let meta = std::fs::metadata(path).map_err(|e| AttachError::Unreadable(shown.clone(), e.to_string()))?;
        if meta.is_dir() {
            return Err(AttachError::Folder(shown));
        }
        std::fs::File::open(path).map_err(|e| AttachError::Unreadable(shown.clone(), e.to_string()))?;
        let name = path.file_name().map_or_else(|| shown.clone(), |n| n.to_string_lossy().into_owned());
        let image = path
            .extension()
            .and_then(|e| e.to_str())
            .is_some_and(|e| IMAGES.contains(&e.to_ascii_lowercase().as_str()));
        Ok(PendingFile { path: shown, name, size: meta.len() as i64, image })
    }

    pub fn size_label(&self) -> String {
        size_label(self.size)
    }
}

/// Adds a file to those waiting, once.
pub fn add(waiting: &mut Vec<PendingFile>, file: PendingFile) {
    if !waiting.iter().any(|w| w.path == file.path) {
        waiting.push(file);
    }
}

impl GoalDetail {
    /// The files sent with an event: those on its work at its moment.
    pub fn attached_to(&self, e: &Event) -> Vec<&Attachment> {
        self.attachments.iter().filter(|a| a.work == e.work && a.at == e.at).collect()
    }

    /// The files sent at the moment of an attachment event, shown on their
    /// own when no message carries them: for the first such event of each
    /// moment, every file of that moment; none otherwise.
    pub fn attached_alone(&self, e: &Event) -> Vec<&Attachment> {
        if e.kind != "attachment" {
            return Vec::new();
        }
        let carried = self.events.iter().any(|m| m.kind != "attachment" && m.work == e.work && m.at == e.at);
        let first = self
            .events
            .iter()
            .filter(|m| m.kind == "attachment" && m.work == e.work && m.at == e.at)
            .map(|m| m.id)
            .min();
        if carried || first != Some(e.id) {
            return Vec::new();
        }
        self.attached_to(e)
    }
}
