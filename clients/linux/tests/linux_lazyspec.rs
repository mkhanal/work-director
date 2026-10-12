//! The requirements in `linux.lazyspec.md`, one test each, run against a wd
//! built from this checkout so the app's model reads the wire the Go side
//! writes today.

use std::cell::RefCell;
use std::collections::{HashMap, HashSet};
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::rc::Rc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::OnceLock;
use std::time::{Duration, Instant};

use futures::executor::{block_on, LocalPool};
use futures::task::LocalSpawnExt;
use serde_json::{json, Value};

use kit::*;

/// A wd built from this checkout, once per test run.
fn built_wd() -> &'static Path {
    static BUILT: OnceLock<PathBuf> = OnceLock::new();
    BUILT.get_or_init(|| {
        let repo = Path::new(env!("CARGO_MANIFEST_DIR")).join("../..");
        let out = std::env::temp_dir().join(format!("wd-linux-tests-{}", std::process::id())).join("wd");
        let status = Command::new("go")
            .args(["build", "-o"])
            .arg(&out)
            .arg("./cmd/wd")
            .current_dir(&repo)
            .status()
            .expect("go is on PATH");
        assert!(status.success(), "go build ./cmd/wd failed in {}", repo.display());
        out
    })
}

fn temp_dir() -> PathBuf {
    static N: AtomicU64 = AtomicU64::new(0);
    let nanos = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_nanos()).unwrap_or(0);
    let dir = std::env::temp_dir().join(format!(
        "wd-linux-{}-{}-{nanos}",
        std::process::id(),
        N.fetch_add(1, Ordering::SeqCst)
    ));
    fs::create_dir_all(&dir).expect("temp dir");
    dir
}

fn executable(path: &Path, script: &str) {
    fs::create_dir_all(path.parent().expect("a parent")).expect("dir");
    fs::write(path, script).expect("write script");
    fs::set_permissions(path, fs::Permissions::from_mode(0o755)).expect("chmod");
}

/// A ledger of its own, with one registered project named demo.
struct Home {
    dir: PathBuf,
    env: HashMap<String, String>,
}

impl Home {
    fn new() -> Home {
        let dir = temp_dir();
        let project = dir.join("demo-repo");
        fs::create_dir_all(&project).expect("project dir");
        let mut env: HashMap<String, String> = std::env::vars().collect();
        env.insert("WD_HOME".into(), dir.display().to_string());
        env.insert("WD_FAKE_STATE".into(), dir.join("fake-state").display().to_string());
        let path = env.get("PATH").cloned().unwrap_or_default();
        env.insert("PATH".into(), format!("{}:{path}", dir.join("bin").display()));
        let home = Home { dir, env };
        home.wd(&["projects", "add", "demo", &project.display().to_string(), "--runner", "claude", "--lazyspec", "n"]);
        home
    }

    /// Runs wd as another process would, returning its trimmed stdout.
    fn wd(&self, argv: &[&str]) -> String {
        let out = Command::new(built_wd()).args(argv).env_clear().envs(&self.env).output().expect("run wd");
        let stdout = String::from_utf8_lossy(&out.stdout).trim().to_string();
        assert!(out.status.success(), "wd {}: {stdout} {}", argv.join(" "), String::from_utf8_lossy(&out.stderr));
        stdout
    }

    /// Files a goal and returns its id.
    fn goal(&self, project: &str, title: &str) -> String {
        let work: Work = serde_json::from_str(&self.wd(&["goal", "add", project, title, "--json"])).expect("a goal");
        work.id
    }

    /// The environment with HOME in the ledger's directory, where a session's
    /// store is looked for.
    fn with_home(&self) -> HashMap<String, String> {
        let mut env = self.env.clone();
        env.insert("HOME".into(), self.dir.display().to_string());
        env
    }

    fn channel(&self) -> Channel {
        Channel::spawn(built_wd(), Some(&self.env)).expect("wd serve --stdio")
    }
}

/// One thread's executor, as GTK's main loop is for the app.
struct Run {
    pool: RefCell<LocalPool>,
}

impl Run {
    fn new() -> Run {
        Run { pool: RefCell::new(LocalPool::new()) }
    }

    fn spawner(&self) -> Spawner {
        let spawner = self.pool.borrow().spawner();
        Rc::new(move |f: futures::future::LocalBoxFuture<'static, ()>| {
            spawner.spawn_local(f).expect("spawn on the test's pool")
        })
    }

    fn store(&self, channel: Channel) -> Rc<Store> {
        Store::new(channel, self.spawner())
    }

    fn wait<T>(&self, f: impl std::future::Future<Output = T>) -> T {
        self.pool.borrow_mut().run_until(f)
    }

    /// Runs what is ready until `done` holds or `within` passes.
    fn until(&self, within: Duration, done: impl Fn() -> bool) -> bool {
        let deadline = Instant::now() + within;
        while Instant::now() < deadline {
            self.pool.borrow_mut().run_until_stalled();
            if done() {
                return true;
            }
            std::thread::sleep(Duration::from_millis(20));
        }
        false
    }
}

fn work_json(id: &str, state: &str) -> Value {
    json!({"id": id, "project": "demo", "title": "T", "detail": "", "kind": "task", "state": state,
        "runner": null, "session": null, "ref": null, "cwd": null, "created": "c", "updated": "u",
        "parent": null, "heading": null, "claim": null, "impact": null, "goal_type": null, "archived": null})
}

fn work(id: &str, state: &str) -> Work {
    serde_json::from_value(work_json(id, state)).expect("a work")
}

fn strings(words: &[&str]) -> Vec<String> {
    words.iter().map(|s| s.to_string()).collect()
}

/// A runner that matches requests to goals and plans goals the way a model
/// would, with no model: it answers a find brief with the goal mentioning CSV,
/// and any other brief with a two-task plan.
fn fake_runner(home: &Home) {
    executable(
        &home.dir.join("bin/fake"),
        r#"#!/bin/sh
state="$WD_FAKE_STATE"; mkdir -p "$state"
case "$1" in
  run)
    n=$(( $(cat "$state/n" 2>/dev/null || echo 0) + 1 )); echo "$n" > "$state/n"
    printf '%s' "$2" > "$state/brief-$n"; echo "session=s$n";;
  export)
    brief="$state/brief-${2#s}"
    if grep -q '^You match a request' "$brief"; then
      hit=""
      grep -q 'CSV export' "$brief" && hit=$(grep -m1 '^- .*CSV' "$brief" | sed 's/^- \([0-9a-f]*\) .*/\1/')
      if [ -n "$hit" ]; then echo "MATCH: $hit | extends the CSV export"; else echo NONE; fi
    else
      printf '## Work\n- [ ] first step\n- [ ] second step\n'
    fi;;
  status) echo 'state: exited';;
  *) echo ok;;
esac
"#,
    );
    fs::create_dir_all(home.dir.join("runners")).expect("runners dir");
    fs::write(
        home.dir.join("runners/fake.toml"),
        r#"name = "fake"
spawn = "fake run {brief}"
session_id = 'session=([A-Za-z0-9-]+)'
send = "fake send {session} {text}"
status = "fake status {session}"
running = 'state: *running'
waiting = 'state: *waiting'
exited = 'state: *exited'
transcript = "fake export {session}"
models = "fake models"
attach = "fake attach {session}"
stop = "fake stop {session}"
"#,
    )
    .expect("fake runner");
}

/// The App Runs The wd Installed Beside It
#[test]
fn the_app_runs_the_wd_installed_beside_it() {
    let dir = temp_dir();
    let app = dir.join("work-director");
    let elsewhere = std::ffi::OsStr::new("/elsewhere/wd");

    assert_eq!(locate_wd(&app, Some(elsewhere)), Ok(PathBuf::from("/elsewhere/wd")), "nothing beside it: WD_BIN");

    fs::write(dir.join("wd"), "#!/bin/sh\n").expect("write");
    assert_eq!(locate_wd(&app, Some(elsewhere)), Ok(PathBuf::from("/elsewhere/wd")), "a wd that cannot run is not run");

    executable(&dir.join("wd"), "#!/bin/sh\n");
    assert_eq!(locate_wd(&app, Some(elsewhere)), Ok(dir.join("wd")), "the wd beside it comes first");

    let bare = temp_dir();
    let on_path = temp_dir();
    executable(&on_path.join("wd"), "#!/bin/sh\n");
    let refused = locate_wd(&bare.join("work-director"), None).expect_err("PATH is never searched");
    let said = refused.to_string();
    assert!(said.contains(&bare.join("wd").display().to_string()) && said.contains("WD_BIN"), "{said}");
    assert_eq!(locate_wd(&bare.join("work-director"), Some(std::ffi::OsStr::new(""))), Err(refused));
}

/// A Reply Reaches The Request That Carries Its Id
#[test]
fn a_reply_reaches_the_request_that_carries_its_id() {
    // Answers the first two requests in reverse, refuses the third, and exits
    // while the fourth is outstanding.
    let fake = temp_dir().join("wd");
    executable(
        &fake,
        r#"#!/bin/sh
id() { echo "$1" | sed 's/.*"id":\([0-9][0-9]*\).*/\1/'; }
arg() { echo "$1" | sed 's/.*"argv":\["\([^"]*\)".*/\1/'; }
read -r a; read -r b
echo "{\"id\":$(id "$b"),\"result\":{\"code\":0,\"stdout\":\"$(arg "$b")\",\"stderr\":\"\"}}"
echo "{\"id\":$(id "$a"),\"result\":{\"code\":0,\"stdout\":\"$(arg "$a")\",\"stderr\":\"\"}}"
read -r c
echo "{\"id\":$(id "$c"),\"error\":{\"kind\":\"not-found\",\"message\":\"no work x\"}}"
read -r d
exit 3
"#,
    );
    let channel = Channel::spawn(&fake, None).expect("spawn the fake");
    let (one, two) = (strings(&["one"]), strings(&["two"]));
    let (first, second) = block_on(async { futures::join!(channel.action(&one), channel.action(&two)) });
    assert_eq!(first.expect("first").stdout, "one");
    assert_eq!(second.expect("second").stdout, "two");
    assert_eq!(
        block_on(channel.work("x")),
        Err(ChannelError::Refused { kind: "not-found".into(), message: "no work x".into() })
    );
    match block_on(channel.board()) {
        Err(ChannelError::Exited(status)) => assert_eq!(status.code(), Some(3)),
        other => panic!("want the exit named, got {other:?}"),
    }
}

/// A Write Runs At Once And Shows The Command It Ran
#[test]
fn a_write_runs_at_once_and_shows_the_command_it_ran() {
    let home = Home::new();
    let id = home.wd(&["add", "demo", "Pick a database"]);
    let plan = Plan::decide(&id, "use sqlite, it's what we know").expect("a decision");
    assert_eq!(plan.command_line(), format!("wd decide {id} \"use sqlite, it's what we know\""));
    let mut typed = vec!["wd".to_string()];
    typed.extend(plan.argv.iter().cloned());
    assert_eq!(kit::plan::split(&plan.command_line()), Ok(typed));

    let run = Run::new();
    let store = run.store(home.channel());
    assert_eq!(run.wait(store.run(&plan)).expect("ran").code, 0);
    let decisions: Vec<String> = run
        .wait(store.events_of(&id))
        .expect("events")
        .into_iter()
        .filter(|e| e.kind == "decision")
        .map(|e| e.body)
        .collect();
    assert_eq!(decisions, vec!["use sqlite, it's what we know"]);

    assert_eq!(plan.undo(), None);
    assert_eq!(Plan::cancel(&id, "").undo().map(|p| p.argv), Some(strings(&["resume", &id])));
    assert_eq!(Plan::pause(&id).undo().map(|p| p.argv), Some(strings(&["resume", &id])));
    assert_eq!(Plan::archive(&work(&id, "done"), "").undo().map(|p| p.argv), Some(strings(&["unarchive", &id])));
    assert_eq!(run.wait(store.run(&Plan::cancel(&id, ""))).expect("cancelled").code, 0);
    let undo = Plan::cancel(&id, "").undo().expect("an undo");
    assert_eq!(run.wait(store.run(&undo)).expect("undone").code, 0);
    assert_ne!(run.wait(store.work(&id)).expect("work").state, WorkState::Dropped);
}

/// Text In The Command Bar Becomes Argv Without A Shell
#[test]
fn text_in_the_command_bar_becomes_argv_without_a_shell() {
    assert_eq!(
        Plan::typed(r#"wd decide abc "two words" 'it said "hi"' $HOME ~ *"#).map(|p| p.argv),
        Ok(strings(&["decide", "abc", "two words", r#"it said "hi""#, "$HOME", "~", "*"]))
    );
    assert_eq!(Plan::typed("  status   --json ").map(|p| p.argv), Ok(strings(&["status", "--json"])));
    assert_eq!(Plan::typed(r#"decide abc "open"#), Err(PlanError::UnclosedQuote));
    assert_eq!(Plan::typed("wd"), Err(PlanError::Empty));
}

/// Only Reopening Asks For Words
#[test]
fn only_reopening_asks_for_words() {
    let open = work("w1", "running");
    let rest = work("w1", "done");
    for blank in ["", "  ", "\n\t"] {
        assert_eq!(Plan::reopen("g1", blank), Err(PlanError::ReasonRequired("reopening")));
        assert_eq!(Plan::cancel("g1", blank).argv, strings(&["cancel", "g1"]));
        assert_eq!(Plan::release("g1", blank).argv, strings(&["release", "g1"]));
        assert_eq!(Plan::archive(&open, blank).argv, strings(&["archive", "w1"]));
    }
    assert_eq!(Plan::archive(&rest, "").argv, strings(&["archive", "w1"]));
    assert_eq!(Plan::archive(&open, " not wanted ").argv, strings(&["archive", "w1", "not wanted"]));
    assert_eq!(Plan::cancel("g1", "client said stop").argv, strings(&["cancel", "g1", "client said stop"]));
    assert_eq!(Plan::reopen("g1", " add search ").map(|p| p.argv), Ok(strings(&["reopen", "g1", "add search"])));
    assert_eq!(Plan::release("g1", "duplicate of g2").argv, strings(&["release", "g1", "duplicate of g2"]));
}

/// Needs-Input And Blocked Never Look The Same
#[test]
fn needs_input_and_blocked_never_look_the_same() {
    let looks: Vec<StateLook> = WorkState::ALL.iter().map(|s| s.look()).collect();
    assert_eq!(looks.iter().map(|l| l.label).collect::<HashSet<_>>().len(), WorkState::ALL.len());
    assert_eq!(looks.iter().map(|l| l.icon).collect::<HashSet<_>>().len(), WorkState::ALL.len());
    assert_ne!(WorkState::NeedsInput.look().tint, WorkState::Blocked.look().tint);
}

/// A Change Made Elsewhere Reaches The Board
#[test]
fn a_change_made_elsewhere_reaches_the_board() {
    let home = Home::new();
    let run = Run::new();
    let store = run.store(home.channel());
    run.wait(store.start());
    assert_eq!(store.board().as_ref().map(|b| b.standalone.is_empty()), Some(true));

    let id = home.wd(&["add", "demo", "Filed from a terminal"]);
    let seen = run.until(Duration::from_secs(2), || {
        store.board().as_ref().is_some_and(|b| b.items(Band::InFlight, None).iter().any(|w| w.id == id))
    });
    assert!(seen, "work filed by another wd process is not on the board within 2s");
    store.stop();
}

/// A Product Scope Shows Only That Product's Work
#[test]
fn a_product_scope_shows_only_that_products_work() {
    let home = Home::new();
    let other = home.dir.join("other-repo");
    fs::create_dir_all(&other).expect("other repo");
    home.wd(&["projects", "add", "other", &other.display().to_string(), "--runner", "claude", "--lazyspec", "n"]);
    let mine = home.goal("demo", "Mine");
    let theirs = home.goal("other", "Theirs");
    let task = home.wd(&["add", "other", "Their task"]);
    let run = Run::new();
    let store = run.store(home.channel());
    run.wait(store.start());
    let board = store.board().clone().expect("a board");
    let ids = |works: Vec<&Work>| works.into_iter().map(|w| w.id.clone()).collect::<Vec<_>>();
    assert_eq!(ids(board.items(Band::InFlight, Some("demo"))), vec![mine]);
    assert_eq!(
        ids(board.items(Band::InFlight, Some("other"))).into_iter().collect::<HashSet<_>>(),
        HashSet::from([theirs.clone(), task])
    );
    assert_eq!(board.items(Band::InFlight, None).len(), 3);
    assert_eq!(ids(board.recent_goals(Some("other"))), vec![theirs]);
    assert_eq!(board.recent_goals(None).len(), 2);
    store.stop();
}

/// A New Goal First Offers The Goals It May Continue
#[test]
fn a_new_goal_first_offers_the_goals_it_may_continue() {
    let home = Home::new();
    fake_runner(&home);
    let repo = home.dir.join("fake-repo");
    fs::create_dir_all(&repo).expect("repo");
    for args in [
        vec!["init", "-q", "-b", "main"],
        vec!["-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"],
    ] {
        assert!(Command::new("git").args(&args).current_dir(&repo).status().expect("git").success());
    }
    home.wd(&["projects", "add", "tracker", &repo.display().to_string(), "--runner", "fake", "--lazyspec", "n"]);
    let csv = home.goal("tracker", "Export tasks to CSV");
    home.wd(&["done", &csv, "--cancelled", "the first version shipped"]);
    let run = Run::new();
    let store = run.store(home.channel());
    run.wait(store.start());
    let flow = NewGoal::new(Rc::clone(&store));

    run.wait(flow.submit("tracker", "add tags to the CSV export"));
    match flow.phase() {
        NewGoalPhase::Offering(found) => {
            assert_eq!(found.iter().map(|f| f.goal.id.clone()).collect::<Vec<_>>(), vec![csv.clone()])
        }
        other => panic!("phase = {other:?}, want the CSV goal offered"),
    }
    assert!(home.wd(&["status", "--all"]).contains(&format!("{csv}\tdone")), "nothing started while offering");

    run.wait(flow.continue_goal(&csv, "add tags to the CSV export"));
    match flow.phase() {
        NewGoalPhase::Started(continued) => assert!(continued.goal.id == csv && continued.tasks.len() == 2),
        other => panic!("phase = {other:?}, want the CSV goal continued"),
    }

    flow.reset();
    run.wait(flow.submit("tracker", "A dark mode for the settings page"));
    let fresh = match flow.phase() {
        NewGoalPhase::Started(fresh) => fresh,
        other => panic!("phase = {other:?}, want a new goal started in one step"),
    };
    assert!(
        fresh.goal.title == "A dark mode for the settings page" && fresh.tasks.is_empty(),
        "the goal returns before it is planned"
    );
    let mut planned = 0;
    let deadline = Instant::now() + Duration::from_secs(20);
    while Instant::now() < deadline {
        planned = run.wait(store.goal(&fresh.goal.id)).expect("goal").tasks.len();
        if planned == 2 {
            break;
        }
        std::thread::sleep(Duration::from_millis(200));
    }
    assert_eq!(planned, 2, "the background planning gave the goal its tasks");
    store.stop();
}

/// A Session's Conversation Follows It, Reading Only What Can Still Change
#[test]
fn a_sessions_conversation_follows_it_reading_only_what_can_still_change() {
    let home = Home::new();
    // A claude that knows no live session: the store is read from its file.
    executable(&home.dir.join("bin/claude"), "#!/bin/sh\necho '[]'\n");
    let id = home.wd(&["add", "demo", "Talked to"]);
    let session = "c0ffee00-0000-4000-8000-000000000001";
    home.wd(&["attach", &id, session, "--runner", "claude"]);
    let file = home.dir.join(format!(".claude/projects/anywhere/{session}.jsonl"));
    fs::create_dir_all(file.parent().expect("parent")).expect("store dir");
    let lines = |records: &[&str]| records.iter().map(|r| format!("{r}\n")).collect::<String>();
    let opening = [
        r#"{"type":"user","message":{"content":"the brief"}}"#,
        r#"{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"Weighing it"},{"type":"text","text":"Looking"}]}}"#,
        r#"{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}]}}"#,
    ];
    fs::write(&file, lines(&opening)).expect("write store");

    let run = Run::new();
    let channel = Channel::spawn(built_wd(), Some(&home.with_home())).expect("wd");
    let store = run.store(channel);
    let feed = Feed::new(Rc::clone(&store), &id, None);
    run.wait(feed.read());
    assert_eq!(feed.state().failure, None);
    let kinds = |feed: &Feed| feed.state().steps.iter().map(|s| s.kind).collect::<Vec<_>>();
    assert_eq!(kinds(&feed), vec![StepKind::Prompt, StepKind::Thinking, StepKind::Text, StepKind::Tool]);
    assert_eq!(feed.stable(), 3, "the running tool call is where the next read starts");

    let mut later = opening.to_vec();
    later.push(r#"{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}"#);
    later.push(r#"{"type":"assistant","message":{"content":[{"type":"tool_use","id":"q1","name":"AskUserQuestion","input":{"questions":[{"header":"Colour","question":"Which colour?","multiSelect":false,"options":[{"label":"Red","description":""},{"label":"Blue","description":""}]}]}}]}}"#);
    fs::write(&file, lines(&later)).expect("write store");
    run.wait(feed.read());
    assert_eq!(
        kinds(&feed),
        vec![StepKind::Prompt, StepKind::Thinking, StepKind::Text, StepKind::Tool, StepKind::Question]
    );
    assert_eq!(feed.state().steps[3].tool.as_ref().and_then(|t| t.result.clone()), Some("ok".to_string()));
    assert_eq!(
        feed.state().asking.as_ref().and_then(|q| q.items.first()).map(|i| i.question.clone()),
        Some("Which colour?".to_string())
    );
    assert_eq!(feed.stable(), 4, "the question waiting for its answer is where the next read starts");
    store.stop();
}

/// A Conversation Keeps The Newest In View Until You Scroll Up To Read
#[test]
fn a_conversation_keeps_the_newest_in_view_until_you_scroll_up_to_read() {
    assert!(follow::at_bottom(0.0, 600.0, 400.0), "nothing to scroll");
    assert!(follow::at_bottom(1400.0, 600.0, 2000.0), "at the very bottom");
    assert!(follow::at_bottom(1360.0, 600.0, 2000.0), "a few pixels short still follows");
    assert!(!follow::at_bottom(1300.0, 600.0, 2000.0), "scrolled up to read");
    assert!(!follow::at_bottom(1400.0, 600.0, 2200.0), "more came in while reading further up");
}

/// A Question Is Answered In Place, One Answer Per Item
#[test]
fn a_question_is_answered_in_place_one_answer_per_item() {
    let options = json!([{"label": "Red", "description": ""}, {"label": "Blue", "description": ""}, {"label": "Green", "description": ""}]);
    let q: Question = serde_json::from_value(json!({"id": "q1", "items": [
        {"header": "Colour", "question": "Which colour?", "multi": false, "options": options},
        {"header": "Extras", "question": "Which extras?", "multi": true, "options": options},
    ], "answers": null, "reply": null}))
    .expect("a question");
    let mut draft = AnswerDraft::new(q.clone());
    assert_eq!(draft.answers(), None);
    draft.toggle("Red", 0);
    draft.toggle("Blue", 0);
    assert_eq!(draft.answers(), None, "an item without an answer holds the whole answer back");
    draft.toggle("Green", 1);
    draft.toggle("Red", 1);
    draft.set_typed(1, "a stripe");
    assert_eq!(draft.answers(), Some(strings(&["Blue", "Red, Green, a stripe"])));
    draft.set_typed(0, "teal");
    assert_eq!(draft.answers().map(|a| a[0].clone()), Some("teal".to_string()));

    let plan = Plan::answer("w1", &q, &strings(&["teal", "Red, Green"])).expect("an answer");
    assert_eq!(plan.argv, strings(&["answer", "w1", "teal", "Red, Green"]));
    assert_eq!(Plan::answer("w1", &q, &strings(&["teal"])), Err(PlanError::Empty));
    assert_eq!(Plan::answer("w1", &q, &strings(&["teal", "  "])), Err(PlanError::Empty));
}

/// The Key That Sends A Message Is The Person's Choice
#[test]
fn the_key_that_sends_a_message_is_the_persons_choice() {
    assert_eq!(SendKey::STANDARD, SendKey::Return);
    assert_eq!(SendKey::Return.press(Chord::NONE), ReturnPress::Send);
    assert_eq!(SendKey::Return.press(Chord::SHIFT), ReturnPress::Newline);
    for key in [SendKey::ControlReturn, SendKey::AltReturn] {
        assert_eq!(key.press(key.chord()), ReturnPress::Send);
        assert_eq!(key.press(Chord::NONE), ReturnPress::Newline, "Enter starts a new line when {key:?} sends");
        assert_eq!(SendKey::named(key.name()), Some(key));
    }
    assert_eq!(SendKey::ControlReturn.press(Chord::ALT), ReturnPress::Newline);
    assert_eq!(SendKey::AltReturn.press(Chord { shift: true, ..Chord::ALT }), ReturnPress::Newline);
    assert_eq!(SendKey::Return.press(Chord::CONTROL), ReturnPress::Newline);
}

/// Stopping A Session Puts Back What It Had Not Read
#[test]
fn stopping_a_session_puts_back_what_it_had_not_read() {
    let home = Home::new();
    // A claude with no live session: nothing is working, so stopping only
    // takes back what was waiting.
    executable(&home.dir.join("bin/claude"), "#!/bin/sh\necho '[]'\n");
    let id = home.wd(&["add", "demo", "Told the wrong thing"]);
    home.wd(&["attach", &id, "c0ffee00-0000-4000-8000-000000000002", "--runner", "claude"]);
    home.wd(&["pause", &id]);
    home.wd(&["send", &id, "drop the tables"]);
    home.wd(&["send", &id, "all of them"]);

    assert_eq!(Plan::stop(&id).argv, strings(&["stop", &id, "--json"]));
    let run = Run::new();
    let store = run.store(Channel::spawn(built_wd(), Some(&home.with_home())).expect("wd"));
    let back = run.wait(store.stop_work(&Plan::stop(&id))).expect("stopped");
    assert_eq!(back, "drop the tables\n\nall of them");
    store.stop();
}

/// A Message Shows At Once And The Field Is Free Again
#[test]
fn a_message_shows_at_once_and_the_field_is_free_again() {
    let home = Home::new();
    let goal = home.goal("demo", "Pick a database");
    let run = Run::new();
    let store = run.store(home.channel());
    let settled = |id: u64| -> Outgoing {
        let done = run.until(Duration::from_secs(5), || {
            store.outbox().iter().any(|m| m.id == id && m.state != OutgoingState::Sending)
        });
        assert!(done, "message {id} never settled");
        store.outbox().iter().find(|m| m.id == id).cloned().expect("still in the outbox")
    };

    let taken = store.post(Plan::decide(&goal, "use sqlite").expect("plan"), &goal, None);
    assert!(taken.state == OutgoingState::Sending && taken.text == "use sqlite");
    assert_eq!(store.outbox().iter().map(|m| m.id).collect::<Vec<_>>(), vec![taken.id]);
    assert_eq!(settled(taken.id).state, OutgoingState::Sent);
    let detail = run.wait(store.goal(&goal)).expect("goal");
    assert!(taken.arrived_in_goal(&detail));
    assert_eq!(detail.yours().iter().map(|e| e.body.clone()).collect::<Vec<_>>(), vec!["use sqlite"]);
    assert!(detail.is_yours(&detail.yours()[0]));

    let refused = store.post(Plan::send("nope", "hello").expect("plan"), "nope", None);
    match settled(refused.id).state {
        OutgoingState::Failed { why, sent: false } => assert!(!why.is_empty()),
        other => panic!("a message to unknown work was not refused: {other:?}"),
    }
    store.discard(refused.id);
    assert!(!store.outbox().iter().any(|m| m.id == refused.id));

    let half = store.post(
        Plan::decide(&goal, "keep it local").expect("plan"),
        &goal,
        Some(Plan::typed("wd decide nope x").expect("plan")),
    );
    match settled(half.id).state {
        OutgoingState::Failed { sent: true, .. } => {}
        other => panic!("a message whose follow-up failed is not marked taken: {other:?}"),
    }
    store.retry(half.id);
    settled(half.id);
    let again = run.wait(store.goal(&goal)).expect("goal");
    assert_eq!(again.yours().iter().map(|e| e.body.clone()).collect::<Vec<_>>(), vec!["use sqlite", "keep it local"]);
}

/// Escape Pressed Twice Quickly Stops The Work
#[test]
fn escape_pressed_twice_quickly_stops_the_work() {
    let t0 = Instant::now();
    let mut taps = EscapeTaps::default();
    assert!(!taps.tap(t0), "a lone Escape stops nothing");
    assert!(taps.tap(t0 + Duration::from_millis(500)));
    assert!(!taps.tap(t0 + Duration::from_millis(700)), "a third quick Escape does not stop again");
    let mut slow = EscapeTaps::default();
    slow.tap(t0);
    assert!(!slow.tap(t0 + Duration::from_millis(1200)), "a second Escape after the window stops nothing");
}

/// The Agents Panel Shows Each Task And The Subagents It Started
#[test]
fn the_agents_panel_shows_each_task_and_the_subagents_it_started() {
    let (a, b, c, quiet) = (work("a", "running"), work("b", "needs-input"), work("c", "done"), work("q", "queued"));
    let usage = |tokens: i64, from: &str, to: &str| json!({"model": "m", "tokens": tokens, "tools": 1, "started": from, "last": to});
    let activity = |status: &str, asking: bool, usage: Value, subagents: Value| -> Activity {
        let question =
            if asking { json!({"id": "q1", "items": [], "answers": null, "reply": null}) } else { Value::Null };
        serde_json::from_value(json!({"now": "working", "kind": "text", "at": null, "asking": question, "error": null,
            "status": status, "usage": usage, "subagents": subagents}))
        .expect("an activity")
    };
    let subs = json!([
        {"id": "s1", "description": "Probe", "type": "fork", "done": true, "said": "found", "usage": usage(5, "2026-10-11T10:00:00Z", "2026-10-11T10:02:00Z")},
        {"id": "s2", "description": "Dig", "type": "fork", "done": false, "said": null, "usage": usage(7, "2026-10-11T10:00:00Z", "2026-10-11T10:01:00Z")},
    ]);
    let roster = AgentRoster::for_goal(
        None,
        false,
        &[a.clone(), b, c, quiet],
        &HashMap::from([
            (
                "a".to_string(),
                activity("running", false, usage(100, "2026-10-11T10:00:00Z", "2026-10-11T10:05:00Z"), subs),
            ),
            (
                "b".to_string(),
                activity("waiting", true, usage(10, "2026-10-11T10:00:00Z", "2026-10-11T10:01:00Z"), json!([])),
            ),
            (
                "c".to_string(),
                activity("stopped", false, usage(1000, "2026-10-11T09:00:00Z", "2026-10-11T09:30:00Z"), json!([])),
            ),
        ]),
    );
    assert_eq!(
        roster.cards.iter().map(|c| c.task.as_str()).collect::<Vec<_>>(),
        vec!["a", "b", "c"],
        "a task with no session has no card"
    );
    assert_eq!(
        roster.cards.iter().map(|c| c.phase).collect::<Vec<_>>(),
        vec![Phase::Working, Phase::Asking, Phase::Settled]
    );
    assert_eq!(
        roster.cards[0].subagents.iter().map(|c| c.phase).collect::<Vec<_>>(),
        vec![Phase::Settled, Phase::Working]
    );
    assert_eq!(roster.settled(), 2);
    assert_eq!(roster.tokens(), 1122);
    let now = kit::time::instant("2026-10-11T10:10:00Z").expect("a time");
    assert_eq!(roster.cards[0].elapsed(now), Some(600), "a working card runs to now");
    assert_eq!(roster.cards[2].elapsed(now), Some(1800), "a settled card stops at its last record");

    let goal = work("g", "queued");
    let short = || usage(9, "2026-10-11T10:00:00Z", "2026-10-11T10:00:30Z");
    let planned = AgentRoster::for_goal(
        Some(&goal),
        false,
        std::slice::from_ref(&a),
        &HashMap::from([
            ("g".to_string(), activity("running", false, short(), json!([]))),
            ("a".to_string(), activity("running", false, short(), json!([]))),
        ]),
    );
    assert_eq!(
        planned.cards.iter().map(|c| c.title.as_str()).collect::<Vec<_>>(),
        vec!["Plan", "T"],
        "a goal's planning comes first"
    );
    let overseen = AgentRoster::for_goal(
        Some(&goal),
        true,
        std::slice::from_ref(&a),
        &HashMap::from([("g".to_string(), activity("idle", false, short(), json!([])))]),
    );
    assert_eq!(overseen.cards.iter().map(|c| c.title.as_str()).collect::<Vec<_>>(), vec!["Supervisor"]);
    let unplanned = AgentRoster::for_goal(
        Some(&goal),
        false,
        std::slice::from_ref(&a),
        &HashMap::from([("a".to_string(), activity("running", false, short(), json!([])))]),
    );
    assert_eq!(unplanned.cards.len(), 1);

    let home = Home::new();
    executable(&home.dir.join("bin/claude"), "#!/bin/sh\necho '[]'\n");
    let id = home.wd(&["add", "demo", "Delegates"]);
    let session = "c0ffee00-0000-4000-8000-000000000003";
    home.wd(&["attach", &id, session, "--runner", "claude"]);
    let dir = home.dir.join(".claude/projects/anywhere");
    fs::create_dir_all(dir.join(format!("{session}/subagents"))).expect("subagents dir");
    fs::write(
        dir.join(format!("{session}.jsonl")),
        "{\"type\":\"user\",\"timestamp\":\"2026-10-11T10:00:00Z\",\"message\":{\"content\":\"the brief\"}}\n",
    )
    .expect("store");
    fs::write(
        dir.join(format!("{session}/subagents/agent-s9.jsonl")),
        "{\"type\":\"user\",\"isSidechain\":true,\"timestamp\":\"2026-10-11T10:01:00Z\",\"message\":{\"content\":\"probe the parser\"}}\n",
    )
    .expect("subagent store");
    fs::write(
        dir.join(format!("{session}/subagents/agent-s9.meta.json")),
        r#"{"agentType":"fork","description":"Probe"}"#,
    )
    .expect("meta");
    let run = Run::new();
    let store = run.store(Channel::spawn(built_wd(), Some(&home.with_home())).expect("wd"));
    let main = Feed::new(Rc::clone(&store), &id, None);
    run.wait(main.read());
    assert_eq!(main.state().subagents.iter().map(|s| s.id.as_str()).collect::<Vec<_>>(), vec!["s9"]);
    let probe = Feed::new(Rc::clone(&store), &id, Some("s9"));
    run.wait(probe.read());
    assert_eq!(probe.state().failure, None);
    assert_eq!(
        probe.state().steps.iter().map(|s| s.text.clone()).collect::<Vec<_>>(),
        vec![Some("probe the parser".to_string())]
    );
    store.stop();
}

/// A Goal Reads As One Conversation With Its Supervisor And Its Agents
#[test]
fn a_goal_reads_as_one_conversation_with_its_supervisor_and_its_agents() {
    let event = |id: i64, work: &str, kind: &str, body: &str, at: &str| json!({"id": id, "work": work, "kind": kind, "body": body, "at": format!("2026-10-12T01:{at}Z"), "effective": null, "decision": null});
    let answer = |id: i64, work: &str, body: &str, source: &str, at: &str| {
        json!({"id": id, "work": work, "kind": "answer", "body": body, "at": format!("2026-10-12T01:{at}Z"), "effective": null,
            "decision": {"question": "q", "answer": body, "source": source, "runner": "", "model": "", "tokens": 0, "reverses": 0, "reversed_by": 0}})
    };
    let task = |id: &str, title: &str, state: &str| {
        let mut w = work_json(id, state);
        w["title"] = json!(title);
        w
    };
    let supervised = "The collector never calls the detector.\nACTIONS:\nSEND a: wire it in\nTASK: Weekly note\n\nDone:\n- SEND a: wire it in\n- TASK: Weekly note\n- RESOLVE 18: a wires it in\n\nRefused:\n- START b — b is review, already under way";
    let detail = json!({
        "goal": task("g", "Release notes", "running"),
        "tasks": [task("a", "Impacts", "running"), task("b", "Collector", "review"), task("n", "Weekly note", "running")],
        "rollup": {"counts": {}, "done": 0, "total": 3},
        "events": [
            event(5, "g", "supervised", supervised, "05:00"),
            event(1, "g", "decision", "keep the js script", "01:00"),
            event(2, "a", "state", "running", "02:00"),
            event(3, "a", "note", "stopped without a report: wiring belongs to the collector", "03:00"),
            event(4, "g", "look", "a task was noted on (1)", "04:00"),
            event(6, "g", "spawn", "supervise claude s1", "04:00"),
            event(7, "b", "state", "dropped", "06:00"),
            event(8, "a", "question", "which driver?", "07:00"),
            answer(9, "a", "sqlite", "director", "08:00"),
            event(10, "a", "sent", "sqlite", "08:00"),
            event(11, "g", "question", "Tag v1.0.0?\n- yes (recommended)\n- no", "09:00"),
            answer(12, "g", "yes", "person", "10:00"),
        ],
        "claims": [], "landings": [], "delivery": null, "activity": {},
        "asking": {"id": "event-11", "items": [{"header": "", "question": "Tag v1.0.0?", "multi": false,
            "options": [{"label": "yes (recommended)", "description": ""}, {"label": "no", "description": ""}]}], "answers": null, "reply": null},
    });
    let goal: GoalDetail = serde_json::from_value(detail).expect("a goal");
    assert!(goal.supervised());
    assert_eq!(
        goal.asking.as_ref().map(|q| q.items[0].options.iter().map(|o| o.label.clone()).collect::<Vec<_>>()),
        Some(strings(&["yes (recommended)", "no"]))
    );
    let steps = goal.thread();
    assert_eq!(
        steps.iter().map(|s| s.id()).collect::<Vec<_>>(),
        vec![1, 3, 4, 5, 7, 8, 11],
        "oldest first; a state change that does not end work, a spawn and an answer's message are left out"
    );
    let ThreadStep::Exchange(_, Some(auto)) = &steps[5] else { panic!("step 6 is {:?}", steps[5]) };
    assert!(auto.id == 9 && auto.answered_by_director() && !goal.is_yours(auto));
    let ThreadStep::Exchange(asked, Some(mine)) = &steps[6] else { panic!("step 7 is {:?}", steps[6]) };
    assert_eq!(
        asked.asked_items(),
        vec![AskedItem { question: "Tag v1.0.0?".into(), choices: strings(&["yes (recommended)", "no"]) }]
    );
    assert!(mine.id == 12 && !mine.answered_by_director());
    assert_eq!(goal.yours().iter().map(|e| e.id).collect::<Vec<_>>(), vec![1, 12]);
    let ThreadStep::Stopped(_, said) = &steps[1] else { panic!("step 2 is {:?}", steps[1]) };
    assert_eq!(said, "wiring belongs to the collector");
    let ThreadStep::Look(look) = &steps[2] else { panic!("step 3 is {:?}", steps[2]) };
    assert_eq!(look.body, "a task was noted on (1)");
    let ThreadStep::Supervisor(_, reply) = &steps[3] else { panic!("step 4 is {:?}", steps[3]) };
    assert_eq!(reply.said, "The collector never calls the detector.", "the action lines are not said twice");
    assert_eq!(
        reply.actions.iter().map(|a| a.task.clone()).collect::<Vec<_>>(),
        vec![Some("a".to_string()), Some("n".to_string()), None],
        "each action names the task it told or filed"
    );
    assert_eq!(reply.refused, strings(&["START b — b is review, already under way"]));
}

/// The Sidebar Lists Work As Threads, Most Recently Active First
#[test]
fn the_sidebar_lists_work_as_threads_most_recently_active_first() {
    let item = |id: &str, state: &str, project: &str, updated: &str| {
        let mut w = work_json(id, state);
        w["project"] = json!(project);
        w["updated"] = json!(updated);
        w
    };
    let goal = |w: Value, band: &str| json!({"work": w, "rollup": {"counts": {}, "done": 0, "total": 1}, "band": band});
    let board: Board = serde_json::from_value(json!({
        "goals": [
            goal(item("old", "running", "demo", "2026-10-01"), "in-flight"),
            goal(item("new", "needs-input", "demo", "2026-10-09"), "needs-you"),
            goal(item("later", "queued", "demo", "2026-10-08"), "in-flight"),
            goal(item("shipped", "done", "demo", "2026-10-10"), "at-rest"),
            goal(item("elsewhere", "running", "other", "2026-10-11"), "in-flight"),
        ],
        "standalone": [{"work": item("fix", "running", "demo", "2026-10-05"), "band": "in-flight"}],
    }))
    .expect("a board");
    let ids = |threads: &[BoardThread]| threads.iter().map(|t| t.work.id.clone()).collect::<Vec<_>>();
    let t = board.threads(Some("demo"), None);
    assert_eq!(ids(&t.open), strings(&["new", "fix", "old"]));
    assert_eq!(ids(&t.queued), strings(&["later"]));
    assert_eq!(ids(&t.settled), strings(&["shipped"]));
    assert_eq!(board.threads(None, None).open.first().map(|t| t.work.id.as_str()), Some("elsewhere"));
    assert_eq!(ids(&board.threads(Some("demo"), Some(Band::NeedsYou)).open), strings(&["new"]));
}

/// The Next Gate Is One Button
#[test]
fn the_next_gate_is_one_button() {
    let home = Home::new();
    let id = home.wd(&["add", "demo", "Gated"]);
    let run = Run::new();
    let store = run.store(home.channel());
    assert_eq!(Plan::passing(&run.wait(store.gate(&id)).expect("gate")), None, "queued work waits on no gate");
    home.wd(&["set", &id, "running"]);
    home.wd(&["set", &id, "review"]);
    home.wd(&["report", &id, "DONE\nSTATUS: DONE"]);
    let verify = Plan::passing(&run.wait(store.gate(&id)).expect("gate")).expect("a gate to pass");
    assert_eq!(verify.argv, strings(&["verify", &id]));
    assert_eq!(verify.title, "Verify");
    store.stop();
}

/// Work That Comes To Wait On You Is Announced Once
#[test]
fn work_that_comes_to_wait_on_you_is_announced_once() {
    let board = |rows: &[(&str, &str, &str)]| -> Board {
        let goals: Vec<Value> = rows
            .iter()
            .map(|(id, state, band)| json!({"work": work_json(id, state), "rollup": {"counts": {}, "done": 0, "total": 1}, "band": band}))
            .collect();
        serde_json::from_value(json!({"goals": goals, "standalone": []})).expect("a board")
    };
    let first =
        board(&[("a", "running", "in-flight"), ("b", "needs-input", "needs-you"), ("c", "running", "in-flight")]);
    assert!(first.arrivals(None).is_empty(), "nothing is announced from the first read");
    let second = board(&[
        ("a", "needs-input", "needs-you"),
        ("b", "needs-input", "needs-you"),
        ("c", "review", "ready-to-push"),
        ("d", "paused", "paused"),
    ]);
    assert_eq!(
        second.arrivals(Some(&first)).into_iter().map(|a| a.work.id).collect::<HashSet<_>>(),
        HashSet::from(["a".to_string(), "c".to_string()])
    );
    assert!(second.arrivals(Some(&second)).is_empty(), "staying put is not news");
}

/// A Product Is Added From A Folder
#[test]
fn a_product_is_added_from_a_folder() {
    assert_eq!(Plan::product_name("/home/me/code/My App_v2"), "my-app-v2");
    let home = Home::new();
    let folder = home.dir.join("My App");
    fs::create_dir_all(&folder).expect("folder");
    let run = Run::new();
    let store = run.store(home.channel());
    run.wait(store.start());
    let runners: HashSet<String> =
        run.wait(store.doctor()).expect("doctor").runners.into_iter().map(|r| r.runner).collect();
    assert!(["claude", "opencode", "codex"].iter().all(|r| runners.contains(*r)), "{runners:?}");
    let path = folder.display().to_string();
    let plan = Plan::add_product(&Plan::product_name(&path), &path, "claude", "ask").expect("a plan");
    assert_eq!(plan.argv[..7], strings(&["projects", "add", "my-app", &path, "--runner", "claude", "--mode"])[..]);
    assert_eq!(run.wait(store.run(&plan)).expect("ran").code, 0);
    run.wait(store.reload_projects()).expect("projects");
    assert!(store.projects().iter().any(|p| p.name == "my-app" && p.mode == "ask"));
    assert_eq!(Plan::add_product(" ", &path, "claude", "auto"), Err(PlanError::Empty));
    store.stop();
}

/// A Correction Can Be Kept As Feedback
#[test]
fn a_correction_can_be_kept_as_feedback() {
    let home = Home::new();
    let id = home.wd(&["add", "demo", "Taught"]);
    let run = Run::new();
    let store = run.store(home.channel());
    let taught = run.wait(store.work(&id)).expect("work");
    let plan = Plan::remember("  always pin versions ", &taught).expect("a plan");
    assert_eq!(
        plan.argv,
        strings(&[
            "feedback",
            "add",
            "always pin versions",
            "--work",
            &id,
            "--project",
            "demo",
            "--source",
            "director"
        ])
    );
    assert_eq!(run.wait(store.run(&plan)).expect("ran").code, 0);
    assert!(home.wd(&["feedback", "list"]).contains("always pin versions"));
    assert_eq!(Plan::remember(" ", &taught), Err(PlanError::Empty));
    store.stop();
}

/// A Goal Is Renamed Where Its Name Shows
#[test]
fn a_goal_is_renamed_where_its_name_shows() {
    let home = Home::new();
    let id = home.goal("demo", "Fix the login");
    let run = Run::new();
    let store = run.store(home.channel());
    run.wait(store.start());
    let plan = Plan::rename(&id, "  Fix the login page  ").expect("a plan");
    assert_eq!(plan.argv, strings(&["rename", &id, "Fix the login page"]));
    assert_eq!(run.wait(store.run(&plan)).expect("ran").code, 0);
    assert_eq!(run.wait(store.work(&id)).expect("work").title, "Fix the login page");
    let shown = run.until(Duration::from_secs(2), || {
        store
            .board()
            .as_ref()
            .is_some_and(|b| b.recent_goals(None).iter().any(|w| w.id == id && w.title == "Fix the login page"))
    });
    assert!(shown, "the sidebar's board has the new name");
    assert_eq!(Plan::rename(&id, " \n "), Err(PlanError::Empty));
    store.stop();
}
