package player

import (
	"fmt"

	"setlist/internal/hibylink/protocol"
	"setlist/internal/hibylink/rpc"
)

func (d device) cover() ([]byte, error) {
	reply, err := d.caller.Call(rpc.GetCover)
	if err != nil {
		return nil, err
	}
	if reply.Result() < 0 {
		return nil, nil
	}
	image, err := reply.Values.Bytes(1)
	if err != nil {
		return nil, err
	}
	if reply.Result() != len(image) {
		return nil, fmt.Errorf("%w: cover length %d differs from %d image bytes", protocol.ErrProtocol, reply.Result(), len(image))
	}
	return image, nil
}

func (c *Controller) Cover() ([]byte, error) {
	var image []byte
	err := c.withDevice(func(d device) error {
		var err error
		image, err = d.cover()
		return err
	})
	return image, err
}
