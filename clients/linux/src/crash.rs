//! A panic leaves a note under `$WD_HOME/crashes/<time>-linux-<pid>/note.json`
//! before the app dies, in the shape `wd crashes` reads: what panicked and
//! where, its backtrace, and what was on screen.

use std::backtrace::Backtrace;
use std::io;
use std::panic::PanicHookInfo;
use std::path::{Path, PathBuf};

use chrono::{DateTime, SecondsFormat, Utc};
use serde_json::json;

/// What the app shows: the work open, if any, and which view of it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Screen {
    pub work: Option<String>,
    pub view: String,
}

/// `$WD_HOME`, as wd reads it; None without it or a home directory.
pub fn wd_home() -> Option<PathBuf> {
    match std::env::var_os("WD_HOME") {
        Some(home) if !home.is_empty() => Some(PathBuf::from(home)),
        _ => std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".work-director")),
    }
}

/// Makes every panic write its note under home first, then report as it
/// would have. screen says what is on screen as the panic happens.
pub fn keep_panics(home: PathBuf, build: String, screen: impl Fn() -> Screen + Send + Sync + 'static) {
    let previous = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        match keep(&home, Utc::now(), std::process::id(), &build, &what(info), &Backtrace::force_capture(), &screen()) {
            Ok(dir) => {
                eprintln!("the app crashed: wd crashes {}", dir.file_name().unwrap_or_default().to_string_lossy())
            }
            Err(e) => eprintln!("the note of this crash was not written: {e}"),
        }
        previous(info);
    }));
}

/// The panic's message and where it was raised.
fn what(info: &PanicHookInfo<'_>) -> String {
    let message = match (info.payload().downcast_ref::<&str>(), info.payload().downcast_ref::<String>()) {
        (Some(s), _) => s.to_string(),
        (_, Some(s)) => s.clone(),
        _ => "a panic without a message".to_string(),
    };
    match info.location() {
        Some(at) => format!("{message} at {}:{}:{}", at.file(), at.line(), at.column()),
        None => message,
    }
}

/// Writes the note in a folder of its own, named for when and which process.
pub fn keep(
    home: &Path,
    at: DateTime<Utc>,
    pid: u32,
    build: &str,
    reason: &str,
    stack: &Backtrace,
    screen: &Screen,
) -> io::Result<PathBuf> {
    let dir = home.join("crashes").join(format!("{}-linux-{pid}", at.format("%Y%m%d-%H%M%S")));
    std::fs::create_dir_all(&dir)?;
    let note = json!({
        "app": "linux",
        "at": at.to_rfc3339_opts(SecondsFormat::Secs, true),
        "build": build,
        "pid": pid,
        "exception": {"name": "panic", "reason": reason, "stack": stack.to_string().lines().collect::<Vec<_>>()},
        "screen": {"work": screen.work, "view": screen.view},
        "crumbs": [],
    });
    std::fs::write(dir.join("note.json"), serde_json::to_vec_pretty(&note)?)?;
    Ok(dir)
}
