# Goal tree design references

The approved screens for the goal tree, exported from the
[Goal Tree Design canvas](https://claude.ai/artifact/9CXtntt3b7zH2D21CFoZ45). The macOS and
Linux apps match these; where they differ from `docs/design/desktop.md`, these win.

Each `<name>.html` is a static copy of one artboard; `<name>.png` is it rendered at 1280×900,
the reference a UI check compares an app screenshot against. Re-render after editing:

```sh
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --hide-scrollbars \
  --force-device-scale-factor=1 --window-size=1280,900 --screenshot=$PWD/main.png file://$PWD/main.html
```

| File | Screen |
|---|---|
| `main` | A goal and its sub-goals (Overview) |
| `subgoal` | A sub-goal, one level down |
| `proposal` | A proposed breakdown, ask mode |
| `pivot` | A course change on the top goal |
| `finishline` | Where does this goal finish? |
| `conversation` | Conversation tab |
| `record` | Record tab |
