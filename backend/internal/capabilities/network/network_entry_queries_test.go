package network

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type networkEntryQueryRepositoryStub struct {
	actor uint
	items []NetworkEntryListItem
	err   error
}

func (s *networkEntryQueryRepositoryStub) ListNetworkEntries(_ context.Context, actor uint) ([]NetworkEntryListItem, error) {
	s.actor = actor
	return s.items, s.err
}

func TestNetworkEntryQueriesDelegatesCurrentActor(t *testing.T) {
	want := []NetworkEntryListItem{{NetworkEntryRecord: NetworkEntryRecord{ID: 7, Name: "front"}, ServiceKind: "forward"}}
	repository := &networkEntryQueryRepositoryStub{items: want}
	got, err := (NetworkEntryQueries{Repository: repository}).List(context.Background(), 42)
	if err != nil || repository.actor != 42 || !reflect.DeepEqual(got, want) {
		t.Fatalf("actor=%d items=%+v error=%v", repository.actor, got, err)
	}
}

func TestNetworkEntryQueriesRejectsUnavailableInvocation(t *testing.T) {
	for _, service := range []NetworkEntryQueries{{}, {Repository: &networkEntryQueryRepositoryStub{}}} {
		if _, err := service.List(context.Background(), 0); !errors.Is(err, ErrNetworkEntryQueryUnavailable) {
			t.Fatalf("error=%v", err)
		}
	}
}
