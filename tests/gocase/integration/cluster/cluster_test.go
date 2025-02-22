/*
* Licensed to the Apache Software Foundation (ASF) under one
* or more contributor license agreements.  See the NOTICE file
* distributed with this work for additional information
* regarding copyright ownership.  The ASF licenses this file
* to you under the Apache License, Version 2.0 (the
* "License"); you may not use this file except in compliance
* with the License.  You may obtain a copy of the License at
*
*   http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing,
* software distributed under the License is distributed on an
* "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
* KIND, either express or implied.  See the License for the
* specific language governing permissions and limitations
* under the License.
 */

package cluster

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/apache/incubator-kvrocks/tests/gocase/util"
	"github.com/go-redis/redis/v9"
	"github.com/stretchr/testify/require"
)

func TestDisableCluster(t *testing.T) {
	srv := util.StartServer(t, map[string]string{})
	defer srv.Close()

	ctx := context.Background()
	rdb := srv.NewClient()
	defer func() { require.NoError(t, rdb.Close()) }()

	t.Run("can't execute cluster command if disabled", func(t *testing.T) {
		require.ErrorContains(t, rdb.ClusterNodes(ctx).Err(), "not enabled")
	})
}

func TestClusterKeySlot(t *testing.T) {
	srv := util.StartServer(t, map[string]string{"cluster-enabled": "yes"})
	defer srv.Close()

	ctx := context.Background()
	rdb := srv.NewClient()
	defer func() { require.NoError(t, rdb.Close()) }()

	slotTableLen := len(util.SlotTable)
	for i := 0; i < slotTableLen; i++ {
		require.EqualValues(t, i, rdb.ClusterKeySlot(ctx, util.SlotTable[i]).Val())
	}
}

func TestClusterNodes(t *testing.T) {
	srv := util.StartServer(t, map[string]string{"cluster-enabled": "yes"})
	defer srv.Close()

	ctx := context.Background()
	rdb := srv.NewClient()
	defer func() { require.NoError(t, rdb.Close()) }()

	nodeID := "07c37dfeb235213a872192d90877d0cd55635b91"
	require.NoError(t, rdb.Do(ctx, "clusterx", "SETNODEID", nodeID).Err())

	t.Run("basic function of cluster", func(t *testing.T) {
		// cluster is not initialized
		util.ErrorRegexp(t, rdb.ClusterNodes(ctx).Err(), ".*CLUSTERDOWN.*not initialized.*")

		// set cluster nodes info
		clusterNodes := fmt.Sprintf("%s %s %d master - 0-100", nodeID, srv.Host(), srv.Port())
		require.NoError(t, rdb.Do(ctx, "clusterx", "SETNODES", clusterNodes, "2").Err())
		require.EqualValues(t, "2", rdb.Do(ctx, "clusterx", "version").Val())

		// get and check cluster nodes info
		nodes := rdb.ClusterNodes(ctx).Val()
		fields := strings.Split(nodes, " ")
		require.Len(t, fields, 9)
		require.Equal(t, fmt.Sprintf("%s@%d", srv.HostPort(), srv.Port()+10000), fields[1])
		require.Equal(t, "myself,master", fields[2])
		require.Equal(t, "0-100\n", fields[8])

		// cluster slot command
		slots := rdb.ClusterSlots(ctx).Val()
		require.Len(t, slots, 1)
		require.EqualValues(t, 0, slots[0].Start)
		require.EqualValues(t, 100, slots[0].End)
		require.EqualValues(t, []redis.ClusterNode{{ID: nodeID, Addr: srv.HostPort()}}, slots[0].Nodes)
	})

	t.Run("cluster topology is reset by old version", func(t *testing.T) {
		// set cluster nodes info
		clusterNodes := fmt.Sprintf("%s %s %d master - 0-200", nodeID, srv.Host(), srv.Port())
		require.NoError(t, rdb.Do(ctx, "clusterx", "SETNODES", clusterNodes, "1", "force").Err())
		require.EqualValues(t, "1", rdb.Do(ctx, "clusterx", "version").Val())
		nodes := rdb.ClusterNodes(ctx).Val()
		fields := strings.Split(nodes, " ")
		require.Len(t, fields, 9)
		require.Equal(t, "0-200\n", fields[8])
	})

	t.Run("errors of cluster subcommand", func(t *testing.T) {
		require.ErrorContains(t, rdb.Do(ctx, "cluster", "no-subcommand").Err(), "CLUSTER")
		require.ErrorContains(t, rdb.Do(ctx, "clusterx", "version", "a").Err(), "CLUSTER")
		require.ErrorContains(t, rdb.Do(ctx, "cluster", "nodes", "a").Err(), "CLUSTER")
		require.ErrorContains(t, rdb.Do(ctx, "clusterx", "setnodeid", "a").Err(), "CLUSTER")
		require.ErrorContains(t, rdb.Do(ctx, "clusterx", "setnodes", "a").Err(), "CLUSTER")
		require.ErrorContains(t, rdb.Do(ctx, "clusterx", "setnodes", "a", -1).Err(), "Invalid cluster version")
		require.ErrorContains(t, rdb.Do(ctx, "clusterx", "setslot", "16384", "07c37dfeb235213a872192d90877d0cd55635b91", 1).Err(), "CLUSTER")
		require.ErrorContains(t, rdb.Do(ctx, "clusterx", "setslot", "16384", "a", 1).Err(), "CLUSTER")
	})
}

func TestClusterDumpAndLoadClusterNodesInfo(t *testing.T) {
	srv1 := util.StartServer(t, map[string]string{"cluster-enabled": "yes"})
	defer srv1.Close()
	ctx := context.Background()
	rdb1 := srv1.NewClient()
	defer func() { require.NoError(t, rdb1.Close()) }()
	nodeID1 := "07c37dfeb235213a872192d90877d0cd55635b91"
	require.NoError(t, rdb1.Do(ctx, "clusterx", "SETNODEID", nodeID1).Err())

	srv2 := util.StartServer(t, map[string]string{"cluster-enabled": "yes"})
	defer srv2.Close()
	rdb2 := srv2.NewClient()
	defer func() { require.NoError(t, rdb2.Close()) }()
	nodeID2 := "07c37dfeb235213a872192d90877d0cd55635b92"
	require.NoError(t, rdb2.Do(ctx, "clusterx", "SETNODEID", nodeID2).Err())

	clusterNodes := fmt.Sprintf("%s %s %d master - ", nodeID1, srv1.Host(), srv1.Port())
	clusterNodes += "0-1 2 4-8191 8192 8193 10000 10002-11002 16381 16382-16383\n"
	clusterNodes += fmt.Sprintf("%s %s %d master -", nodeID2, srv2.Host(), srv2.Port())

	require.NoError(t, rdb1.Do(ctx, "clusterx", "SETNODES", clusterNodes, "1").Err())
	require.NoError(t, rdb2.Do(ctx, "clusterx", "SETNODES", clusterNodes, "1").Err())

	srv1.Restart()
	slots := rdb1.ClusterSlots(ctx).Val()
	require.Len(t, slots, 5)
	require.EqualValues(t, 10000, slots[2].Start)
	require.EqualValues(t, 10000, slots[2].End)
	require.EqualValues(t, []redis.ClusterNode{{ID: nodeID1, Addr: srv1.HostPort()}}, slots[2].Nodes)
	nodes := rdb1.ClusterNodes(ctx).Val()
	require.Contains(t, nodes, "0-2 4-8193 10000 10002-11002 16381-16383")

	newNodeID := "0123456789012345678901234567890123456789"
	require.NoError(t, rdb1.Do(ctx, "clusterx", "SETNODEID", newNodeID).Err())
	srv1.Restart()
	slots = rdb1.ClusterSlots(ctx).Val()
	require.EqualValues(t, 10000, slots[2].Start)
	require.EqualValues(t, 10000, slots[2].End)
	nodes = rdb1.ClusterNodes(ctx).Val()
	require.Contains(t, nodes, "0-2 4-8193 10000 10002-11002 16381-16383")

	require.NoError(t, rdb2.Do(ctx, "clusterx", "setslot", "0", "node", nodeID2, "2").Err())
	require.NoError(t, rdb1.Do(ctx, "clusterx", "setslot", "0", "node", nodeID2, "2").Err())

	srv1.Restart()
	srv2.Restart()
	nodes = rdb1.ClusterNodes(ctx).Val()
	require.Contains(t, nodes, "1-2 4-8193 10000 10002-11002 16381-16383")
	nodes = rdb2.ClusterNodes(ctx).Val()
	require.Contains(t, nodes, "1-2 4-8193 10000 10002-11002 16381-16383")
}

func TestClusterComplexTopology(t *testing.T) {
	srv := util.StartServer(t, map[string]string{"cluster-enabled": "yes"})
	defer srv.Close()

	ctx := context.Background()
	rdb := srv.NewClient()
	defer func() { require.NoError(t, rdb.Close()) }()

	nodeID := "07c37dfeb235213a872192d90877d0cd55635b91"
	clusterNodes := fmt.Sprintf("%s %s %d master - ", nodeID, srv.Host(), srv.Port())
	clusterNodes += "0-1 2 4-8191 8192 8193 10000 10002-11002 16381 16382-16383"
	require.NoError(t, rdb.Do(ctx, "clusterx", "SETNODES", clusterNodes, "1").Err())
	require.NoError(t, rdb.Do(ctx, "clusterx", "SETNODEID", nodeID).Err())

	slots := rdb.ClusterSlots(ctx).Val()
	require.Len(t, slots, 5)
	require.EqualValues(t, 10000, slots[2].Start)
	require.EqualValues(t, 10000, slots[2].End)
	require.EqualValues(t, []redis.ClusterNode{{ID: nodeID, Addr: srv.HostPort()}}, slots[2].Nodes)

	nodes := rdb.ClusterNodes(ctx).Val()
	require.Contains(t, nodes, "0-2 4-8193 10000 10002-11002 16381-16383")
}

func TestClusterSlotSet(t *testing.T) {
	ctx := context.Background()

	srv1 := util.StartServer(t, map[string]string{"cluster-enabled": "yes"})
	defer srv1.Close()
	rdb1 := srv1.NewClient()
	defer func() { require.NoError(t, rdb1.Close()) }()
	nodeID1 := "07c37dfeb235213a872192d90877d0cd55635b91"
	require.NoError(t, rdb1.Do(ctx, "clusterx", "SETNODEID", nodeID1).Err())

	srv2 := util.StartServer(t, map[string]string{"cluster-enabled": "yes"})
	defer srv2.Close()
	rdb2 := srv2.NewClient()
	defer func() { require.NoError(t, rdb2.Close()) }()
	nodeID2 := "07c37dfeb235213a872192d90877d0cd55635b92"
	require.NoError(t, rdb2.Do(ctx, "clusterx", "SETNODEID", nodeID2).Err())

	clusterNodes := fmt.Sprintf("%s %s %d master - 0-16383\n", nodeID1, srv1.Host(), srv1.Port())
	clusterNodes += fmt.Sprintf("%s %s %d master -", nodeID2, srv2.Host(), srv2.Port())
	require.NoError(t, rdb2.Do(ctx, "clusterx", "SETNODES", clusterNodes, "2").Err())
	require.NoError(t, rdb1.Do(ctx, "clusterx", "SETNODES", clusterNodes, "2").Err())

	slotKey := util.SlotTable[0]
	require.NoError(t, rdb1.Set(ctx, slotKey, 0, 0).Err())
	util.ErrorRegexp(t, rdb2.Set(ctx, slotKey, 0, 0).Err(), fmt.Sprintf(".*MOVED 0.*%d.*", srv1.Port()))

	require.NoError(t, rdb2.Do(ctx, "clusterx", "setslot", "0", "node", nodeID2, "3").Err())
	require.NoError(t, rdb1.Do(ctx, "clusterx", "setslot", "0", "node", nodeID2, "3").Err())
	require.EqualValues(t, "3", rdb2.Do(ctx, "clusterx", "version").Val())
	require.EqualValues(t, "3", rdb1.Do(ctx, "clusterx", "version").Val())
	slots := rdb2.ClusterSlots(ctx).Val()
	require.EqualValues(t, slots, rdb1.ClusterSlots(ctx).Val())
	require.Len(t, slots, 2)
	require.EqualValues(t, 0, slots[0].Start)
	require.EqualValues(t, 0, slots[0].End)
	require.EqualValues(t, []redis.ClusterNode{{ID: nodeID2, Addr: srv2.HostPort()}}, slots[0].Nodes)
	require.EqualValues(t, 1, slots[1].Start)
	require.EqualValues(t, 16383, slots[1].End)
	require.EqualValues(t, []redis.ClusterNode{{ID: nodeID1, Addr: srv1.HostPort()}}, slots[1].Nodes)

	require.NoError(t, rdb2.Set(ctx, slotKey, 0, 0).Err())
	util.ErrorRegexp(t, rdb1.Set(ctx, slotKey, 0, 0).Err(), fmt.Sprintf(".*MOVED 0.*%d.*", srv2.Port()))
	require.NoError(t, rdb2.Do(ctx, "clusterx", "setslot", "1", "node", nodeID2, "4").Err())
	require.NoError(t, rdb1.Do(ctx, "clusterx", "setslot", "1", "node", nodeID2, "4").Err())
	slots = rdb2.ClusterSlots(ctx).Val()
	require.EqualValues(t, slots, rdb1.ClusterSlots(ctx).Val())
	require.Len(t, slots, 2)
	require.EqualValues(t, 0, slots[0].Start)
	require.EqualValues(t, 1, slots[0].End)
	require.EqualValues(t, []redis.ClusterNode{{ID: nodeID2, Addr: srv2.HostPort()}}, slots[0].Nodes)
	require.EqualValues(t, 2, slots[1].Start)
	require.EqualValues(t, 16383, slots[1].End)
	require.EqualValues(t, []redis.ClusterNode{{ID: nodeID1, Addr: srv1.HostPort()}}, slots[1].Nodes)

	// wrong version can't update slot distribution
	require.ErrorContains(t, rdb2.Do(ctx, "clusterx", "setslot", "2", "node", nodeID2, "6").Err(), "version")
	require.ErrorContains(t, rdb2.Do(ctx, "clusterx", "setslot", "2", "node", nodeID2, "4").Err(), "version")
	require.EqualValues(t, "4", rdb2.Do(ctx, "clusterx", "version").Val())
	require.EqualValues(t, "4", rdb1.Do(ctx, "clusterx", "version").Val())
}

func TestClusterMultiple(t *testing.T) {
	ctx := context.Background()

	var srv []*util.KvrocksServer
	var rdb []*redis.Client
	var nodeID []string

	for i := 0; i < 4; i++ {
		s := util.StartServer(t, map[string]string{"cluster-enabled": "yes"})
		t.Cleanup(s.Close)
		c := s.NewClient()
		t.Cleanup(func() { require.NoError(t, c.Close()) })
		srv = append(srv, s)
		rdb = append(rdb, c)
		nodeID = append(nodeID, fmt.Sprintf("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx%02d", i))
	}

	t.Run("requests on non-init-cluster", func(t *testing.T) {
		util.ErrorRegexp(t, rdb[0].Set(ctx, util.SlotTable[0], 0, 0).Err(), ".*CLUSTERDOWN.*not served.*")
		util.ErrorRegexp(t, rdb[2].Set(ctx, util.SlotTable[16383], 16383, 0).Err(), ".*CLUSTERDOWN.*not served.*")
	})

	clusterNodes := fmt.Sprintf("%s %s %d master - 0-1 3 5-8191\n", nodeID[1], srv[1].Host(), srv[1].Port())
	clusterNodes += fmt.Sprintf("%s %s %d master - 8192-16383\n", nodeID[2], srv[2].Host(), srv[2].Port())
	clusterNodes += fmt.Sprintf("%s %s %d slave %s", nodeID[3], srv[3].Host(), srv[3].Port(), nodeID[2])

	// node0 doesn't serve any slot, just like a router
	for i := 0; i < 4; i++ {
		require.NoError(t, rdb[i].Do(ctx, "clusterx", "setnodes", clusterNodes, "1").Err())
	}

	t.Run("cluster info command", func(t *testing.T) {
		r := rdb[1].ClusterInfo(ctx).Val()
		require.Contains(t, r, "cluster_state:ok")
		require.Contains(t, r, "cluster_slots_assigned:16382")
		require.Contains(t, r, "cluster_slots_ok:16382")
		require.Contains(t, r, "cluster_known_nodes:3")
		require.Contains(t, r, "cluster_size:2")
		require.Contains(t, r, "cluster_current_epoch:1")
		require.Contains(t, r, "cluster_my_epoch:1")
	})

	t.Run("MOVED slot ip:port if needed", func(t *testing.T) {
		// request node2 that doesn't serve slot 0, we will receive MOVED
		util.ErrorRegexp(t, rdb[2].Set(ctx, util.SlotTable[0], 0, 0).Err(), fmt.Sprintf(".*MOVED 0.*%d.*", srv[1].Port()))
		// request node3 that doesn't serve slot 0, we will receive MOVED
		util.ErrorRegexp(t, rdb[3].Get(ctx, util.SlotTable[0]).Err(), fmt.Sprintf(".*MOVED 0.*%d.*", srv[1].Port()))
		// request node1 that doesn't serve slot 16383, we will receive MOVED, and the MOVED node must be master
		util.ErrorRegexp(t, rdb[1].Get(ctx, util.SlotTable[16383]).Err(), fmt.Sprintf(".*MOVED 16383.*%d.*", srv[2].Port()))
	})

	t.Run("requests on cluster are ok", func(t *testing.T) {
		// request node1 that serves slot 0, that's ok
		require.NoError(t, rdb[1].Set(ctx, util.SlotTable[0], 0, 0).Err())
		// request node2 that serve slot 16383, that's ok
		require.NoError(t, rdb[2].Set(ctx, util.SlotTable[16383], 16383, 0).Err())
		// request replicas a write command, it's wrong
		require.ErrorContains(t, rdb[3].Set(ctx, util.SlotTable[16383], 16383, 0).Err(), "MOVED")
		// request a read-only command to node3 that serve slot 16383, that's ok
		util.WaitForOffsetSync(t, rdb[2], rdb[3])
		require.Equal(t, "16383", rdb[3].Get(ctx, util.SlotTable[16383]).Val())
	})

	t.Run("requests non-member of cluster, role is master", func(t *testing.T) {
		util.ErrorRegexp(t, rdb[0].Set(ctx, util.SlotTable[0], 0, 0).Err(), fmt.Sprintf(".*MOVED 0.*%d.*", srv[1].Port()))
		util.ErrorRegexp(t, rdb[0].Get(ctx, util.SlotTable[16383]).Err(), fmt.Sprintf(".*MOVED 16383.*%d.*", srv[2].Port()))
	})

	t.Run("cluster slot is not served", func(t *testing.T) {
		util.ErrorRegexp(t, rdb[1].Set(ctx, util.SlotTable[2], 2, 0).Err(), ".*CLUSTERDOWN.*not served.*")
	})

	t.Run("multiple keys(cross slots) command is wrong", func(t *testing.T) {
		require.ErrorContains(t, rdb[1].MSet(ctx, util.SlotTable[0], 0, util.SlotTable[1], 1).Err(), "CROSSSLOT")
	})

	t.Run("multiple keys(the same slots) command is right", func(t *testing.T) {
		require.NoError(t, rdb[1].MSet(ctx, util.SlotTable[0], 0, util.SlotTable[0], 1).Err())
	})

	t.Run("cluster MULTI-exec cross slots and in one node", func(t *testing.T) {
		require.NoError(t, rdb[1].Do(ctx, "MULTI").Err())
		require.NoError(t, rdb[1].Set(ctx, util.SlotTable[0], 0, 0).Err())
		require.NoError(t, rdb[1].Set(ctx, util.SlotTable[1], 0, 0).Err())
		require.EqualValues(t, []interface{}{"OK", "OK"}, rdb[1].Do(ctx, "EXEC").Val())
	})

	t.Run("cluster MULTI-exec cross slots but not in one node", func(t *testing.T) {
		require.NoError(t, rdb[1].Set(ctx, util.SlotTable[0], "no-multi", 0).Err())
		require.NoError(t, rdb[1].Do(ctx, "MULTI").Err())
		require.NoError(t, rdb[1].Set(ctx, util.SlotTable[0], "multi", 0).Err())
		util.ErrorRegexp(t, rdb[1].Set(ctx, util.SlotTable[16383], 0, 0).Err(), fmt.Sprintf(".*MOVED 16383.*%d.*", srv[2].Port()))
		require.ErrorContains(t, rdb[1].Do(ctx, "EXEC").Err(), "EXECABORT")
		require.Equal(t, "no-multi", rdb[1].Get(ctx, util.SlotTable[0]).Val())
	})
}
