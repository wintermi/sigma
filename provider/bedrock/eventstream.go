// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

type eventStreamFrame struct {
	MessageType   string
	EventType     string
	ContentType   string
	ExceptionType string
	Payload       []byte
}

type eventStreamDecoder struct {
	r io.Reader
}

func newEventStreamDecoder(r io.Reader) *eventStreamDecoder {
	return &eventStreamDecoder{r: r}
}

func (d *eventStreamDecoder) Next() (*eventStreamFrame, error) {
	var prelude [12]byte
	if _, err := io.ReadFull(d.r, prelude[:]); err != nil {
		return nil, fmt.Errorf("eventstream: reading prelude: %w", err)
	}

	totalLen := binary.BigEndian.Uint32(prelude[0:4])
	headersLen := binary.BigEndian.Uint32(prelude[4:8])
	preludeCRC := binary.BigEndian.Uint32(prelude[8:12])
	if got := crc32.ChecksumIEEE(prelude[0:8]); got != preludeCRC {
		return nil, fmt.Errorf("eventstream: prelude CRC mismatch: got %08x, want %08x", got, preludeCRC)
	}

	remaining := int(totalLen) - 12 - 4
	const maxFrameSize = 10 * 1024 * 1024
	if remaining < 0 || remaining > maxFrameSize {
		return nil, fmt.Errorf("eventstream: invalid total length %d", totalLen)
	}
	if int(headersLen) > remaining {
		return nil, fmt.Errorf("eventstream: headers length %d exceeds frame payload %d", headersLen, remaining)
	}

	buf := make([]byte, remaining+4)
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return nil, fmt.Errorf("eventstream: reading frame body: %w", err)
	}
	messageCRC := binary.BigEndian.Uint32(buf[remaining:])
	crc := crc32.NewIEEE()
	_, _ = crc.Write(prelude[:])
	_, _ = crc.Write(buf[:remaining])
	if crc.Sum32() != messageCRC {
		return nil, fmt.Errorf("eventstream: message CRC mismatch")
	}

	frame := &eventStreamFrame{Payload: buf[headersLen:remaining]}
	if err := parseEventStreamHeaders(buf[:headersLen], frame); err != nil {
		return nil, err
	}
	return frame, nil
}

func parseEventStreamHeaders(headers []byte, frame *eventStreamFrame) error {
	for off := 0; off < len(headers); {
		if off >= len(headers) {
			return nil
		}
		nameLen := int(headers[off])
		off++
		if off+nameLen > len(headers) {
			return fmt.Errorf("eventstream: header name overflow")
		}
		name := string(headers[off : off+nameLen])
		off += nameLen
		if off >= len(headers) {
			return fmt.Errorf("eventstream: missing header type tag")
		}
		typeTag := headers[off]
		off++

		switch typeTag {
		case 7:
			if off+2 > len(headers) {
				return fmt.Errorf("eventstream: string header value length overflow")
			}
			valueLen := int(binary.BigEndian.Uint16(headers[off : off+2]))
			off += 2
			if off+valueLen > len(headers) {
				return fmt.Errorf("eventstream: string header value overflow")
			}
			value := string(headers[off : off+valueLen])
			off += valueLen
			switch name {
			case ":message-type":
				frame.MessageType = value
			case ":event-type":
				frame.EventType = value
			case ":content-type":
				frame.ContentType = value
			case ":exception-type":
				frame.ExceptionType = value
			}
		case 0, 1:
		case 2:
			off++
		case 3:
			off += 2
		case 4:
			off += 4
		case 5, 8:
			off += 8
		case 9:
			off += 16
		case 6:
			if off+2 > len(headers) {
				return fmt.Errorf("eventstream: bytes header length overflow")
			}
			valueLen := int(binary.BigEndian.Uint16(headers[off : off+2]))
			if off+2+valueLen > len(headers) {
				return fmt.Errorf("eventstream: bytes header value overflows header block")
			}
			off += 2 + valueLen
		default:
			return fmt.Errorf("eventstream: unknown header type tag %d", typeTag)
		}
		if off > len(headers) {
			return fmt.Errorf("eventstream: header overflow")
		}
	}
	return nil
}
