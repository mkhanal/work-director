//! What the app remembers between launches about how it is used, not about
//! the work: the product in scope, the last page open, the send key. One
//! `key=value` line each in the user's config directory.

use std::cell::RefCell;
use std::collections::BTreeMap;
use std::io;
use std::path::PathBuf;

use gtk::glib;
use kit::SendKey;

pub struct Settings {
    path: PathBuf,
    values: RefCell<BTreeMap<String, String>>,
}

impl Settings {
    /// The settings, and why they could not be read when a file was there.
    /// A first launch has no file and starts from the defaults.
    pub fn load() -> (Settings, Option<String>) {
        let path = glib::user_config_dir().join("work-director").join("settings");
        let (text, problem) = match std::fs::read_to_string(&path) {
            Ok(text) => (text, None),
            Err(e) if e.kind() == io::ErrorKind::NotFound => (String::new(), None),
            Err(e) => (String::new(), Some(format!("cannot read {}: {e}", path.display()))),
        };
        let values = text
            .lines()
            .filter_map(|line| line.split_once('='))
            .map(|(k, v)| (k.trim().to_string(), v.trim().to_string()))
            .collect();
        (Settings { path, values: RefCell::new(values) }, problem)
    }

    fn get(&self, key: &str) -> Option<String> {
        self.values.borrow().get(key).filter(|v| !v.is_empty()).cloned()
    }

    fn set(&self, key: &str, value: &str) -> io::Result<()> {
        if self.values.borrow().get(key).map(String::as_str) == Some(value) {
            return Ok(());
        }
        self.values.borrow_mut().insert(key.to_string(), value.to_string());
        let text: String = self.values.borrow().iter().map(|(k, v)| format!("{k}={v}\n")).collect();
        if let Some(dir) = self.path.parent() {
            std::fs::create_dir_all(dir)?;
        }
        std::fs::write(&self.path, text)
    }

    /// The product everything is scoped to; none is all products.
    pub fn scope(&self) -> Option<String> {
        self.get("scope")
    }

    pub fn set_scope(&self, scope: Option<&str>) -> io::Result<()> {
        self.set("scope", scope.unwrap_or(""))
    }

    /// The work last open, so the app opens where it was left.
    pub fn last_work(&self) -> Option<String> {
        self.get("last-work")
    }

    pub fn set_last_work(&self, id: Option<&str>) -> io::Result<()> {
        self.set("last-work", id.unwrap_or(""))
    }

    pub fn send_key(&self) -> SendKey {
        self.get("send-key").and_then(|k| SendKey::named(&k)).unwrap_or(SendKey::STANDARD)
    }

    pub fn set_send_key(&self, key: SendKey) -> io::Result<()> {
        self.set("send-key", key.name())
    }

    /// Whether the agents panel shows beside a work page.
    pub fn agents_shown(&self) -> bool {
        self.get("agents").is_none_or(|v| v == "shown")
    }

    pub fn set_agents_shown(&self, shown: bool) -> io::Result<()> {
        self.set("agents", if shown { "shown" } else { "hidden" })
    }
}
