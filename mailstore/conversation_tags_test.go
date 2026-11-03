package mailstore

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestConversationTagsNormalizeReplaceAndStayAccountScoped(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "tags@example.com", "", "hash")
	if err != nil {
		t.Fatal(err)
	}
	first := testAccount(t, s, owner.ID, "first@example.com", true)
	second := testAccount(t, s, owner.ID, "second@example.com", false)
	if err := s.SetConversationTags(ctx, first.ID, "thread", []string{" 跟进 ", "重点", "跟进"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConversationTags(ctx, second.ID, "thread", []string{"other"}); err != nil {
		t.Fatal(err)
	}
	tags, err := s.ListConversationTags(ctx, first.ID)
	if err != nil || !reflect.DeepEqual(tags["thread"], []string{"跟进", "重点"}) {
		t.Fatalf("tags = %v, err = %v", tags, err)
	}
	for _, invalid := range [][]string{{" "}, {strings.Repeat("字", 33)}, {"line\nbreak"}, {"tab\tname"}, {"\xff"}, make([]string, 11)} {
		if err := s.SetConversationTags(ctx, first.ID, "thread", invalid); err == nil {
			t.Fatalf("accepted invalid tags: %q", invalid)
		}
	}
	tags, _ = s.ListConversationTags(ctx, first.ID)
	if !reflect.DeepEqual(tags["thread"], []string{"跟进", "重点"}) {
		t.Fatalf("invalid replacement changed saved tags: %v", tags)
	}
	if err := s.SetConversationTags(ctx, first.ID, "thread", []string{"报价"}); err != nil {
		t.Fatal(err)
	}
	tags, _ = s.ListConversationTags(ctx, first.ID)
	if !reflect.DeepEqual(tags["thread"], []string{"报价"}) {
		t.Fatalf("replacement retained old tags: %v", tags)
	}
	if err := s.SetConversationTags(ctx, first.ID, "thread", nil); err != nil {
		t.Fatal(err)
	}
	tags, _ = s.ListConversationTags(ctx, first.ID)
	if len(tags) != 0 {
		t.Fatalf("clear retained tags: %v", tags)
	}
	tags, _ = s.ListConversationTags(ctx, second.ID)
	if !reflect.DeepEqual(tags["thread"], []string{"other"}) {
		t.Fatalf("clearing first account changed second account: %v", tags)
	}
}
