package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Serve runs a session: p on this end, somebody's terminal on the other
// end of conn. It returns once the session is over, and it closes both.
//
// Whichever side ends first ends the session. The program exiting is
// told to the client with its status; the client going away closes the
// program, which is what closing a terminal window has always meant.
func Serve(ctx context.Context, conn *websocket.Conn, p Process, target string) Outcome {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer p.Close()
	conn.SetReadLimit(MaxFrame)

	if err := WriteMessage(ctx, conn, Message{Type: TypeReady, Target: target}); err != nil {
		conn.CloseNow()
		return Outcome{Reason: ReasonClosed}
	}

	activity := make(chan struct{}, 1)
	mark := func() {
		select {
		case activity <- struct{}{}:
		default:
		}
	}

	exited := make(chan Outcome, 1)
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					exited <- Outcome{Reason: ReasonClosed}
					return
				}
				mark()
			}
			if err != nil {
				break
			}
		}
		// The output ending is the program ending — the Engine closes
		// the stream when the process goes — but its status is a
		// separate question, and asked with a deadline of its own.
		wait, stop := context.WithTimeout(context.WithoutCancel(ctx), WaitTimeout)
		defer stop()
		code, err := p.Wait(wait)
		if err != nil {
			exited <- Outcome{Reason: ReasonFailed, Err: err}
			return
		}
		exited <- Outcome{Reason: ReasonExited, Code: code}
	}()

	left := make(chan Outcome, 1)
	go func() {
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				left <- Outcome{Reason: ReasonClosed}
				return
			}
			mark()
			switch typ {
			case websocket.MessageBinary:
				if _, err := p.Write(data); err != nil {
					// The program stopped reading because it is
					// ending. The reader above reports how.
					continue
				}
			case websocket.MessageText:
				var m Message
				if json.Unmarshal(data, &m) != nil || m.Type != TypeResize {
					continue
				}
				if s := (Size{Cols: m.Cols, Rows: m.Rows}); s.Valid() {
					_ = p.Resize(ctx, s.Cols, s.Rows)
				}
			}
		}
	}()

	idle := time.NewTimer(IdleTimeout)
	defer idle.Stop()
	ping := time.NewTicker(PingInterval)
	defer ping.Stop()

	for {
		select {
		case out := <-exited:
			if out.Reason == ReasonClosed {
				conn.CloseNow()
				return out
			}
			msg := Message{Type: TypeExit, Code: &out.Code}
			if out.Err != nil {
				msg = Message{Type: TypeError, Message: out.Err.Error()}
			}
			_ = WriteMessage(ctx, conn, msg)
			conn.Close(websocket.StatusNormalClosure, "")
			return out
		case out := <-left:
			conn.CloseNow()
			return out
		case <-activity:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(IdleTimeout)
		case <-idle.C:
			_ = WriteMessage(ctx, conn, Message{Type: TypeError, Message: ErrIdle.Error()})
			conn.Close(websocket.StatusNormalClosure, "idle")
			return Outcome{Reason: ReasonIdle}
		case <-ping.C:
			go func() {
				pctx, stop := context.WithTimeout(ctx, PingInterval)
				defer stop()
				if conn.Ping(pctx) != nil {
					conn.CloseNow()
				}
			}()
		case <-ctx.Done():
			_ = WriteMessage(context.Background(), conn, Message{Type: TypeError, Message: "the daemon is shutting down"})
			conn.CloseNow()
			return Outcome{Reason: ReasonStopped}
		}
	}
}

// Relay joins two sessions' connections and copies every frame between
// them unchanged, until either ends. The control plane stands between a
// browser and a worker this way: the worker runs Serve, and nothing here
// has to understand what it is carrying.
//
// It reads the frames going towards the client for the one thing it
// wants to know, which is how the session ended, so the audit log can
// say so without the worker having to tell it a second way.
func Relay(ctx context.Context, client, program *websocket.Conn) Outcome {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client.SetReadLimit(MaxFrame)
	program.SetReadLimit(MaxFrame)

	// said is how the program said the session ended, once it has. The
	// client hangs up the moment it reads that, and its goroutine can
	// report the hang-up before this one reports the goodbye — so the
	// goodbye is kept here rather than raced.
	var mu sync.Mutex
	said := Outcome{Reason: ReasonClosed}
	ended := make(chan struct{}, 2)
	go func() {
		defer func() { ended <- struct{}{} }()
		for {
			typ, data, err := program.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageText {
				var m Message
				if json.Unmarshal(data, &m) == nil {
					mu.Lock()
					switch m.Type {
					case TypeExit:
						said = Outcome{Reason: ReasonExited}
						if m.Code != nil {
							said.Code = *m.Code
						}
					case TypeError:
						said = Outcome{Reason: ReasonFailed, Err: errors.New(m.Message)}
						if m.Message == ErrIdle.Error() {
							said = Outcome{Reason: ReasonIdle}
						}
					}
					mu.Unlock()
				}
			}
			if client.Write(ctx, typ, data) != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { ended <- struct{}{} }()
		for {
			typ, data, err := client.Read(ctx)
			if err != nil {
				return
			}
			if program.Write(ctx, typ, data) != nil {
				return
			}
		}
	}()

	// The worker pings its own end. The browser's end is this daemon's
	// to keep alive, because a browser never pings.
	ping := time.NewTicker(PingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ended:
			mu.Lock()
			out := said
			mu.Unlock()
			// Closed with a handshake when the program said goodbye,
			// so the client reads the exit frame before the socket
			// goes; torn down otherwise.
			if out.Reason == ReasonClosed {
				client.CloseNow()
				program.CloseNow()
			} else {
				client.Close(websocket.StatusNormalClosure, "")
				program.Close(websocket.StatusNormalClosure, "")
			}
			return out
		case <-ping.C:
			go func() {
				pctx, stop := context.WithTimeout(ctx, PingInterval)
				defer stop()
				if client.Ping(pctx) != nil {
					client.CloseNow()
				}
			}()
		case <-ctx.Done():
			_ = WriteMessage(context.Background(), client, Message{Type: TypeError, Message: "the daemon is shutting down"})
			client.CloseNow()
			program.CloseNow()
			return Outcome{Reason: ReasonStopped}
		}
	}
}

// Attach is the client's half: it puts what the program prints on out
// and sends what is read from in, until the session ends.
//
// ready is called once the program is running, with where it runs, so a
// caller can say where it landed. Nil is fine.
func Attach(ctx context.Context, conn *websocket.Conn, in io.Reader, out io.Writer, resizes <-chan Size, ready func(target string)) Outcome {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer conn.CloseNow()
	conn.SetReadLimit(MaxFrame)

	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				if conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		for {
			select {
			case s, ok := <-resizes:
				if !ok {
					return
				}
				if s.Valid() {
					_ = WriteMessage(ctx, conn, Message{Type: TypeResize, Cols: s.Cols, Rows: s.Rows})
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return Outcome{Reason: ReasonStopped}
			}
			return Outcome{Reason: ReasonClosed, Err: errors.New("the connection to the instance closed")}
		}
		if typ == websocket.MessageBinary {
			if _, err := out.Write(data); err != nil {
				return Outcome{Reason: ReasonClosed, Err: err}
			}
			continue
		}
		var m Message
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case TypeReady:
			if ready != nil {
				ready(m.Target)
			}
		case TypeExit:
			code := 0
			if m.Code != nil {
				code = *m.Code
			}
			conn.Close(websocket.StatusNormalClosure, "")
			return Outcome{Reason: ReasonExited, Code: code}
		case TypeError:
			return Outcome{Reason: ReasonFailed, Err: errors.New(m.Message)}
		}
	}
}

// WriteMessage sends one text frame.
func WriteMessage(ctx context.Context, conn *websocket.Conn, m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, b)
}

// ReadMessage waits for one text frame, for a session that has to hear
// something before it starts.
func ReadMessage(ctx context.Context, conn *websocket.Conn) (Message, error) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return Message{}, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var m Message
		if err := json.Unmarshal(data, &m); err != nil {
			return Message{}, fmt.Errorf("read a message: %w", err)
		}
		return m, nil
	}
}

// Fail tells the client why its session will not start, and closes.
// Used before a program exists — after one does, Serve says it.
func Fail(ctx context.Context, conn *websocket.Conn, err error) {
	_ = WriteMessage(ctx, conn, Message{Type: TypeError, Message: err.Error()})
	conn.Close(websocket.StatusNormalClosure, "")
}
