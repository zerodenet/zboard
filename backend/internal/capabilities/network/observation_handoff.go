package network

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

// ObservationSource is an immutable receiver-side source, not proof of the
// installed runtime. Payload and routing identities remain byte-for-byte.
// LegacyFloor is imported only by the owned migration, never by live traffic.
type ObservationSource struct {
	Observation                NodeObservation
	Kind, PrincipalKey, FlowID string
	Payload                    []byte
	LegacyFloor                bool
}

// WriteObservationSource avoids building one JSON buffer for a compacted record
// or a wire-size routing identity. Each variable field is length delimited.
func WriteObservationSource(w io.Writer, s ObservationSource) error {
	var err error
	number := func(n uint64) {
		if err == nil {
			err = binary.Write(w, binary.BigEndian, n)
		}
	}
	bytes := func(b []byte) {
		number(uint64(len(b)))
		if err == nil {
			_, err = w.Write(b)
		}
	}
	str := func(v string) { bytes([]byte(v)) }
	position := func(p EventPosition) {
		str(p.AssetTag)
		str(p.ID)
		str(p.CoreInstanceID)
		number(p.Sequence)
		number(p.ConfigRevision)
		str(p.OccurredAt.UTC().Format(time.RFC3339Nano))
		str(p.SourceID)
		str(p.ReceivedAt.UTC().Format(time.RFC3339Nano))
	}
	number(1)
	number(uint64(s.Observation.NodeID))
	position(s.Observation.Latest)
	if s.Observation.StatsEvent != nil {
		number(1)
		position(*s.Observation.StatsEvent)
	} else {
		number(0)
	}
	number(s.Observation.Stats.ActiveSessions)
	number(s.Observation.Stats.BytesUp)
	number(s.Observation.Stats.BytesDown)
	str(s.Kind)
	str(s.PrincipalKey)
	str(s.FlowID)
	bytes(s.Payload)
	if s.LegacyFloor {
		number(1)
	} else {
		number(0)
	}
	return err
}

// ReadObservationSource is used only after the store verifies actual part sizes
// and the content hash. A length in corrupt material cannot allocate past limit.
func ReadObservationSource(r io.Reader, limit uint64) (s ObservationSource, err error) {
	remaining := limit
	number := func() (n uint64) {
		if err == nil {
			if remaining < 8 {
				err = io.ErrUnexpectedEOF
				return
			}
			remaining -= 8
			err = binary.Read(r, binary.BigEndian, &n)
		}
		return
	}
	bytes := func() []byte {
		n := number()
		if err != nil {
			return nil
		}
		if n > remaining {
			err = errors.New("observation field exceeds material budget")
			return nil
		}
		remaining -= n
		// The wire format stores length and bytes, not slice allocation state.
		// Use nil as the canonical representation of an empty payload.
		if n == 0 {
			return nil
		}
		b := make([]byte, int(n))
		_, err = io.ReadFull(r, b)
		return b
	}
	str := func() string { return string(bytes()) }
	position := func() (p EventPosition) {
		p.AssetTag = str()
		p.ID = str()
		p.CoreInstanceID = str()
		p.Sequence = number()
		p.ConfigRevision = number()
		v := str()
		if err == nil {
			p.OccurredAt, err = time.Parse(time.RFC3339Nano, v)
		}
		p.SourceID = str()
		v = str()
		if err == nil {
			p.ReceivedAt, err = time.Parse(time.RFC3339Nano, v)
		}
		return
	}
	if number() != 1 && err == nil {
		err = errors.New("unsupported observation material version")
	}
	s.Observation.NodeID = uint(number())
	s.Observation.Latest = position()
	flag := number()
	if flag == 1 {
		p := position()
		s.Observation.StatsEvent = &p
	} else if flag != 0 && err == nil {
		err = errors.New("invalid stats presence")
	}
	s.Observation.Stats = NodeStats{ActiveSessions: number(), BytesUp: number(), BytesDown: number()}
	s.Kind = str()
	s.PrincipalKey = str()
	s.FlowID = str()
	s.Payload = bytes()
	flag = number()
	s.LegacyFloor = flag == 1
	if flag > 1 && err == nil {
		err = errors.New("invalid floor flag")
	}
	if remaining != 0 && err == nil {
		err = errors.New("trailing observation material")
	}
	return
}

type ObservationProjectionRepository interface {
	HasPending(context.Context) (bool, error)
	Drain(context.Context, int, int64) (int, error)
}
type ObservationProjections struct {
	Repository ObservationProjectionRepository
}

func (s ObservationProjections) HasPending(ctx context.Context) (bool, error) {
	return s.Repository.HasPending(ctx)
}
func (s ObservationProjections) Drain(ctx context.Context, count int, bytes int64) (int, error) {
	return s.Repository.Drain(ctx, count, bytes)
}

// CounterEvidence describes current values, not a credential/runtime attestation.
// Empty reference/unknown is retained for legacy and heartbeat counter writers.
type CounterEvidence struct {
	Known             bool       `json:"known"`
	LatestSourceKnown bool       `json:"latest_source_known"`
	StatsSourceKnown  bool       `json:"stats_source_known"`
	Source            string     `json:"source"`
	LatestReference   string     `json:"latest_reference,omitempty"`
	StatsReference    string     `json:"stats_reference,omitempty"`
	CounterReceivedAt *time.Time `json:"counter_received_at,omitempty"`
	LatestOccurredAt  *time.Time `json:"latest_occurred_at,omitempty"`
	LatestReceivedAt  *time.Time `json:"latest_received_at,omitempty"`
	StatsOccurredAt   *time.Time `json:"stats_occurred_at,omitempty"`
	StatsReceivedAt   *time.Time `json:"stats_received_at,omitempty"`
}
