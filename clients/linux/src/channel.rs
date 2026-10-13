//! The channel holds `wd serve --stdio` open: one JSON request per line in,
//! one reply or pushed event per line out.

use std::collections::HashMap;
use std::fmt;
use std::io::{BufRead, BufReader, Write};
use std::path::Path;
use std::process::{ChildStdin, Command, ExitStatus, Stdio};
use std::sync::{Arc, Mutex, MutexGuard};

use futures::channel::{mpsc, oneshot};
use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};

use crate::conversation::ConversationPage;
use crate::ledger::{ActionResult, Board, Event, Gate, GoalDetail, Work};
use crate::record::RecordView;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ChannelError {
    /// wd exited while the request was outstanding, or before it was sent.
    Exited(ExitStatus),
    /// wd answered the request with an error.
    Refused { kind: String, message: String },
    /// A line from wd was not the reply its request expected.
    Unreadable(String),
    /// wd could not be started or written to.
    Io(String),
    /// The channel was closed by the app.
    Closed,
}

impl fmt::Display for ChannelError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ChannelError::Exited(status) => write!(f, "wd exited ({status})"),
            ChannelError::Refused { kind, message } => write!(f, "{kind}: {message}"),
            ChannelError::Unreadable(detail) => write!(f, "unreadable reply from wd: {detail}"),
            ChannelError::Io(detail) => write!(f, "cannot talk to wd: {detail}"),
            ChannelError::Closed => write!(f, "the channel to wd is closed"),
        }
    }
}

impl std::error::Error for ChannelError {}

type Reply = Result<String, ChannelError>;

struct Waiting {
    next: i64,
    replies: HashMap<i64, oneshot::Sender<Reply>>,
    exit: Option<ExitStatus>,
}

struct Inner {
    input: Mutex<Option<ChildStdin>>,
    waiting: Mutex<Waiting>,
    events: Mutex<Option<mpsc::UnboundedReceiver<Event>>>,
}

/// A handle on one running `wd serve --stdio`; clones share it.
#[derive(Clone)]
pub struct Channel {
    inner: Arc<Inner>,
}

#[derive(Serialize)]
struct Request<'a> {
    id: i64,
    method: &'a str,
    params: Params<'a>,
}

#[derive(Serialize, Default)]
struct Params<'a> {
    #[serde(skip_serializing_if = "Option::is_none")]
    id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    argv: Option<&'a [String]>,
    #[serde(skip_serializing_if = "Option::is_none")]
    from: Option<usize>,
    #[serde(skip_serializing_if = "Option::is_none")]
    agent: Option<&'a str>,
}

#[derive(Deserialize)]
struct Answer<T> {
    result: Option<T>,
    error: Option<Refusal>,
}

#[derive(Deserialize)]
struct Refusal {
    kind: String,
    message: String,
}

#[derive(Deserialize)]
struct Head {
    id: Option<i64>,
    push: Option<String>,
}

#[derive(Deserialize)]
struct Push {
    data: Event,
}

fn lock<T>(m: &Mutex<T>) -> MutexGuard<'_, T> {
    // A panic while holding one of these locks leaves nothing half-written
    // that matters: each critical section is a single insert, remove or write.
    m.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

impl Channel {
    /// Starts `wd serve --stdio`. `environment` replaces the inherited one when given.
    pub fn spawn(wd: &Path, environment: Option<&HashMap<String, String>>) -> Result<Channel, ChannelError> {
        let mut command = Command::new(wd);
        command.args(["serve", "--stdio"]).stdin(Stdio::piped()).stdout(Stdio::piped()).stderr(Stdio::inherit());
        if let Some(env) = environment {
            command.env_clear().envs(env);
        }
        let mut child = command.spawn().map_err(|e| ChannelError::Io(format!("{}: {e}", wd.display())))?;
        let stdin = child.stdin.take().ok_or_else(|| ChannelError::Io("wd has no stdin".into()))?;
        let stdout = child.stdout.take().ok_or_else(|| ChannelError::Io("wd has no stdout".into()))?;
        let (sink, events) = mpsc::unbounded();
        let inner = Arc::new(Inner {
            input: Mutex::new(Some(stdin)),
            waiting: Mutex::new(Waiting { next: 1, replies: HashMap::new(), exit: None }),
            events: Mutex::new(Some(events)),
        });
        let reader = Arc::clone(&inner);
        std::thread::Builder::new()
            .name("wd-stdio".into())
            .spawn(move || {
                // Lines are handled in the order wd wrote them.
                for line in BufReader::new(stdout).lines() {
                    match line {
                        Ok(line) => reader.receive(line, &sink),
                        Err(e) => {
                            reader.fail_all(ChannelError::Unreadable(e.to_string()));
                            break;
                        }
                    }
                }
                let status = child.wait();
                sink.close_channel();
                match status {
                    Ok(status) => {
                        lock(&reader.waiting).exit = Some(status);
                        reader.fail_all(ChannelError::Exited(status));
                    }
                    Err(e) => reader.fail_all(ChannelError::Io(e.to_string())),
                }
            })
            .map_err(|e| ChannelError::Io(e.to_string()))?;
        Ok(Channel { inner })
    }

    /// Every ledger event any wd process records, in id order, while wd runs.
    /// There is one stream; the first to ask takes it.
    pub fn events(&self) -> Option<mpsc::UnboundedReceiver<Event>> {
        lock(&self.inner.events).take()
    }

    /// Ends wd by closing its input; it answers what it holds and exits.
    pub fn close(&self) {
        lock(&self.inner.input).take();
    }

    pub async fn board(&self) -> Result<Board, ChannelError> {
        self.request("board", Params::default()).await
    }

    pub async fn goal(&self, id: &str) -> Result<GoalDetail, ChannelError> {
        self.request("goal", Params { id: Some(id), ..Params::default() }).await
    }

    /// What was decided and done at every depth under a goal.
    pub async fn record(&self, id: &str) -> Result<RecordView, ChannelError> {
        self.request("record", Params { id: Some(id), ..Params::default() }).await
    }

    pub async fn work(&self, id: &str) -> Result<Work, ChannelError> {
        self.request("work", Params { id: Some(id), ..Params::default() }).await
    }

    pub async fn events_of(&self, id: &str) -> Result<Vec<Event>, ChannelError> {
        self.request("events", Params { id: Some(id), ..Params::default() }).await
    }

    pub async fn gate(&self, id: &str) -> Result<Gate, ChannelError> {
        self.request("gate", Params { id: Some(id), ..Params::default() }).await
    }

    /// The work's session steps from index `from` on, or a subagent's.
    pub async fn conversation(
        &self,
        id: &str,
        from: usize,
        agent: Option<&str>,
    ) -> Result<ConversationPage, ChannelError> {
        self.request("conversation", Params { id: Some(id), from: Some(from), agent, ..Params::default() }).await
    }

    /// Runs `wd` with argv, exactly as given.
    pub async fn action(&self, argv: &[String]) -> Result<ActionResult, ChannelError> {
        self.request("action", Params { argv: Some(argv), ..Params::default() }).await
    }

    async fn request<T: DeserializeOwned>(&self, method: &str, params: Params<'_>) -> Result<T, ChannelError> {
        let reply = self.send(method, params)?;
        let line = reply.await.map_err(|_| ChannelError::Unreadable(format!("{method}: the reply was lost")))??;
        let answer: Answer<T> =
            serde_json::from_str(&line).map_err(|e| ChannelError::Unreadable(format!("{method}: {e}")))?;
        if let Some(refusal) = answer.error {
            return Err(ChannelError::Refused { kind: refusal.kind, message: refusal.message });
        }
        answer
            .result
            .ok_or_else(|| ChannelError::Unreadable(format!("{method}: a reply with neither result nor error")))
    }

    fn send(&self, method: &str, params: Params<'_>) -> Result<oneshot::Receiver<Reply>, ChannelError> {
        let (tx, rx) = oneshot::channel();
        let id = {
            let mut waiting = lock(&self.inner.waiting);
            if let Some(status) = waiting.exit {
                return Err(ChannelError::Exited(status));
            }
            let id = waiting.next;
            waiting.next += 1;
            waiting.replies.insert(id, tx);
            id
        };
        let mut line = serde_json::to_vec(&Request { id, method, params })
            .map_err(|e| ChannelError::Unreadable(format!("{method}: {e}")))?;
        line.push(b'\n');
        let written = match lock(&self.inner.input).as_mut() {
            Some(input) => {
                input.write_all(&line).and_then(|_| input.flush()).map_err(|e| ChannelError::Io(e.to_string()))
            }
            None => Err(ChannelError::Closed),
        };
        if let Err(e) = written {
            lock(&self.inner.waiting).replies.remove(&id);
            return Err(e);
        }
        Ok(rx)
    }
}

impl Inner {
    fn receive(&self, line: String, sink: &mpsc::UnboundedSender<Event>) {
        let head: Head = match serde_json::from_str(&line) {
            Ok(head) => head,
            Err(_) => {
                // A line that is not JSON at all matches no request, so every
                // waiting request learns the channel is broken.
                self.fail_all(ChannelError::Unreadable(line));
                return;
            }
        };
        if let Some(id) = head.id {
            let waiter = lock(&self.waiting).replies.remove(&id);
            if let Some(waiter) = waiter {
                // A request whose caller stopped waiting drops its receiver; the reply has no reader.
                let _ = waiter.send(Ok(line));
            }
        } else if head.push.as_deref() == Some("event") {
            match serde_json::from_str::<Push>(&line) {
                // The receiver is gone once nobody follows events; they are not needed then.
                Ok(push) => {
                    let _ = sink.unbounded_send(push.data);
                }
                Err(e) => self.fail_all(ChannelError::Unreadable(format!("pushed event: {e}"))),
            }
        }
    }

    fn fail_all(&self, error: ChannelError) {
        let pending: Vec<_> = lock(&self.waiting).replies.drain().map(|(_, w)| w).collect();
        for waiter in pending {
            let _ = waiter.send(Err(error.clone()));
        }
    }
}
