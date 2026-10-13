//! The PATH every wd the app starts runs with. An app opened from a desktop
//! launcher inherits the session's bare PATH, where a runner CLI installed
//! under the home directory is not found; the person's login shell knows
//! where theirs live.

use std::collections::{HashMap, HashSet};
use std::io::Read;
use std::os::unix::fs::MetadataExt;
use std::path::Path;
use std::process::{Command, Stdio};
use std::sync::mpsc;
use std::time::Duration;

const MARKER: &str = "__WD_PATH__";

/// What the login shell runs, interactive so it reads the rc file where PATH is often set: its PATH between markers, so rc noise around it is ignored.
pub const SCRIPT: &str = r#"printf "%s" "__WD_PATH__${PATH}__WD_PATH__""#;

/// Where runner CLIs usually live, tried in order when the login shell does not answer.
const USUAL: [&str; 7] = [
    "~/.local/bin",
    "~/.opencode/bin",
    "~/.cargo/bin",
    "~/bin",
    "/opt/homebrew/bin",
    "/usr/local/bin",
    "~/.npm-global/bin",
];

/// The text between the first two markers, or None when they are missing or enclose nothing.
pub fn parse(output: &str) -> Option<String> {
    let mut parts = output.split(MARKER);
    parts.next()?;
    let path = parts.next()?;
    parts.next()?;
    (!path.is_empty()).then(|| path.to_string())
}

/// `first`'s entries, then `then`'s that `first` lacks; empty entries dropped.
pub fn merge(first: &str, then: &str) -> String {
    let mut seen = HashSet::new();
    first.split(':').chain(then.split(':')).filter(|e| !e.is_empty() && seen.insert(*e)).collect::<Vec<_>>().join(":")
}

/// The inherited PATH plus each usual directory that exists.
pub fn fallback(inherited: &str, home: &str, exists: impl Fn(&str) -> bool) -> String {
    let found: Vec<String> = USUAL
        .iter()
        .map(|dir| match dir.strip_prefix('~') {
            Some(rest) => format!("{home}{rest}"),
            None => dir.to_string(),
        })
        .filter(|dir| exists(dir))
        .collect();
    merge(inherited, &found.join(":"))
}

/// The PATH wd runs with: the login shell's, or the fallback, then the inherited one.
pub fn path(login: Option<&str>, inherited: &str, home: &str, exists: impl Fn(&str) -> bool) -> String {
    match login {
        Some(login) => merge(login, inherited),
        None => merge(&fallback(inherited, home, exists), inherited),
    }
}

/// `$SHELL`, else the account's shell, else bash.
pub fn shell(environment: &HashMap<String, String>, account: Option<&str>) -> String {
    [environment.get("SHELL").map(String::as_str), account]
        .into_iter()
        .flatten()
        .find(|s| !s.is_empty())
        .unwrap_or("/bin/bash")
        .to_string()
}

/// `environment` with only PATH replaced.
pub fn with_path(environment: &HashMap<String, String>, path: String) -> HashMap<String, String> {
    let mut env = environment.clone();
    env.insert("PATH".into(), path);
    env
}

/// Asks the login shell for its PATH and returns the environment every wd gets.
/// Blocks for at most `timeout`; run it off the main thread.
pub fn resolve(environment: &HashMap<String, String>, timeout: Duration) -> HashMap<String, String> {
    let login = ask(&shell(environment, account_shell().as_deref()), timeout);
    let home = environment.get("HOME").cloned().unwrap_or_default();
    let inherited = environment.get("PATH").cloned().unwrap_or_default();
    let path = path(login.as_deref(), &inherited, &home, |dir| Path::new(dir).is_dir());
    with_path(environment, path)
}

/// The shell /etc/passwd names for this process's user.
fn account_shell() -> Option<String> {
    let uid = std::fs::metadata("/proc/self").ok()?.uid().to_string();
    let passwd = std::fs::read_to_string("/etc/passwd").ok()?;
    passwd
        .lines()
        .map(|l| l.split(':').collect::<Vec<_>>())
        .find(|f| f.len() >= 7 && f[2] == uid)
        .map(|f| f[6].to_string())
}

/// The login shell's PATH, or None when it fails or does not answer in time.
fn ask(shell: &str, timeout: Duration) -> Option<String> {
    let mut child = Command::new(shell)
        .args(["-l", "-i", "-c", SCRIPT])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .ok()?;
    let mut stdout = child.stdout.take()?;
    let (tx, rx) = mpsc::channel();
    // Done once both markers are in or the pipe closes: a child the rc file
    // leaves running may hold the pipe open long after the shell exits.
    std::thread::spawn(move || {
        let mut seen = Vec::new();
        let mut chunk = [0u8; 4096];
        while let Ok(n) = stdout.read(&mut chunk) {
            seen.extend_from_slice(&chunk[..n]);
            let found = parse(&String::from_utf8_lossy(&seen));
            if n == 0 || found.is_some() {
                // The receiver is gone once the wait timed out; nobody needs the answer then.
                let _ = tx.send(found);
                return;
            }
        }
        let _ = tx.send(None);
    });
    let answer = rx.recv_timeout(timeout).ok().flatten();
    // A shell that has not exited by now is not needed; it may already be gone.
    let _ = child.kill();
    let _ = child.wait();
    answer
}
