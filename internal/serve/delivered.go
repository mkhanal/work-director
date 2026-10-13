package serve

import (
	"strconv"
	"sync"
	"time"

	"wd/internal/core"
	"wd/internal/delivery"
)

// deliveryFresh is how long a goal's delivery, once read, answers without
// being read again. Delivery is read from git and GitHub, which take tens to
// hundreds of milliseconds and say nothing when they change.
const deliveryFresh = 2 * time.Second

// deliveries is each goal's delivery as last read, by goal id. A read whose
// inputs from the ledger are unchanged is answered with the one held, and one
// past deliveryFresh is read again in the background for the next read: a
// person opening a goal never waits on git or GitHub twice.
type deliveries struct {
	mu sync.Mutex
	m  map[string]*delivered
}

type delivered struct {
	// key is what the read depended on in the ledger: the directory, the
	// goal's state and the latest landing.
	key     string
	at      time.Time
	state   delivery.State
	reading bool
}

// deliveryKey is what StatusFor reads from the ledger, as one string.
func deliveryKey(dir string, events []core.Event, goal core.Work) string {
	landing := 0
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == core.EventPr {
			landing = events[i].ID
			break
		}
	}
	return dir + "\x00" + string(goal.State) + "\x00" + strconv.Itoa(landing)
}

// of is goal's delivery read in dir.
func (d *deliveries) of(dir string, events []core.Event, goal core.Work, tasks []core.Work) delivery.State {
	key := deliveryKey(dir, events, goal)
	d.mu.Lock()
	if d.m == nil {
		d.m = map[string]*delivered{}
	}
	held, ok := d.m[goal.ID]
	if ok && held.key == key {
		state := held.state
		if time.Since(held.at) >= deliveryFresh && !held.reading {
			held.reading = true
			go d.read(dir, events, goal, tasks, key)
		}
		d.mu.Unlock()
		return state
	}
	d.mu.Unlock()
	return d.read(dir, events, goal, tasks, key)
}

// read reads goal's delivery now and holds it under key.
func (d *deliveries) read(dir string, events []core.Event, goal core.Work, tasks []core.Work, key string) delivery.State {
	state := delivery.StatusFor(dir, events, goal, tasks)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.m[goal.ID] = &delivered{key: key, at: time.Now(), state: state}
	return state
}
