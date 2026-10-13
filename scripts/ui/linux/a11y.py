"""Drive the app through its accessibility tree: find widgets by role and name,
press them, type into them, and dump what is on screen."""
import sys, time, subprocess
import gi
gi.require_version("Atspi", "2.0")
from gi.repository import Atspi

def app():
    desk = Atspi.get_desktop(0)
    for i in range(desk.get_child_count()):
        a = desk.get_child_at_index(i)
        if a and "work" in (a.get_name() or "").lower():
            return a
    raise SystemExit("app not on the accessibility bus: " + ", ".join(desk.get_child_at_index(i).get_name() or "?" for i in range(desk.get_child_count())))

def walk(node, depth=0, out=None, limit=4000):
    out = [] if out is None else out
    if node is None or len(out) > limit: return out
    try:
        role = node.get_role_name(); name = node.get_name() or ""
        states = node.get_state_set()
        if states.contains(Atspi.StateType.SHOWING) or depth < 3:
            out.append((depth, role, name, node))
            for i in range(node.get_child_count()):
                walk(node.get_child_at_index(i), depth + 1, out, limit)
    except Exception as e:
        out.append((depth, "error", str(e), None))
    return out

def dump(filter_roles=None):
    for d, role, name, _ in walk(app()):
        if filter_roles and role not in filter_roles: continue
        if name or role in ("push button", "toggle button", "text", "radio button", "check box", "label"):
            print("  " * d + f"[{role}] {name[:110]!r}")

def find(role, name, exact=False, nth=0):
    hits = [n for d, r, nm, n in walk(app()) if r == role and (nm == name if exact else name.lower() in (nm or "").lower())]
    if len(hits) <= nth:
        raise SystemExit(f"no {role} named {name!r}")
    return hits[nth]

def press(role, name, exact=False, nth=0):
    n = find(role, name, exact, nth)
    act = n.get_action_iface()
    if act and act.get_n_actions() > 0:
        act.do_action(0)
    else:
        click(n)
    time.sleep(0.8)

def click(n, double=False):
    c = n.get_component_iface()
    ext = c.get_extents(Atspi.CoordType.SCREEN)
    x, y = ext.x + ext.width // 2, ext.y + ext.height // 2
    subprocess.run(["xdotool", "mousemove", str(x), str(y), "click", "--repeat", "2" if double else "1", "1"], check=True)
    time.sleep(0.8)

def type_text(text):
    subprocess.run(["xdotool", "type", "--delay", "15", text], check=True)

def key(*keys):
    subprocess.run(["xdotool", "key", *keys], check=True)
    time.sleep(0.6)

def shot(name):
    subprocess.run(["import", "-window", "root", f"/shots/{name}.png"], check=True)

if __name__ == "__main__":
    dump()

def scrollbars():
    out = []
    for d, r, nm, n in walk(app()):
        if r == "scroll bar":
            v = n.get_value_iface()
            if v: out.append((round(v.get_current_value()), round(v.get_maximum_value())))
    return out
