package rpc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"setlist/internal/playerlink/protocol"
)

var ErrPoisoned = errors.New("player connection is unusable after a failed exchange")

const (
	firstSerial    = 11
	maxSerial      = 0xffff
	readBufferSize = 64 * 1024
)

type Reply struct {
	Signature string
	Values    protocol.Values
}

func (r Reply) Result() int {
	result, err := r.Values.Int(0)
	if err != nil {
		return -1
	}
	return result
}

type Client struct {
	conn                 net.Conn
	timeout              time.Duration
	serial               uint16
	group                uint32
	stream               protocol.FrameStream
	reassembler          *protocol.Reassembler
	replies              []protocol.Message
	droppedNotifications int
	failure              error
}

func NewClient(conn net.Conn, timeout time.Duration) *Client {
	return &Client{
		conn:        conn,
		timeout:     timeout,
		serial:      firstSerial,
		group:       firstSerial,
		reassembler: protocol.NewReassembler(),
	}
}

func (c *Client) Failure() error {
	return c.failure
}

func (c *Client) DroppedNotifications() int {
	return c.droppedNotifications
}

func (c *Client) Call(op Operation, args ...any) (Reply, error) {
	if op.Validate != nil {
		if err := op.Validate(args); err != nil {
			return Reply{}, fmt.Errorf("%s: %w", op.Name, err)
		}
	} else if len(args) != 0 {
		return Reply{}, fmt.Errorf("%s: %w: it takes no arguments", op.Name, ErrArgumentRefused)
	}
	if c.failure != nil {
		return Reply{}, fmt.Errorf("%w: %v", ErrPoisoned, c.failure)
	}
	call, err := protocol.NewCall(op.Service, op.Action, c.serial, op.Request, args)
	if err != nil {
		return Reply{}, err
	}
	wire, err := protocol.FrameMessage(call, c.group)
	if err != nil {
		return Reply{}, err
	}
	reply, err := c.exchange(op, wire)
	if err != nil {
		c.failure = err
		return Reply{}, fmt.Errorf("%s: %w", op.Name, err)
	}
	c.advanceCounters()
	return reply, nil
}

func (c *Client) exchange(op Operation, wire []byte) (Reply, error) {
	if err := c.conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return Reply{}, err
	}
	if _, err := c.conn.Write(wire); err != nil {
		return Reply{}, err
	}
	message, err := c.nextReply()
	if err != nil {
		return Reply{}, err
	}
	return c.checkReply(op, message)
}

func (c *Client) checkReply(op Operation, message protocol.Message) (Reply, error) {
	if message.Kind != protocol.KindReply || message.Serial != c.serial {
		return Reply{}, fmt.Errorf("%w: reply kind %d serial %d does not answer serial %d",
			protocol.ErrProtocol, message.Kind, message.Serial, c.serial)
	}
	values, err := message.Values()
	if err != nil {
		return Reply{}, err
	}
	if !strings.HasPrefix(message.Signature, "i") || len(values) == 0 {
		return Reply{}, fmt.Errorf("%w: reply has no result code", protocol.ErrProtocol)
	}
	reply := Reply{Signature: message.Signature, Values: values}
	if reply.Result() < 0 {
		if message.Signature != "i" {
			return Reply{}, fmt.Errorf("%w: negative result carries a payload", protocol.ErrProtocol)
		}
		return reply, nil
	}
	if !op.acceptsReply(message.Signature) {
		return Reply{}, fmt.Errorf("%w: unexpected reply signature %q", protocol.ErrProtocol, message.Signature)
	}
	return reply, nil
}

func (c *Client) nextReply() (protocol.Message, error) {
	buffer := make([]byte, readBufferSize)
	for len(c.replies) == 0 {
		count, err := c.conn.Read(buffer)
		if count > 0 {
			if feedErr := c.feed(buffer[:count]); feedErr != nil {
				return protocol.Message{}, feedErr
			}
		}
		if errors.Is(err, io.EOF) {
			return protocol.Message{}, c.endOfStream()
		}
		if err != nil {
			return protocol.Message{}, err
		}
	}
	reply := c.replies[0]
	c.replies = c.replies[1:]
	return reply, nil
}

func (c *Client) feed(data []byte) error {
	frames, err := c.stream.Feed(data)
	if err != nil {
		return err
	}
	for _, frame := range frames {
		message, err := c.reassembler.Feed(frame)
		if err != nil {
			return err
		}
		if message == nil {
			continue
		}
		if message.Kind == protocol.KindNotification {
			c.droppedNotifications++
			continue
		}
		c.replies = append(c.replies, *message)
	}
	return nil
}

func (c *Client) endOfStream() error {
	if err := c.stream.Finish(); err != nil {
		return err
	}
	if err := c.reassembler.Finish(); err != nil {
		return err
	}
	return fmt.Errorf("player closed the connection: %w", io.EOF)
}

func (c *Client) advanceCounters() {
	c.serial = c.serial%maxSerial + 1
	c.group++
}
