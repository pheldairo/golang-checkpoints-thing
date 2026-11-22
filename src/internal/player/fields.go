package player

import "setlist/internal/playerlink/protocol"

type fieldReader struct {
	values protocol.Values
	err    error
}

func (r *fieldReader) text(index int) string {
	if r.err != nil {
		return ""
	}
	value, err := r.values.Text(index)
	r.err = err
	return value
}

func (r *fieldReader) number(index int) int {
	if r.err != nil {
		return 0
	}
	value, err := r.values.Int(index)
	r.err = err
	return value
}
