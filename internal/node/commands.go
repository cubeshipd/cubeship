package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// The half of the conversation that goes the other way.
//
// A worker dials the control plane and nothing dials a worker — that is
// the decision the whole cluster is built on, and its cost is that this
// instance cannot ask a machine anything. A log lives on the box its
// container is on; so does an exec, and so do a container's own
// counters. Refusing all of that forever would be paying the cost twice.
//
// So the answer to a machine's own poll carries **commands**, and the
// request is what waits: somebody asks for a remote app's log, the
// control plane parks that request, wakes the machine's parked poll,
// and the machine's answer releases the first one. Nothing is pushed,
// nothing listens on the worker, and the round trip is one poll rather
// than one interval.
//
// **It is all in memory.** A command is a request in flight: it means
// nothing once the person who asked has gone, and a row for one would
// be a row to clean up. A daemon restart drops what was in flight, which
// is exactly what a restart should do to a question nobody is waiting
// on any more.

// PollWait is how long a machine's poll is held open when there is
// nothing to say.
//
// Long enough that a cluster of ten costs a request every couple of
// seconds rather than one a second, and short enough to sit well inside
// every proxy's idle timeout — Traefik's is three minutes, and a
// connection parked past one is a connection something in the middle
// closes without either end learning why.
const PollWait = 25 * time.Second

// CommandTimeout is how long whoever asked waits for a machine's
// answer.
//
// It bounds the *asking*, not the doing: a machine that is off never
// answers, and the person who wanted a log would otherwise wait on a
// screen forever. Comfortably longer than a poll is held, so a command
// that arrives just after a machine parked still catches that poll
// rather than timing out against it.
const CommandTimeout = PollWait + 15*time.Second

// MaxAnswerBytes bounds what a machine may send back for one command.
//
// A log is capped by the tail it was asked for, and this is the second
// wall: a machine is a credential somebody holds, and a body with no
// limit on it is a way to make this daemon's memory somebody else's
// decision. Two megabytes is comfortably more than the largest tail the
// dashboard asks for.
const MaxAnswerBytes = 2 << 20

// Kinds of command. One so far, and the shape is what matters: adding
// exec, or a container's counters, is another value here and another
// case in the agent.
const (
	// CommandLogs asks for the tail of a container's log.
	CommandLogs = "logs"

	// CommandUpdate tells a machine to replace itself with a version.
	//
	// **Nothing comes back from it.** A machine replacing itself stops
	// answering — the process holding the poll open is the one being
	// stopped — so the answer is the version it reports on the pass
	// after it returns, which is a thing it already sends every ten
	// seconds. Waiting for a reply here would be waiting for a process
	// that is about to be killed on purpose.
	CommandUpdate = "update"
)

// Command is one thing the control plane wants a machine to do.
type Command struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Container is what the command is about, by id — the one the
	// machine reported when it started it.
	Container string `json:"container,omitempty"`
	// Tail is how many lines of log to read, in Docker's own spelling:
	// a number, or "all".
	Tail string `json:"tail,omitempty"`
	// Version is the release to replace this machine with. Only an
	// update carries one.
	Version string `json:"version,omitempty"`
}

// ErrNoAnswer is a machine that did not come back in time. It says
// nothing about why — the box may be off, its daemon may be down, or
// the network may be — which is the same thing `unreachable` says about
// a machine that stopped calling.
var ErrNoAnswer = errors.New("the server did not answer")

// answer is what comes back from a machine, as bytes rather than a
// string: a log is bytes, and turning one into JSON would replace
// whatever in it is not valid UTF-8 with a question mark.
type answer struct {
	output []byte
	err    error
}

// hub holds what is in flight.
type hub struct {
	mu sync.Mutex
	// wake is how a parked poll is released, one channel per machine,
	// buffered so a signal with nobody parked is not lost — the next
	// poll finds it and returns at once.
	wake map[int64]chan struct{}
	// queued is what a machine will be handed on its next poll.
	queued map[int64][]Command
	// waiting is where each answer goes, by command id, with the
	// machine it was sent to: a machine may only answer its own.
	waiting map[string]pending
}

type pending struct {
	node int64
	back chan answer
}

func newHub() *hub {
	return &hub{
		wake:    map[int64]chan struct{}{},
		queued:  map[int64][]Command{},
		waiting: map[string]pending{},
	}
}

// Signal releases a machine's parked poll, or leaves a mark for its
// next one.
//
// Called whenever there is something new for a machine to hear: a
// command, or a deploy placed on it. Without it the machine finds out on
// its next poll, which is up to PollWait later — correct, and slow
// enough that somebody would think a deploy had not registered.
func (h *hub) Signal(nodeID int64) {
	h.mu.Lock()
	ch, ok := h.wake[nodeID]
	if !ok {
		ch = make(chan struct{}, 1)
		h.wake[nodeID] = ch
	}
	h.mu.Unlock()

	select {
	case ch <- struct{}{}:
	default:
		// Already marked. One wake is all a poll needs, and it will
		// find everything queued when it runs.
	}
}

// Park holds a machine's poll open until there is something to say.
// It reports whether something arrived; either way the caller answers,
// because a poll that returns with nothing is how a machine's next one
// starts.
func (h *hub) Park(ctx context.Context, nodeID int64) bool {
	h.mu.Lock()
	ch, ok := h.wake[nodeID]
	if !ok {
		ch = make(chan struct{}, 1)
		h.wake[nodeID] = ch
	}
	h.mu.Unlock()

	select {
	case <-ch:
		return true
	case <-time.After(PollWait):
		return false
	case <-ctx.Done():
		return false
	}
}

// Take hands over everything queued for a machine and forgets it. What
// is handed over is on its way: a command the machine never answers
// times out on the asking side rather than being retried, because a
// second copy of "read this log" is a second log nobody asked for.
func (h *hub) Take(nodeID int64) []Command {
	h.mu.Lock()
	defer h.mu.Unlock()
	queued := h.queued[nodeID]
	delete(h.queued, nodeID)
	return queued
}

// Ask sends one command to a machine and waits for what it says.
// Tell queues a command and does not wait for an answer.
//
// One command needs this and it is the reason it exists: a machine told
// to replace itself stops answering, because the process holding the
// poll open is the one being stopped. Ask would wait for a reply that
// is never coming and time out on a success.
func (h *hub) Tell(nodeID int64, cmd Command) error {
	id, err := commandID()
	if err != nil {
		return err
	}
	cmd.ID = id

	h.mu.Lock()
	h.queued[nodeID] = append(h.queued[nodeID], cmd)
	h.mu.Unlock()

	// Woken rather than left for the next pass: an update somebody is
	// watching should start now, and the machine is parked on a request
	// this releases.
	h.Signal(nodeID)
	return nil
}

func (h *hub) Ask(ctx context.Context, nodeID int64, cmd Command) ([]byte, error) {
	id, err := commandID()
	if err != nil {
		return nil, err
	}
	cmd.ID = id
	back := make(chan answer, 1)

	h.mu.Lock()
	h.waiting[id] = pending{node: nodeID, back: back}
	h.queued[nodeID] = append(h.queued[nodeID], cmd)
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.waiting, id)
		h.mu.Unlock()
	}()

	h.Signal(nodeID)

	select {
	case a := <-back:
		return a.output, a.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(CommandTimeout):
		return nil, ErrNoAnswer
	}
}

// Answer releases whoever asked. A command id nobody is waiting for is
// dropped without complaint: the asker gave up, or this daemon has been
// restarted since, and a machine that answers late is not doing anything
// wrong.
func (h *hub) Answer(nodeID int64, id string, output []byte, failed error) {
	h.mu.Lock()
	p, ok := h.waiting[id]
	if ok {
		delete(h.waiting, id)
	}
	h.mu.Unlock()

	// A machine may only answer what was sent to it. Without this a
	// worker's credential would be a way to feed somebody else's screen
	// whatever it liked.
	if !ok || p.node != nodeID {
		return
	}
	select {
	case p.back <- answer{output: output, err: failed}:
	default:
	}
}

func commandID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
