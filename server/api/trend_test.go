// Copyright 2017 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package api

import (
	"fmt"

	. "github.com/pingcap/check"
	"github.com/pingcap/kvproto/pkg/metapb"
	"github.com/pingcap/pd/server"
	"github.com/pingcap/pd/server/core"
	"github.com/pingcap/pd/server/schedule"
)

var _ = Suite(&testTrendSuite{})

type testTrendSuite struct{}

func (s *testTrendSuite) TestTend(c *C) {
	svr, cleanup := mustNewServer(c)
	defer cleanup()
	mustWaitLeader(c, []*server.Server{svr})

	mustBootstrapCluster(c, svr)
	for i := 1; i <= 3; i++ {
		mustPutStore(c, svr, uint64(i), metapb.StoreState_Up, nil)
	}

	// Create 3 regions, all peers on store1 and store2, and the leaders are all on store1.
	mustRegionHeartbeat(c, svr, s.newRegionInfo(4, "", "a", 2, 2, []uint64{1, 2}, 1))
	mustRegionHeartbeat(c, svr, s.newRegionInfo(5, "a", "b", 2, 2, []uint64{1, 2}, 1))
	mustRegionHeartbeat(c, svr, s.newRegionInfo(6, "b", "", 2, 2, []uint64{1, 2}, 1))

	// Create 3 operators that transfers leader, moves follower, moves leader.
	svr.GetHandler().AddTransferLeaderOperator(4, 2)
	svr.GetHandler().AddTransferPeerOperator(5, 2, 3)
	svr.GetHandler().AddTransferPeerOperator(6, 1, 3)

	// Complete the operators.
	mustRegionHeartbeat(c, svr, s.newRegionInfo(4, "", "a", 2, 2, []uint64{1, 2}, 2))
	region := s.newRegionInfo(5, "a", "b", 3, 2, []uint64{1, 3}, 1)
	op, err := svr.GetHandler().GetOperator(5)
	c.Assert(op, NotNil)
	region.Peers[1].Id = op.Step(0).(schedule.AddPeer).PeerID
	region.Voters[1].Id = op.Step(0).(schedule.AddPeer).PeerID
	mustRegionHeartbeat(c, svr, region)

	op, err = svr.GetHandler().GetOperator(6)
	c.Assert(op, NotNil)
	region = s.newRegionInfo(6, "b", "", 3, 2, []uint64{2, 3}, 2)
	region.Peers[1].Id = op.Step(0).(schedule.AddPeer).PeerID
	region.Voters[1].Id = op.Step(0).(schedule.AddPeer).PeerID
	mustRegionHeartbeat(c, svr, region)

	var trend Trend
	err = readJSONWithURL(fmt.Sprintf("%s%s/api/v1/trend", svr.GetAddr(), apiPrefix), &trend)
	c.Assert(err, IsNil)

	// Check store states.
	expectLeaderCount := map[uint64]int{1: 1, 2: 2, 3: 0}
	expectRegionCount := map[uint64]int{1: 2, 2: 2, 3: 2}
	c.Assert(len(trend.Stores), Equals, 3)
	for _, store := range trend.Stores {
		c.Assert(store.LeaderCount, Equals, expectLeaderCount[store.ID])
		c.Assert(store.RegionCount, Equals, expectRegionCount[store.ID])
	}

	// Check history.
	expectHistory := map[trendHistoryEntry]int{
		{From: 1, To: 2, Kind: "leader"}: 2,
		{From: 1, To: 3, Kind: "region"}: 1,
		{From: 2, To: 3, Kind: "region"}: 1,
	}
	c.Assert(len(trend.History.Entries), Equals, 3)
	for _, history := range trend.History.Entries {
		c.Assert(history.Count, Equals, expectHistory[trendHistoryEntry{From: history.From, To: history.To, Kind: history.Kind}])
	}
}

func (s *testTrendSuite) newRegionInfo(id uint64, startKey, endKey string, confVer, ver uint64, stores []uint64, leaderStore uint64) *core.RegionInfo {
	var (
		peers  []*metapb.Peer
		leader *metapb.Peer
	)
	for _, id := range stores {
		p := &metapb.Peer{Id: 10 + id, StoreId: id}
		if id == leaderStore {
			leader = p
		}
		peers = append(peers, p)
	}
	return core.NewRegionInfo(
		&metapb.Region{
			Id:          id,
			StartKey:    []byte(startKey),
			EndKey:      []byte(endKey),
			RegionEpoch: &metapb.RegionEpoch{ConfVer: confVer, Version: ver},
			Peers:       peers,
		},
		leader,
	)
}
