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

/// A goal view from the fields a test names, the tree's own left empty.
fn goal_view(mut v: Value) -> GoalDetail {
    let empty = json!({"chain": [], "tree": [], "paths": {}, "done_when": null, "finish": null,
        "progress": {"done": 0, "total": 0, "superseded": 0}, "not_done_because": [], "needs_you": [],
        "course": null, "superseded": [], "proposed": false, "reasoning": null});
    for (k, value) in empty.as_object().expect("an object") {
        if v.get(k).is_none() {
            v[k] = value.clone();
        }
    }
    serde_json::from_value(v).expect("a goal view")
}

/// A goal as the board lists it, with nothing of its tree told.
fn board_goal(work: Value, band: &str) -> Value {
    json!({"work": work, "rollup": {"counts": {}, "done": 0, "total": 1}, "band": band,
        "progress": {"done": 0, "total": 1, "superseded": 0}, "needs_you": 0, "finish": null, "finished": null, "proposed": false})
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

/// Wd Runs With The Person's Login Shell PATH
#[test]
fn wd_runs_with_the_persons_login_shell_path() {
    assert_eq!(login_path::parse("Last login: today\n__WD_PATH__/a:/b__WD_PATH__\nbye").as_deref(), Some("/a:/b"));
    assert_eq!(login_path::parse("no markers here"), None);
    assert_eq!(login_path::parse("__WD_PATH____WD_PATH__"), None);
    assert_eq!(login_path::merge("/a:/b", "/b:/c::/a"), "/a:/b:/c");
    assert_eq!(login_path::path(Some("/login:/usr/bin"), "/usr/bin:/bin", "/h", |_| true), "/login:/usr/bin:/bin");
    let present = ["/h/.opencode/bin", "/h/.local/bin", "/usr/local/bin", "/h/.npm-global/bin"];
    assert_eq!(
        login_path::path(None, "/usr/bin:/bin:/usr/local/bin", "/h", |d| present.contains(&d)),
        "/usr/bin:/bin:/usr/local/bin:/h/.local/bin:/h/.opencode/bin:/h/.npm-global/bin"
    );
    let with_shell: HashMap<String, String> = [("SHELL".to_string(), "/bin/fish".to_string())].into();
    assert_eq!(login_path::shell(&with_shell, Some("/bin/zsh")), "/bin/fish");
    assert_eq!(login_path::shell(&HashMap::new(), Some("/bin/zsh")), "/bin/zsh");
    assert_eq!(login_path::shell(&HashMap::new(), None), "/bin/bash");

    let home = Home::new();
    let login = home.dir.join("login-bin");
    executable(&login.join("codex"), "#!/bin/sh\n");
    let shell = home.dir.join("shell");
    // Answers only when run as an interactive login shell.
    executable(
        &shell,
        &format!(
            "#!/bin/sh\necho 'welcome back'\n[ \"$1 $2\" = '-l -i' ] && printf '__WD_PATH__{}:/usr/bin__WD_PATH__'\necho 'rc noise'\n",
            login.display()
        ),
    );
    let mut desktop = home.env.clone();
    desktop.insert("PATH".into(), "/usr/bin:/bin:/usr/sbin:/sbin".into());
    desktop.insert("SHELL".into(), shell.display().to_string());
    let env = login_path::resolve(&desktop, Duration::from_secs(5));
    assert_eq!(env["PATH"], format!("{}:/usr/bin:/bin:/usr/sbin:/sbin", login.display()));
    let others = |e: &HashMap<String, String>| {
        e.iter().filter(|(k, _)| *k != "PATH").map(|(k, v)| (k.clone(), v.clone())).collect::<HashMap<_, _>>()
    };
    assert_eq!(others(&env), others(&desktop), "nothing else changes");
    let run = Run::new();
    let store = run.store(Channel::spawn(built_wd(), Some(&env)).expect("wd serve --stdio"));
    let doctor = run.wait(store.doctor()).expect("doctor");
    assert!(
        doctor.runners.iter().any(|r| r.runner == "codex" && r.detected),
        "wd finds a runner only the login shell's PATH has"
    );
    store.stop();

    executable(&shell, "#!/bin/sh\nsleep 30\n");
    let started = Instant::now();
    let late = login_path::resolve(&desktop, Duration::from_millis(500));
    assert!(started.elapsed() < Duration::from_secs(5));
    assert!(
        late["PATH"].starts_with("/usr/bin:/bin:/usr/sbin:/sbin"),
        "a shell that does not answer falls back to the inherited PATH"
    );
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
    assert_eq!(draft.answers(), Some(strings(&["Blue", "(no answer)"])), "an item left unanswered says so");
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

    let yes_no: Question = serde_json::from_value(json!({"id": "q2", "items": [
        {"header": "", "question": "Ship it?", "multi": false, "options": [
            {"label": "Yes (recommended)", "description": ""}, {"label": "No", "description": ""}]},
    ], "answers": null, "reply": null}))
    .expect("a question");
    let announced: Vec<&str> = yes_no.items[0].options.iter().map(QuestionOption::shown).collect();
    assert_eq!(announced, vec!["Yes", "No"], "each option is announced by what it shows");
    let finish: NeedYou = serde_json::from_value(json!({"work": "f", "path": "P", "question": {"id": "q3", "items": [
        {"header": "", "question": "Where does this goal finish?", "multi": false, "options": [
            {"label": "research (recommended)", "description": ""}]}], "answers": null, "reply": null},
        "change": {"kind": "finish", "choices": ["research (recommended)"]}}))
    .expect("a finish-line question");
    assert_eq!(
        finish.shown_question().items[0].options[0].shown(),
        "Research complete: a written comparison and a pick",
        "a finish line is announced by its title, not its terse label"
    );
    let mut draft = AnswerDraft::new(yes_no);
    draft.toggle("Yes (recommended)", 0);
    assert_eq!(draft.answers(), Some(strings(&["Yes"])), "the recommended mark is not sent");
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
        "claims": [], "landings": [], "delivery": null, "activity": {}, "reports": {},
        "asking": {"id": "event-11", "items": [{"header": "", "question": "Tag v1.0.0?", "multi": false,
            "options": [{"label": "yes (recommended)", "description": ""}, {"label": "no", "description": ""}]}], "answers": null, "reply": null},
    });
    let goal = goal_view(detail);
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

/// A node of a goal's tree as wd serves one, from the fields a test names.
fn node_json(id: &str, kind: &str, state: &str, fields: Value) -> Value {
    let mut w = work_json(id, state);
    w["kind"] = json!(kind);
    w["title"] = json!(format!("Title {id}"));
    let mut n = json!({"work": w, "group": false, "done_when": null, "after": [], "waiting_on": [],
        "why_not_moving": null, "owner": null, "planned_into": null, "progress": null, "doing": null,
        "steps": [], "judged": null, "needs_you": 0, "children": []});
    for (k, v) in fields.as_object().expect("an object") {
        n[k] = v.clone();
    }
    n
}

/// A Goal Page Shows What Done Means, What Needs You And Its Sub-Goals
#[test]
fn a_goal_page_shows_what_done_means_what_needs_you_and_its_sub_goals() {
    let step = |n: i64, state: &str| json!({"n": n, "text": format!("step {n}"), "state": state});
    let claude = json!({"runner": "claude", "agent": null});
    let child = node_json("r", "task", "needs-input", json!({"needs_you": 1}));
    let goal = goal_view(json!({
        "goal": work_json("g", "running"), "tasks": [], "rollup": {"counts": {}, "done": 1, "total": 4},
        "events": [], "claims": [], "landings": [], "delivery": null, "activity": {}, "reports": {},
        "asking": null,
        "chain": [{"id": "g", "title": "Release notes", "state": "running"}],
        "done_when": "every release has notes",
        "finish": {"line": "complete PR", "source": "taste"},
        "progress": {"done": 1, "total": 4, "superseded": 1},
        "not_done_because": ["3 sub-goals open", "done-when not judged met yet"],
        "course": {"verdict": "on course", "why": "the tag format suits the rest", "at": "2026-10-12T01:00:00Z"},
        "tree": [
            node_json("t", "task", "done", json!({"done_when": "each release has a tag", "owner": claude,
                "steps": [step(1, "done"), step(2, "done")], "judged": {"verdict": "met", "why": "ok"}})),
            node_json("d", "goal", "running", json!({"planned_into": 3, "progress": {"done": 1, "total": 3, "superseded": 0},
                "needs_you": 1, "children": [child]})),
            node_json("c", "task", "running", json!({"owner": claude, "doing": "Writing the template",
                "steps": [step(1, "done"), step(2, "doing"), step(3, "todo"), step(4, "dropped")]})),
            node_json("a", "task", "queued", json!({"why_not_moving": "waits on Title d, Title c"})),
            node_json("x", "task", "running", json!({"steps": [step(1, "doing")]})),
            node_json("w", "task", "superseded", json!({"why_not_moving": "superseded: guides live with each release"})),
        ],
    }));
    assert!(!goal.nested());
    assert_eq!(
        goal.finish_card(),
        Some((
            "A complete PR".to_string(),
            "From your taste: a PR open with checks green and the done-when met.".to_string()
        ))
    );
    assert_eq!(goal.course.as_ref().map(Course::badge), Some(("On course".to_string(), Tone::Settled)));
    assert_eq!(goal.progress_line(), "1 of 4 done · 1 superseded");

    let rows = goal.sub_goal_rows();
    let row = |id: &str| rows.iter().find(|r| r.id == id).expect("a row").clone();
    assert_eq!(row("t").meta, "Done when each release has a tag · claude · 2 steps · judged met");
    assert_eq!(row("t").badge, ("Done".to_string(), Tone::Settled));
    assert_eq!(row("d").meta, "Planned into 3 sub-goals · 1 of 3 done");
    assert_eq!(row("d").badge, ("1 needs you".to_string(), Tone::Attention));
    assert_eq!(row("d").opens.as_deref(), Some("d"), "a sub-goal that is a goal opens its page");
    assert_eq!(row("c").meta, "claude · now: Writing the template");
    assert_eq!(row("c").badge, ("Working · 1 of 3".to_string(), Tone::Live), "a dropped step is not counted");
    assert!(row("c").open && !row("x").open, "only the first working task starts with its steps shown");
    assert_eq!((row("c").opens, row("c").mark), (None, Mark::Working));
    assert_eq!(row("a").meta, "Waits on Title d, Title c");
    assert_eq!(row("a").badge, ("Waiting".to_string(), Tone::Waiting));
    assert_eq!(row("w").meta, "Superseded: guides live with each release");
    assert!(row("w").struck);

    let mut nested = goal.clone();
    nested.chain.push(Link { id: "d".into(), title: "Guide".into(), state: WorkState::Running });
    assert!(nested.nested() && nested.finish_card().is_none(), "a sub-goal finishes where its top goal does");

    assert_eq!(
        Plan::add_sub_goal("g", " Guides for each release ", "a guide per release").map(|p| p.argv),
        Ok(strings(&["goal", "add", "g", "Guides for each release", "--done-when", "a guide per release"]))
    );
    assert!(Plan::add_sub_goal("g", "Guides", " ").is_err(), "a sub-goal needs what makes it done");

    let record: RecordView = serde_json::from_value(json!({"summary": {"decisions": 1, "yours": 1, "course": 1,
        "corrections": 0, "questions": 0, "answered_by_director": 0, "chores": 1, "judged": 0}, "entries": [
        {"event": 3, "at": "a", "kind": "course", "what": "on course: fine", "where": "Guide", "by": "director"},
        {"event": 2, "at": "a", "kind": "chore", "what": "refreshed dependencies", "where": "Guide › Flags", "by": "director"},
        {"event": 1, "at": "a", "kind": "decision", "what": "English only", "where": "Guide", "by": "you"}]}))
    .expect("a record");
    assert_eq!(record.director_did().iter().map(|e| e.event).collect::<Vec<_>>(), vec![3, 2]);
}

/// The Conversation Tags Each Turn With Its Sub-Goal And Filters By It
#[test]
fn the_conversation_tags_each_turn_with_its_sub_goal_and_filters_by_it() {
    let event = |id: i64, work: &str, kind: &str, body: &str| {
        json!({"id": id, "work": work, "kind": kind, "body": body, "at": format!("2026-10-12T01:{id:02}:00Z"),
            "effective": null, "decision": null})
    };
    let rollback = node_json("r", "task", "needs-input", json!({}));
    let goal = goal_view(json!({
        "goal": work_json("g", "running"), "tasks": [work_json("t", "done"), work_json("r", "needs-input")],
        "rollup": {"counts": {}, "done": 1, "total": 2}, "claims": [], "landings": [], "delivery": null,
        "activity": {}, "reports": {}, "asking": null,
        "events": [
            event(1, "g", "decision", "English only"),
            event(2, "t", "report", "DONE\nSTATUS: DONE"),
            event(3, "t", "state", "done"),
            event(4, "r", "question", "down scripts?"),
            event(5, "g", "supervised", "On course.\nACTIONS:\nWAIT"),
            event(6, "d", "decision", "done when: rollback steps listed"),
            event(8, "g", "decision", "finishes at: complete PR (from taste)"),
            event(9, "g", "decision", "a1b2 waits on: c3d4"),
            event(10, "g", "decision", "merged a1b2 into c3d4"),
            event(11, "g", "decision", "classified build"),
            json!({"id": 7, "work": "g", "kind": "decision", "body": "added 7f3a: Rollback", "at": "2026-10-12T01:07:00Z",
                "effective": null, "decision": {"question": "the sub-goals of g", "answer": "added 7f3a: Rollback", "source": "person",
                "runner": "", "model": "", "tokens": 0, "reverses": 0, "reversed_by": 0}}),
        ],
        "tree": [
            node_json("t", "task", "done", json!({"judged": {"verdict": "met", "why": "tags"}})),
            node_json("d", "goal", "running", json!({"children": [rollback]})),
        ],
        "paths": {"t": "Tagged", "d": "Guide", "r": "Guide › Rollback"},
    }));
    assert_eq!(goal.tag("r"), Some("Guide › Rollback"));
    assert_eq!(goal.tag("g"), None, "the goal's own turns carry no tag");
    let chips: Vec<(String, usize)> = goal.filters().into_iter().map(|c| (c.label, c.count)).collect();
    assert_eq!(
        chips,
        vec![
            ("Everything".to_string(), 5),
            ("You and the director".to_string(), 2),
            ("Title t".to_string(), 2),
            ("Title d".to_string(), 1),
        ]
    );
    let ids = |filter: ThreadFilter| goal.items(filter).iter().map(ThreadItem::id).collect::<Vec<_>>();
    assert_eq!(ids(ThreadFilter::YouAndDirector), vec![1, 5]);
    assert_eq!(ids(ThreadFilter::SubGoal("d".into())), vec![4], "a sub-goal keeps what happened under it");
    assert_eq!(ids(ThreadFilter::SubGoal("t".into())), vec![2, 3]);
    let done = goal.events.iter().find(|e| e.id == 3).expect("the done event");
    assert_eq!(goal.finished_line(done).as_deref(), Some("Tagged · done · judged met"));
    assert_eq!(goal.finished_line(&goal.events[0]), None);
    assert_eq!(
        goal.addressees(),
        vec![
            ("g".to_string(), "whole goal".to_string()),
            ("t".to_string(), "Tagged".to_string()),
            ("d".to_string(), "Guide".to_string()),
            ("r".to_string(), "Guide › Rollback".to_string()),
        ]
    );
    assert_eq!(Plan::send("r", "only redeploy").map(|p| p.argv), Ok(strings(&["send", "r", "only redeploy"])));
}

/// The Record Lists What Was Decided And Done, Filtered By Kind And Sub-Goal
#[test]
fn the_record_lists_what_was_decided_and_done_filtered_by_kind_and_sub_goal() {
    let entry = |event: i64, kind: &str, what: &str, place: &str, by: &str| json!({"event": event, "at": format!("2026-10-12T{:02}:00:00Z", event), "kind": kind, "what": what, "where": place, "by": by});
    let record: RecordView = serde_json::from_value(json!({
    "summary": {"decisions": 2, "yours": 1, "course": 2, "corrections": 1, "questions": 1,
        "answered_by_director": 1, "chores": 1, "judged": 1},
    "entries": [
        entry(9, "answer", "forward-only", "Guide › Rollback", "director"),
        entry(8, "course", "on course: the sections match", "Guide", "director"),
        entry(7, "judged", "met: tags and lists", "Tagged", "director"),
        entry(6, "chore", "refreshed dependencies", "Guide › Flags", "director"),
        entry(5, "course", "correct: superseded the wiki", "Guidebook", "director"),
        entry(4, "question", "down scripts?", "Guide › Rollback", "director"),
        entry(3, "decision", "English only", "Release notes (top)", "you"),
        entry(2, "finish", "finishes at: complete PR (from taste)", "Release notes (top)", "director"),
        entry(1, "delivery", "https://example.com/pull/7", "Release notes (top)", "director"),
        entry(0, "asked", "Release notes", "Release notes (top)", "you"),
    ]}))
    .expect("a record");
    assert_eq!(
        record.summary.cards().map(|(n, what)| format!("{n} {what}")),
        [
            "2 decisions · 1 yours",
            "2 course checks · 1 correction",
            "1 question · 1 answered by the director",
            "1 chore the director ran",
            "1 report judged",
        ]
        .map(String::from)
    );
    let events = |filter, place: Option<&str>| record.shown(filter, place).iter().map(|e| e.event).collect::<Vec<_>>();
    assert_eq!(events(RecordFilter::Everything, None).len(), 10, "newest first, everything");
    assert_eq!(events(RecordFilter::Decisions, None), vec![3, 2, 0]);
    assert_eq!(events(RecordFilter::Course, None), vec![8, 5]);
    assert_eq!(events(RecordFilter::Questions, None), vec![9, 4]);
    assert_eq!(events(RecordFilter::Reports, None), vec![7]);
    assert_eq!(events(RecordFilter::Chores, None), vec![6]);
    assert_eq!(events(RecordFilter::Delivery, None), vec![1]);
    assert_eq!(
        events(RecordFilter::Everything, Some("Guide")),
        vec![9, 8, 6, 4],
        "on the sub-goal or under it, not Guidebook"
    );
    assert_eq!(events(RecordFilter::Questions, Some("Guide")), vec![9, 4]);
    let badge = |event: i64| record.entries.iter().find(|e| e.event == event).map(RecordEntry::badge);
    assert_eq!(badge(8), Some(("On course".to_string(), Tone::Settled)));
    assert_eq!(badge(5), Some(("Corrected".to_string(), Tone::Live)));
    assert_eq!(badge(7), Some(("Judged met".to_string(), Tone::Settled)));
    assert_eq!(badge(3), Some(("Decision".to_string(), Tone::Decision)));
    assert_eq!(record.entries[1].by, RecordBy::Director);
    assert_eq!(record.entries[6].place, "Release notes (top)");
}

/// A Change Waiting For The Person Shows What Would Change
#[test]
fn a_change_waiting_for_the_person_shows_what_would_change() {
    let question = |id: &str, options: &[&str]| {
        json!({"id": id, "items": [{"header": "", "question": "Change course?", "multi": false,
            "options": options.iter().map(|o| json!({"label": o, "description": ""})).collect::<Vec<_>>()}],
            "answers": null, "reply": null})
    };
    let course = json!({"work": "g", "path": "Why does sync drop events?", "question": question("q1", &["Change course (recommended)", "Keep the goal as it is"]),
        "change": {"kind": "course", "actions": ["SUPERSEDE d3: no longer needed"], "lines": ["Supersede “Compare vendors”: no longer needed"],
            "now": {"title": "Why does sync drop events?", "done_when": "The cause is found", "finish": "discovery"},
            "proposed": {"title": "Sync never drops events", "done_when": "A 429 batch is retried", "finish": "complete PR"},
            "why": "The fix is small."}});
    let finish = json!({"work": "f", "path": "Pick a queue", "question": question("q2", &["research (recommended)", "local build"]),
        "change": {"kind": "finish", "choices": ["research (recommended)", "local build"]}});
    let plan = json!({"work": "s", "path": "Guide", "question": question("q3", &["yes", "no"]),
        "change": {"kind": "plan", "actions": ["GOAL s: Flags | done when: listed"], "lines": ["Add “Flags” under Guide, done when listed"]}});
    let plain =
        json!({"work": "r", "path": "Guide › Rollback", "question": question("q4", &["a", "b"]), "change": null});
    let goal = goal_view(json!({
        "goal": work_json("g", "running"), "tasks": [], "rollup": {"counts": {}, "done": 0, "total": 4},
        "events": [], "claims": [], "landings": [], "delivery": null, "activity": {}, "reports": {}, "asking": null,
        "proposed": true,
        "needs_you": [course, finish, plan, plain],
        "tree": [
            node_json("a1", "goal", "queued", json!({"done_when": "every change keeps who and why", "why_not_moving": "proposed — needs approval"})),
            node_json("a2", "task", "queued", json!({"done_when": "the export is signed", "after": ["a1"]})),
            node_json("a3", "task", "queued", json!({"done_when": "nothing can change it", "after": ["a1"]})),
            node_json("a4", "task", "queued", json!({"done_when": "each clause is mapped", "after": ["a2", "a3"]})),
        ],
    }));
    let cards = goal.proposal().expect("a proposed breakdown");
    let shown: Vec<(usize, String, Vec<String>, bool)> =
        cards.iter().map(|c| (c.number, c.meta.clone(), c.chips.clone(), c.splits)).collect();
    assert_eq!(
        shown,
        vec![
            (
                1,
                "Done when every change keeps who and why".into(),
                strings(&["Starts first", "Will be planned further"]),
                false
            ),
            (2, "Done when the export is signed".into(), strings(&["After 1", "One agent"]), true),
            (3, "Done when nothing can change it".into(), strings(&["After 1", "One agent"]), true),
            (4, "Done when each clause is mapped".into(), strings(&["After 2 and 3", "One agent"]), true),
        ]
    );
    let mut approved = goal.clone();
    approved.proposed = false;
    assert_eq!(approved.proposal(), None, "an approved breakdown is the plan, not a proposal");
    assert_eq!(Plan::approve("g").argv, strings(&["goal", "approve", "g"]));
    assert_eq!(Plan::replan("g").argv, strings(&["goal", "plan", "g"]));
    assert_eq!(
        Plan::edit_sub_goal("a2", None, Some(" the export is a signed PDF ")).map(|p| p.argv),
        Ok(strings(&["goal", "edit", "a2", "--done-when", "the export is a signed PDF"]))
    );
    assert!(Plan::edit_sub_goal("a2", None, None).is_err());
    assert_eq!(Plan::split_sub_goal("a2").argv, strings(&["goal", "split", "a2"]));
    assert_eq!(
        Plan::drop_sub_goal("a4", "not asked").map(|p| p.argv),
        Ok(strings(&["goal", "drop", "a4", "not asked"]))
    );
    assert!(Plan::drop_sub_goal("a4", " ").is_err(), "dropping says why");

    let [course, finish, plan, plain] = [0, 1, 2, 3].map(|i| goal.needs_you[i].clone());
    assert_eq!(course.label("g"), "Course change on the top goal · always yours to decide");
    assert_eq!(
        course.compare(),
        vec![
            ("Goal", "Why does sync drop events?".to_string(), "Sync never drops events".to_string()),
            ("Done when", "The cause is found".to_string(), "A 429 batch is retried".to_string()),
            ("Finishes at", "Discovery".to_string(), "A complete PR".to_string()),
        ]
    );
    let change = course.change.as_ref().expect("a change");
    assert_eq!((change.why.as_str(), change.lines.len()), ("The fix is small.", 1));
    assert_eq!(finish.label("g"), "Before I plan it · where does this goal finish?");
    assert_eq!(finish.change.as_ref().map(|c| c.choices.len()), Some(2));
    let offered: Vec<(String, Option<String>, String)> = finish.shown_question().items[0]
        .options
        .iter()
        .map(|o| (o.label.clone(), o.title.clone(), o.description.clone()))
        .collect();
    assert_eq!(
        offered,
        vec![
            (
                "research (recommended)".to_string(),
                Some("Research complete: a written comparison and a pick".to_string()),
                "Trade-offs and a recommendation with its reasons. No code changes.".to_string()
            ),
            (
                "local build".to_string(),
                Some("A local build you can try".to_string()),
                "The work on a branch, with how to run it.".to_string()
            ),
        ],
        "each finish line with what it means, answered by its own words"
    );
    assert_eq!(course.shown_question().items[0].question, "Change course?");
    assert_eq!(plan.label("g"), "Needs you · a change to the plan · in Guide");
    assert_eq!(
        plan.change.as_ref().map(|c| c.lines.clone()),
        Some(strings(&["Add “Flags” under Guide, done when listed"]))
    );
    assert_eq!(plain.label("g"), "Needs you · in Guide › Rollback");
    assert!(plain.compare().is_empty());
    assert_eq!(
        Plan::answer(&course.work, &course.question, &strings(&["Change course"])).map(|p| p.argv),
        Ok(strings(&["answer", "g", "Change course"])),
        "a change is answered as its question"
    );
}

/// A Proposal Says Why It Is Broken Down So
#[test]
fn a_proposal_says_why_it_is_broken_down_so() {
    let home = Home::new();
    let top = home.goal("demo", "An inspector can get a full audit trail");
    home.wd(&["goal", "add", &top, "Every change is kept", "--done-when", "it is so"]);
    let run = Run::new();
    let store = run.store(home.channel());
    let g = run.wait(store.goal(&top)).expect("the goal");
    assert_eq!(g.reasoning, None, "with no reasoning, nothing shows");
    let reasoning: Reasoning = serde_json::from_value(
        json!({"why": "An inspector checks three things.", "left_out": " ", "judge_by": "Export last month for one org."}),
    )
    .expect("a reasoning");
    assert_eq!(
        reasoning.parts(),
        vec![
            ("Why this breakdown", "An inspector checks three things."),
            ("How you'd judge it", "Export last month for one org.")
        ],
        "a part the planner did not say is left out"
    );
    store.stop();
}

/// A Proposal Is Reordered, Merged And Planned Again From Its Cards
#[test]
fn a_proposal_is_reordered_merged_and_planned_again_from_its_cards() {
    let home = Home::new();
    let top = home.goal("demo", "An inspector can get a full audit trail");
    let add = |title: &str, after: &str| -> String {
        let mut argv = vec!["goal", "add", top.as_str(), title, "--done-when", "it is so", "--json"];
        if !after.is_empty() {
            argv.extend_from_slice(&["--after", after]);
        }
        serde_json::from_str::<Work>(&home.wd(&argv)).expect("a sub-goal").id
    };
    let a = add("Every record change keeps who and why", "");
    let b = add("An admin can export the trail", &a);
    let c = add("The trail cannot be edited", &a);
    let d = add("A checklist maps to the export", "");
    home.wd(&["goal", "after", &a, &d]);
    let run = Run::new();
    let store = run.store(home.channel());
    let proposed = || {
        let mut g = run.wait(store.goal(&top)).expect("the goal");
        g.proposed = true;
        g
    };
    let ids = |g: &GoalDetail| g.proposal().expect("a proposal").into_iter().map(|c| c.id).collect::<Vec<_>>();
    let g = proposed();
    assert_eq!(
        ids(&g),
        vec![d.clone(), a.clone(), b.clone(), c.clone()],
        "each after what it waits on, else as served"
    );

    let argv = |plans: Vec<Plan>| plans.into_iter().map(|p| p.argv).collect::<Vec<_>>();
    let moves = g.reorder(&c, 1);
    assert_eq!(
        argv(moves.clone()),
        vec![strings(&["goal", "after", &c, &d]), strings(&["goal", "after", &a, &c])],
        "each waits on the one before it; only waits that change are written, from the top"
    );
    assert_eq!(
        argv(g.reorder(&d, 0)),
        vec![strings(&["goal", "after", &c, &b])],
        "a drop in place still sets the order as shown"
    );
    for plan in &moves {
        home.wd(&plan.argv.iter().map(String::as_str).collect::<Vec<_>>());
    }
    let g = proposed();
    assert_eq!(ids(&g), vec![d.clone(), c.clone(), a.clone(), b.clone()]);
    assert_eq!(
        g.reorder(&a, 0).first().map(|p| p.argv.clone()),
        Some(strings(&["goal", "after", &a, "none"])),
        "the first waits on none"
    );

    let cards = g.proposal().expect("a proposal");
    assert_eq!(
        (cards[3].announced("Drop", ""), cards[3].announced("Merge", " with 3")),
        (format!("Drop 4 · {}", cards[3].title), format!("Merge 4 · {} with 3", cards[3].title)),
        "a card's action is announced with the card it acts on"
    );
    let merge = cards[3].merge(&cards[2]);
    assert_eq!(merge.argv, strings(&["goal", "merge", &a, &b]), "the earlier card is kept");
    home.wd(&merge.argv.iter().map(String::as_str).collect::<Vec<_>>());
    assert_eq!(ids(&proposed()), vec![d.clone(), c.clone(), a.clone()]);

    assert_eq!(
        Plan::replan_with(&top, " keep the export read-only ").map(argv),
        Ok(vec![strings(&["decide", &top, "keep the export read-only"]), strings(&["goal", "plan", &top])])
    );
    assert!(Plan::replan_with(&top, "  ").is_err(), "an empty note is not sent");
    store.stop();
}

/// A Person Changes Where A Top Goal Finishes
#[test]
fn a_person_changes_where_a_top_goal_finishes() {
    let home = Home::new();
    let top = home.goal("demo", "Why does sync drop events?");
    let under = serde_json::from_str::<Work>(&home.wd(&[
        "goal",
        "add",
        &top,
        "The cause is found",
        "--done-when",
        "found",
        "--split",
        "--json",
    ]))
    .expect("a sub-goal")
    .id;
    let named: Vec<&str> = FinishLine::ALL.iter().map(|f| f.choice().0).collect();
    assert_eq!(
        named,
        vec![
            "Discovery: the cause found and explained",
            "Research complete: a written comparison and a pick",
            "A local build you can try",
            "A complete PR",
            "Merged",
            "Released"
        ],
        "each is named as a finish-line question names it"
    );
    let plan = Plan::finish(&top, FinishLine::CompletePr);
    assert_eq!(plan.argv, strings(&["goal", "finish", &top, "complete PR"]));
    home.wd(&plan.argv.iter().map(String::as_str).collect::<Vec<_>>());
    let run = Run::new();
    let store = run.store(home.channel());
    let g = run.wait(store.goal(&top)).expect("the goal");
    assert_eq!(g.finish, Some(Finish { line: FinishLine::CompletePr, source: FinishSource::Person }));
    assert_eq!(g.finish_pick(), Some(Some(FinishLine::CompletePr)));
    assert_eq!(FinishLine::menu_name(Some(FinishLine::CompletePr)), "Finishes at: A complete PR");
    assert_eq!(FinishLine::menu_name(None), "Finishes at: Not decided yet");
    let nested = run.wait(store.goal(&under)).expect("the sub-goal");
    assert_eq!(nested.finish_pick(), None, "a sub-goal's page offers none");
    store.stop();
}

/// A Course Change Shows What Was Found And What Happens To Each Sub-Goal
#[test]
fn a_course_change_shows_what_was_found_and_what_happens_to_each_sub_goal() {
    let need: NeedYou = serde_json::from_value(json!({"work": "g", "path": "Why does sync drop events?",
        "question": {"id": "q", "items": [{"header": "", "question": "Change course?\n  Now: …", "multi": false, "options": [
            {"label": "Change course (recommended)", "description": ""}, {"label": "Keep the goal as it is", "description": ""}]}],
            "answers": null, "reply": null},
        "change": {"kind": "course", "actions": [], "lines": [],
            "now": {"title": "Why does sync drop events?", "done_when": "The cause is found", "finish": "discovery"},
            "proposed": {"title": "Sync never drops events", "done_when": "A 429 batch is retried", "finish": "complete PR"},
            "why": "The fix is small.", "found": "The SDK drops a batch on 429.",
            "sub_goals": [
                {"work": "d1", "title": "The cause is found", "status": "kept", "state": "done", "why": null},
                {"work": "d2", "title": "Tenants are counted", "status": "kept", "state": "running", "why": null},
                {"work": "d4", "title": "Old notes", "status": "kept", "state": "queued", "why": null},
                {"work": null, "title": "A batch is retried", "status": "new", "state": null, "why": null},
                {"work": "d3", "title": "Compare vendors", "status": "superseded", "state": "running", "why": "no longer needed"}]}}))
    .expect("a course change");
    let change = need.change.as_ref().expect("a change");
    assert_eq!(change.found.as_deref(), Some("The SDK drops a batch on 429."));
    let rows: Vec<((String, Tone), bool)> = change.sub_goals.iter().map(|s| (s.badge(), s.struck())).collect();
    assert_eq!(
        rows,
        vec![
            (("Kept · done".to_string(), Tone::Settled), false),
            (("Kept · working".to_string(), Tone::Live), false),
            (("Kept · not started".to_string(), Tone::Waiting), false),
            (("New".to_string(), Tone::Live), false),
            (("Superseded".to_string(), Tone::Waiting), true),
        ]
    );
    assert_eq!(need.keep_label().as_deref(), Some("Keep the goal as it is"), "keep is labelled with the keep choice");
    let argv = |p: Option<Plan>| p.map(|p| p.argv);
    assert_eq!(argv(need.keep()), Some(strings(&["answer", "g", "Keep the goal as it is"])));
    assert_eq!(argv(need.change_course()), Some(strings(&["answer", "g", "Change course"])));
    let edited = need
        .edit_change("Sync never drops events", " A batch is retried, and lost events replayed ", FinishLine::Merged)
        .expect("an edited change");
    assert_eq!(
        edited.into_iter().map(|p| p.argv).collect::<Vec<_>>(),
        vec![
            strings(&["answer", "g", "Change course"]),
            strings(&["goal", "edit", "g", "--done-when", "A batch is retried, and lost events replayed"]),
            strings(&["goal", "finish", "g", "merged"]),
        ],
        "only what was edited is set after the answer"
    );
    let as_proposed = need.edit_change("Sync never drops events", "A 429 batch is retried", FinishLine::CompletePr);
    assert_eq!(as_proposed.map(|p| p.len()), Ok(1), "an edit that edits nothing is the change as proposed");
    assert!(need.edit_change(" ", "x", FinishLine::Merged).is_err(), "a goal keeps a title");

    let home = Home::new();
    let goal = home.goal("demo", "Why does sync drop events?");
    home.wd(&["goal", "edit", &goal, "--done-when", "A batch is retried"]);
    let run = Run::new();
    let store = run.store(home.channel());
    assert_eq!(
        run.wait(store.goal(&goal)).ok().and_then(|g| g.done_when),
        Some("A batch is retried".to_string()),
        "a top goal's done-when is set by the edit an edited change runs"
    );
    store.stop();
}

/// A Finish-Line Question Says Why It Asks And Can Be Remembered
#[test]
fn a_finish_line_question_says_why_it_asks_and_can_be_remembered() {
    let need: NeedYou = serde_json::from_value(json!({"work": "f", "path": "Pick a queue",
        "question": {"id": "q", "items": [{"header": "", "question": "Where does this goal finish?", "multi": false, "options": [
            {"label": "research (recommended)", "description": ""}, {"label": "local build", "description": ""}]}],
            "answers": null, "reply": null},
        "change": {"kind": "finish", "choices": ["research (recommended)", "local build"],
            "sources": {"goal": "\"Compare and pick one\" reads as research", "repo": null, "taste": "In auto mode, a complete PR is done"}}}))
    .expect("a finish-line question");
    assert_eq!(
        need.sources(),
        vec![
            ("The goal", "\"Compare and pick one\" reads as research".to_string()),
            ("Your taste", "In auto mode, a complete PR is done".to_string()),
        ],
        "a source that said nothing is left out"
    );
    assert_eq!(need.remember_label("event-bus").as_deref(), Some("Finish goals in event-bus this way from now on"));
    assert_eq!(
        Plan::answer(&need.work, &need.question, &strings(&["research"])).map(|p| p.remembering().argv),
        Ok(strings(&["answer", "f", "research", "--remember"]))
    );
    let plain: NeedYou = serde_json::from_value(json!({"work": "r", "path": "Rollback",
        "question": {"id": "q", "items": [], "answers": null, "reply": null}, "change": null}))
    .expect("a question");
    assert_eq!((plain.sources(), plain.remember_label("p")), (vec![], None), "only a finish-line question");
    let unsaid: NeedYou = serde_json::from_value(json!({"work": "f", "path": "P",
        "question": {"id": "q", "items": [], "answers": null, "reply": null},
        "change": {"kind": "finish", "choices": [], "sources": null}}))
    .expect("a finish-line question");
    assert!(unsaid.sources().is_empty());
}

/// Letting The Director Decide Shows What It Decided Or Why It Declined
#[test]
fn letting_the_director_decide_shows_what_it_decided_or_why_it_declined() {
    assert_eq!(Plan::director_decides("r").argv, strings(&["answer", "r", "--director", "--json"]));
    let decided = DirectorAnswer::read(
        r#"{"ok": true, "answered": true, "told": "session", "answer": "Redeploy only", "applied": [],
            "decided": [{"question": "Rollback?", "answer": "Redeploy only", "why": "down-migrations are rare"}], "declined": []}"#,
    )
    .expect("a decided answer");
    assert!(decided.answered);
    assert_eq!(
        decided.decided,
        vec![DirectorChoice {
            question: "Rollback?".into(),
            answer: Some("Redeploy only".into()),
            why: "down-migrations are rare".into()
        }]
    );
    let declined = DirectorAnswer::read(
        r#"{"ok": true, "answered": false, "told": null, "answer": null, "applied": [],
            "decided": [{"question": "Rollback?", "answer": "Both", "why": "safer"}],
            "declined": [{"question": "Notes in English?", "why": "a business fact only you know"}]}"#,
    )
    .expect("a declined answer");
    assert!(!declined.answered, "a decline answers nothing");
    assert_eq!(
        declined.declined,
        vec![DirectorChoice {
            question: "Notes in English?".into(),
            answer: None,
            why: "a business fact only you know".into()
        }]
    );
    assert!(DirectorAnswer::read("the director decided").is_err(), "only what --json prints is read");

    let event = |id: i64, kind: &str, body: &str, decision: Value| {
        json!({"id": id, "work": "g", "kind": kind, "body": body, "at": format!("2026-10-13T10:00:0{id}Z"),
            "effective": null, "decision": decision})
    };
    let by_director = |question: &str, answer: &str| {
        json!({"question": question, "answer": answer, "source": "director", "runner": "claude", "model": "m",
            "tokens": 0, "reverses": 0, "reversed_by": 0})
    };
    let g = goal_view(json!({
        "goal": work_json("g", "running"), "tasks": [], "rollup": {"counts": {}, "done": 0, "total": 0},
        "claims": [], "landings": [], "delivery": null, "activity": {}, "reports": {}, "asking": null,
        "events": [
            event(1, "question", "Rollback?\n- Redeploy only (recommended)\n- Both", Value::Null),
            event(2, "note", "the director declined \"Rollback?\": a business fact only you know", Value::Null),
            event(3, "answer", "Redeploy only", by_director("Rollback?", "Redeploy only")),
            event(4, "decision", "the director decided \"Rollback?\": Redeploy only, because down-migrations are rare",
                by_director("Rollback?", "Redeploy only")),
        ],
    }));
    let steps = g.thread();
    assert!(
        matches!(&steps[..], [ThreadStep::Exchange(_, Some(_)), ThreadStep::Declined(_, _)]),
        "the answer is read with its question, the decline as a note, the decision with the answer: {steps:?}"
    );
    let ThreadStep::Declined(_, declined) = &steps[1] else { unreachable!() };
    assert_eq!(
        declined,
        &DirectorChoice { question: "Rollback?".into(), answer: None, why: "a business fact only you know".into() }
    );
    let ThreadStep::Exchange(_, Some(answer)) = &steps[0] else { unreachable!() };
    assert_eq!(
        g.decided_for(answer),
        vec![DirectorChoice {
            question: "Rollback?".into(),
            answer: Some("Redeploy only".into()),
            why: "down-migrations are rare".into()
        }]
    );
}

/// The Kit Reads A Goal As A Tree
#[test]
fn the_kit_reads_a_goal_as_a_tree() {
    let home = Home::new();
    let top = home.goal("demo", "Release notes");
    let add = |parent: &str, title: &str, extra: &[&str]| -> String {
        let mut argv = vec!["goal", "add", parent, title, "--done-when", "it is so", "--json"];
        argv.extend_from_slice(extra);
        serde_json::from_str::<Work>(&home.wd(&argv)).expect("a sub-goal").id
    };
    let tagged = add(&top, "Tagged", &[]);
    let guide = add(&top, "Guide", &["--split"]);
    let after = format!("{tagged},{guide}");
    let workflow = add(&top, "Workflow", &["--after", &after]);
    let rollback = add(&guide, "Rollback", &[]);
    home.wd(&["decide", &top, "keep it small"]);

    let run = Run::new();
    let store = run.store(home.channel());
    let g = run.wait(store.goal(&top)).expect("the goal");
    assert_eq!(g.chain.iter().map(|l| l.id.clone()).collect::<Vec<_>>(), vec![top.clone()]);
    assert!(!g.nested());
    assert_eq!(
        g.tree.iter().map(|n| n.work.id.clone()).collect::<Vec<_>>(),
        vec![tagged.clone(), guide.clone(), workflow.clone()]
    );
    assert_eq!(g.progress, Progress { done: 0, total: 3, superseded: 0 });
    assert_eq!(g.not_done_because, strings(&["3 sub-goals are still open"]));
    assert!(g.needs_you.is_empty() && g.course.is_none() && g.finish.is_none());
    assert_eq!(g.paths[&rollback], "Guide › Rollback");
    let guide_node = g.node(&guide).expect("the guide node");
    assert_eq!(guide_node.opens(), Some(guide.as_str()), "a sub-goal that is a goal opens its page");
    assert_eq!(guide_node.planned_into, Some(1));
    assert_eq!(g.node(&rollback).map(|n| n.done_when.clone()), Some(Some("it is so".to_string())));
    assert_eq!(g.node(&tagged).and_then(Node::opens), None, "a task opens no goal page");
    let w = g.node(&workflow).expect("the workflow node");
    assert_eq!(
        w.waiting_on.iter().cloned().collect::<HashSet<_>>(),
        HashSet::from(["Tagged".to_string(), "Guide".to_string()])
    );
    assert!(w.why_not_moving.is_some());

    let nested = run.wait(store.goal(&guide)).expect("the sub-goal");
    assert_eq!(nested.chain.iter().map(|l| l.id.clone()).collect::<Vec<_>>(), vec![top.clone(), guide.clone()]);
    assert!(nested.nested());
    assert_eq!(nested.done_when.as_deref(), Some("it is so"));

    run.wait(store.refresh());
    let board = store.board().clone().expect("a board");
    let listed = board.goals.iter().find(|b| b.work.id == top).expect("the top goal");
    assert_eq!((listed.progress.total, listed.needs_you, listed.finished.clone()), (3, 0, None));
    assert!(board.goals.iter().all(|b| b.work.id != guide), "the board lists top goals only");

    let record = run.wait(store.record(&top)).expect("the record");
    assert_eq!(
        record.entries.first().map(|e| (e.kind, e.what.as_str(), e.by)),
        Some((RecordKind::Decision, "keep it small", RecordBy::You))
    );
    assert_eq!(record.entries.last().map(|e| e.kind), Some(RecordKind::Asked));
    assert!(record.entries.iter().any(|e| e.place == "Guide › Rollback"));
    assert_eq!(
        record.summary.decisions,
        record.entries.iter().filter(|e| e.kind == RecordKind::Decision).count() as i64
    );

    let group: Node = serde_json::from_value(json!({"work": {"id": "group:g:1", "project": "demo", "title": "Old heading",
        "detail": "", "kind": "goal", "state": "superseded", "runner": null, "session": null, "ref": null, "cwd": null,
        "created": "c", "updated": "u", "parent": "g", "heading": null, "claim": null, "impact": null, "goal_type": null,
        "archived": null}, "group": true, "done_when": null, "after": [], "waiting_on": [], "why_not_moving": "superseded",
        "owner": {"runner": "claude", "agent": null}, "planned_into": null, "progress": null, "doing": "Writing",
        "steps": [{"n": 1, "text": "Read", "state": "done"}], "judged": {"verdict": "met", "why": "ok"},
        "needs_you": 0, "children": []}))
    .expect("a group node");
    assert_eq!(group.opens(), None, "a group of an older plan's tasks opens no page");
    store.stop();
}

/// A Goal Is Read As Its Overview, Its Conversation Or Its Record
#[test]
fn a_goal_is_read_as_its_overview_its_conversation_or_its_record() {
    let event = |id: i64, work: &str, kind: &str, body: &str, decision: Value| {
        json!({"id": id, "work": work, "kind": kind, "body": body, "at": format!("2026-10-12T01:{id:02}:00Z"),
            "effective": null, "decision": decision})
    };
    let person = json!({"question": "q", "answer": "yes", "source": "person", "runner": "", "model": "", "tokens": 0,
        "reverses": 0, "reversed_by": 0});
    let asking = json!({"id": "event-1", "items": [], "answers": null, "reply": null});
    let goal = goal_view(json!({
        "goal": work_json("g", "running"),
        "tasks": [work_json("a", "done"), work_json("b", "blocked"), work_json("c", "running"),
            work_json("d", "queued"), work_json("e", "dropped"), work_json("f", "review")],
        "rollup": {"counts": {}, "done": 1, "total": 6},
        "events": [
            event(1, "g", "decision", "keep the js script", Value::Null),
            event(2, "a", "state", "paused", Value::Null),
            event(3, "g", "look", "a task reported (1)", Value::Null),
            event(4, "a", "verify", "pass", Value::Null),
            event(5, "a", "report", "DONE\nSTATUS: DONE\nFILES: x.ts", Value::Null),
            event(6, "g", "supervised", "All fits.\nACTIONS:\nWAIT", Value::Null),
            event(7, "g", "question", "Tag it?\n- yes\n- no", Value::Null),
            event(8, "g", "answer", "yes", person),
            event(9, "b", "state", "blocked", Value::Null),
        ],
        "claims": [], "landings": [], "delivery": null,
        "activity": {"c": {"now": "Bash", "kind": "tool", "at": null, "asking": asking, "error": null,
            "status": "running", "usage": null, "subagents": null}},
        "asking": asking,
        "reports": {"5": {"verdict": "done", "said": "", "files": ["x.ts"], "file_note": "",
            "sections": [{"name": "VERIFY", "text": "go test → 0"}]}},
    }));

    let ids = |filter: ThreadFilter| goal.items(filter).iter().map(ThreadItem::id).collect::<Vec<_>>();
    assert_eq!(
        ids(ThreadFilter::All),
        vec![1, 2, 5, 6, 7, 9],
        "updates 2–4 fold into one row; a lone update stays a step"
    );
    let ThreadItem::Updates(run) = &goal.items(ThreadFilter::All)[1] else { panic!("row 2 is not folded updates") };
    assert_eq!(run.iter().map(ThreadStep::id).collect::<Vec<_>>(), vec![2, 3, 4]);

    let r = &goal.reports["5"];
    assert_eq!(r.verdict, "done");
    assert_eq!(r.files, strings(&["x.ts"]));
    assert_eq!(r.sections.iter().map(|s| s.name.as_str()).collect::<Vec<_>>(), vec!["VERIFY"]);
}

/// The Sidebar Bands Top Goals By Whether They Need You, Work Or Are Completed
#[test]
fn the_sidebar_bands_top_goals_by_whether_they_need_you_work_or_are_completed() {
    let item = |id: &str, state: &str, project: &str, updated: &str| {
        let mut w = work_json(id, state);
        w["project"] = json!(project);
        w["updated"] = json!(updated);
        w
    };
    let goal = |w: Value, band: &str, done: i64, total: i64, needs_you: i64| {
        let mut g = board_goal(w, band);
        g["progress"] = json!({"done": done, "total": total, "superseded": 0});
        g["needs_you"] = json!(needs_you);
        g
    };
    let mut discovery = goal(item("why", "running", "demo", "2026-10-03T00:00:00Z"), "in-flight", 0, 0, 0);
    discovery["finish"] = json!({"line": "discovery", "source": "goal"});
    let mut audit = goal(item("audit", "queued", "demo", "2026-10-02T00:00:00Z"), "needs-you", 0, 4, 0);
    audit["proposed"] = json!(true);
    let mut shipped = goal(item("shipped", "done", "demo", "2026-10-10T00:00:00Z"), "at-rest", 4, 4, 0);
    shipped["finished"] = json!("Complete PR #512");
    let board: Board = serde_json::from_value(json!({
        "goals": [
            goal(item("old", "running", "demo", "2026-10-01T00:00:00Z"), "in-flight", 5, 11, 0),
            goal(item("asks", "running", "demo", "2026-10-09T00:00:00Z"), "in-flight", 1, 4, 1),
            goal(item("later", "queued", "demo", "2026-10-08T00:00:00Z"), "in-flight", 0, 0, 0),
            goal(item("push", "review", "demo", "2026-10-07T00:00:00Z"), "ready-to-push", 2, 2, 0),
            shipped,
            audit,
            discovery,
            goal(item("gone", "superseded", "demo", "2026-10-06T00:00:00Z"), "at-rest", 0, 1, 0),
            goal(item("elsewhere", "running", "other", "2026-10-11T00:00:00Z"), "in-flight", 0, 1, 0),
        ],
        "standalone": [{"work": item("fix", "running", "demo", "2026-10-05T00:00:00Z"), "band": "in-flight"}],
    }))
    .expect("a board");
    let now = kit::time::instant("2026-10-10T02:00:00Z").expect("a time");
    let ids = |rows: &[SidebarRow]| rows.iter().map(|r| r.work.id.clone()).collect::<Vec<_>>();
    let b = board.bands(Some("demo"), now);
    assert_eq!(ids(&b.needs_you), strings(&["asks", "push", "audit"]), "a question under it, or a gate for a person");
    assert!(b.needs_you.iter().all(|r| r.proposed == (r.work.id == "audit")), "only a breakdown waiting is proposed");
    assert_eq!(ids(&b.working), strings(&["later", "fix", "why", "old"]));
    assert_eq!(ids(&b.completed), strings(&["shipped"]));
    assert_eq!(ids(&b.settled), strings(&["gone"]));
    assert_eq!(board.bands(None, now).working.first().map(|r| r.work.id.as_str()), Some("elsewhere"));
    let meta = |id: &str| b.all().find(|r| r.work.id == id).map(|r| (r.meta.clone(), r.needs_you));
    assert_eq!(meta("asks"), Some(("1 of 4".to_string(), 1)));
    assert_eq!(meta("old"), Some(("5 of 11".to_string(), 0)));
    assert_eq!(meta("why"), Some(("Discovery".to_string(), 0)), "the finish line, before there are sub-goals");
    assert_eq!(meta("shipped"), Some(("Complete PR #512 · 2h".to_string(), 0)));
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
        let goals: Vec<Value> = rows.iter().map(|(id, state, band)| board_goal(work_json(id, state), band)).collect();
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

/// Work Is Renamed From Its Page Or Its Menu
#[test]
fn work_is_renamed_from_its_page_or_its_menu() {
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

/// Work Under Way Shows What It Is Doing And For How Long
#[test]
fn work_under_way_shows_what_it_is_doing_and_for_how_long() {
    let session = |status: &str, kind: &str, now: &str, at: &str| {
        json!({"now": now, "kind": kind, "at": at, "asking": null, "error": null, "status": status,
            "usage": null, "subagents": null})
    };
    let titled = |id: &str, title: &str| {
        let mut w = work_json(id, "running");
        w["title"] = json!(title);
        w
    };
    let leaf = node_json("t", "task", "running", json!({}));
    let goal = goal_view(json!({
        "goal": titled("g", "Release notes"),
        "tasks": [titled("a", "Tag releases"), titled("b", "Write the guide"), titled("c", "Check links"),
            titled("d", "Draft the note"), titled("t", "Leaf")],
        "rollup": {"counts": {}, "done": 0, "total": 5},
        "events": [], "claims": [], "landings": [], "delivery": null, "reports": {}, "asking": null,
        "activity": {
            "g": session("running", "thinking", "Weighing the plan", "2026-10-12T10:00:00Z"),
            "a": session("running", "tool", "Bash · go test ./...", "2026-10-12T10:03:00Z"),
            "b": session("running", "tool", "Read · docs/guide.md", "2026-10-12T10:02:00Z"),
            "c": session("idle", "text", "Done for now", "2026-10-12T10:04:00Z"),
            "d": session("waiting", "question", "Which tone?", "2026-10-12T10:05:00Z"),
            "t": session("running", "text", "Looking at it", "2026-10-12T09:00:00Z"),
        },
        "tree": [node_json("s", "goal", "running", json!({"children": [leaf]})), node_json("q", "goal", "queued", json!({}))],
    }));

    let quiet = goal.work_under_way(&[]);
    assert_eq!(
        quiet.rows.iter().map(|r| (r.who.as_str(), r.doing.as_str())).collect::<Vec<_>>(),
        vec![
            ("Tag releases", "Running `go test ./...`"),
            ("Write the guide", "Reading docs/guide.md"),
            ("Director", "Thinking")
        ],
        "only sessions that work, newest first, the goal's own as Director, three shown"
    );
    assert_eq!(quiet.more, 1, "the rest are counted");

    let at = |s: &str| kit::time::instant(s).expect("a time");
    let sending = InFlight { id: 7, write: Write::Send, work: "g".into(), at: at("2026-10-12T10:06:00Z") };
    let elsewhere = InFlight { id: 8, write: Write::Plan, work: "other".into(), at: at("2026-10-12T10:07:00Z") };
    let busy = goal.work_under_way(&[sending, elsewhere]);
    assert_eq!(busy.rows[0].who, "Director");
    assert_eq!(busy.rows[0].doing, "Sending your message", "a write the app started shows while it is in flight");
    assert_eq!(busy.more, 2, "a write on other work is not this goal's");
    assert_eq!(busy.rows[0].elapsed(at("2026-10-12T10:08:05Z")).as_deref(), Some("2m 5s"), "for how long, ticking");
    let deciding = InFlight { id: 9, write: Write::Decide, work: "a".into(), at: at("2026-10-12T10:09:00Z") };
    let decided = &goal.work_under_way(&[deciding]).rows[0];
    assert_eq!((decided.who.as_str(), decided.doing.as_str()), ("Director", "Deciding for you"));

    assert_eq!(doing(Some(StepKind::Thinking), Some("hmm")), "Thinking");
    assert_eq!(doing(Some(StepKind::Text), Some("Let me look")), "Thinking");
    assert_eq!(doing(Some(StepKind::Tool), Some("Edit · src/main.rs")), "Editing src/main.rs");
    assert_eq!(doing(Some(StepKind::Tool), Some("Grep · TODO")), "Searching TODO");
    assert_eq!(doing(Some(StepKind::Tool), Some("Bash")), "Running a command");
    assert_eq!(doing(Some(StepKind::Tool), Some("mcp_probe · ping")), "Using mcp_probe: ping");

    assert_eq!(Write::of(&Plan::send("g", "hi").expect("a plan")), Some((Write::Send, "g".to_string())));
    assert_eq!(Write::of(&Plan::replan("g")), Some((Write::Plan, "g".to_string())));
    assert_eq!(Write::of(&Plan::continue_goal("g", "more").expect("a plan")), Some((Write::Plan, "g".to_string())));
    assert_eq!(Write::of(&Plan::director_decides("a")), Some((Write::Decide, "a".to_string())));
    assert_eq!(Write::of(&Plan::approve("g")), Some((Write::Approve, "g".to_string())));
    assert_eq!(Write::of(&Plan::add_sub_goal("g", "x", "y").expect("a plan")), None, "a quick write shows no row");

    assert_eq!(
        goal.sub_goal_rows().iter().map(|r| (r.id.as_str(), r.working)).collect::<Vec<_>>(),
        vec![("s", true), ("q", false)],
        "a sub-goal is working while a session under it works"
    );

    let mut idle = work_json("i", "running");
    idle["kind"] = json!("goal");
    let mut busy_goal = work_json("w", "running");
    busy_goal["kind"] = json!("goal");
    let mut busy_entry = board_goal(busy_goal, "in-flight");
    busy_entry["rollup"]["counts"] = json!({"running": 1});
    let board: Board = serde_json::from_value(json!({"goals": [board_goal(idle, "in-flight"), busy_entry],
        "standalone": [{"work": work_json("x", "running"), "band": "in-flight"}]}))
    .expect("a board");
    let working: HashMap<String, bool> =
        board.bands(None, chrono::Utc::now()).all().map(|r| (r.work.id.clone(), r.working)).collect();
    assert!(working["w"], "a goal with running work under it is working in the sidebar");
    assert!(!working["i"]);
    assert!(working["x"]);

    let home = Home::new();
    let id = home.wd(&["add", "demo", "Sent to"]);
    let run = Run::new();
    let store = run.store(home.channel());
    run.wait(store.run(&Plan::send(&id, "hello").expect("a plan"))).expect("ran");
    assert!(store.in_flight().is_empty(), "a write is in flight only until wd answers it");
    store.stop();
}

/// The Agents Panel Sits Beside The Page, Never Over It
#[test]
fn the_agents_panel_sits_beside_the_page_never_over_it() {
    assert_eq!(PanelWidth::remembered(None), PanelWidth(320), "it opens at 320 until a width is set");
    assert_eq!(PanelWidth::remembered(Some("400")), PanelWidth(400), "a width set is remembered");
    assert_eq!(PanelWidth::remembered(Some("wide")), PanelWidth(320), "a width that is not one is the default");
    assert_eq!(PanelWidth::remembered(Some("90")), PanelWidth(260), "never narrower than 260");
    assert_eq!(PanelWidth::remembered(Some("2000")), PanelWidth(480), "never wider than 480");
    let w = PanelWidth(320);
    assert_eq!(w.dragged(-40.0), PanelWidth(360), "dragging its edge toward the page widens it");
    assert_eq!(w.dragged(30.0), PanelWidth(290), "dragging its edge away narrows it");
    assert_eq!(w.dragged(-900.0), PanelWidth(480));
    assert_eq!(w.dragged(900.0), PanelWidth(260));
    assert_eq!(PanelWidth::remembered(Some(&PanelWidth(400).setting())), PanelWidth(400));
    assert!(!PanelWidth::over_page(PanelWidth::NARROW + 1.0), "wherever the goals sidebar shows, it sits beside");
    assert!(!PanelWidth::over_page(1280.0));
    assert!(PanelWidth::over_page(PanelWidth::NARROW), "where the goals sidebar folds away, it opens over the page");
    assert!(PanelWidth::over_page(400.0));
}
