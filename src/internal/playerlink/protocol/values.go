package protocol

type Values []any

func (v Values) Int(index int) (int, error) {
	if index < 0 || index >= len(v) {
		return 0, protocolError("reply has no value at position %d", index)
	}
	switch typed := v[index].(type) {
	case uint8:
		return int(typed), nil
	case uint16:
		return int(typed), nil
	case int32:
		return int(typed), nil
	case uint32:
		return int(typed), nil
	}
	return 0, protocolError("value at position %d is %T, not a number", index, v[index])
}

func (v Values) Bytes(index int) ([]byte, error) {
	if index < 0 || index >= len(v) {
		return nil, protocolError("reply has no value at position %d", index)
	}
	raw, ok := v[index].([]byte)
	if !ok {
		return nil, protocolError("value at position %d is %T, not bytes", index, v[index])
	}
	return raw, nil
}

func (v Values) Text(index int) (string, error) {
	raw, err := v.Bytes(index)
	return string(raw), err
}

func (v Values) Rows(index int) ([]Values, error) {
	if index < 0 || index >= len(v) {
		return nil, protocolError("reply has no value at position %d", index)
	}
	rows, ok := v[index].([][]any)
	if !ok {
		return nil, protocolError("value at position %d is %T, not a struct array", index, v[index])
	}
	converted := make([]Values, len(rows))
	for position, row := range rows {
		converted[position] = Values(row)
	}
	return converted, nil
}
