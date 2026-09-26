package shardkv

import (
	"testing"

	"6.5840/shardkv1/shardcfg"
	"6.5840/tester1"
)

func TestConfigHistory5D(t *testing.T) {
	ts := MakeTest(t, "Test (5D): query configuration history...", true)
	defer ts.Cleanup()

	ts.setupKVService()
	sck := ts.ShardCtrler()
	gid2 := ts.newGid()
	if !ts.joinGroups(sck, []tester.Tgid{gid2}) {
		t.Fatal("join failed")
	}

	old := sck.QueryNum(shardcfg.NumFirst)
	current := sck.QueryNum(shardcfg.NumFirst + 1)
	if old == nil || old.Num != 1 || !old.IsMember(shardcfg.Gid1) || old.IsMember(gid2) {
		t.Fatalf("wrong historical configuration: %v", old)
	}
	if current == nil || current.String() != sck.Query().String() {
		t.Fatalf("latest configuration missing from history: %v", current)
	}
}
