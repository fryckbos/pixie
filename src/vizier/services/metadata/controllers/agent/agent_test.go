package agent_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/vfs"
	"github.com/gogo/protobuf/proto"
	"github.com/nats-io/nats.go"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"

	uuidpb "pixielabs.ai/pixielabs/src/api/public/uuidpb"
	distributedpb "pixielabs.ai/pixielabs/src/carnot/planner/distributedpb"
	bloomfilterpb "pixielabs.ai/pixielabs/src/shared/bloomfilterpb"
	k8s_metadatapb "pixielabs.ai/pixielabs/src/shared/k8s/metadatapb"
	metadatapb "pixielabs.ai/pixielabs/src/shared/metadatapb"
	types "pixielabs.ai/pixielabs/src/shared/types/go"
	utils "pixielabs.ai/pixielabs/src/utils"
	"pixielabs.ai/pixielabs/src/utils/testingutils"
	messagespb "pixielabs.ai/pixielabs/src/vizier/messages/messagespb"
	"pixielabs.ai/pixielabs/src/vizier/services/metadata/controllers/agent"
	"pixielabs.ai/pixielabs/src/vizier/services/metadata/controllers/testutils"
	storepb "pixielabs.ai/pixielabs/src/vizier/services/metadata/storepb"
	agentpb "pixielabs.ai/pixielabs/src/vizier/services/shared/agentpb"
	"pixielabs.ai/pixielabs/src/vizier/utils/datastore/pebbledb"
)

func setupManager(t *testing.T) (agent.Store, agent.Manager, *nats.Conn, func()) {
	// Setup NATS.
	natsPort, natsCleanup := testingutils.StartNATS(t)
	nc, err := nats.Connect(testingutils.GetNATSURL(natsPort))
	if err != nil {
		t.Fatal(err)
	}

	memFS := vfs.NewMem()
	c, err := pebble.Open("test", &pebble.Options{
		FS: memFS,
	})
	if err != nil {
		t.Fatal("failed to initialize a pebbledb")
		os.Exit(1)
	}

	db := pebbledb.New(c, 3*time.Second)
	ads := agent.NewDatastore(db, 1*time.Minute)

	cleanupFn := func() {
		natsCleanup()
		db.Close()
	}

	createAgentInADS(t, testutils.ExistingAgentUUID, ads, testutils.ExistingAgentInfo)
	createAgentInADS(t, testutils.UnhealthyAgentUUID, ads, testutils.UnhealthyAgentInfo)
	createAgentInADS(t, testutils.UnhealthyKelvinAgentUUID, ads, testutils.UnhealthyKelvinAgentInfo)

	clock := testingutils.NewTestClock(time.Unix(0, testutils.ClockNowNS))
	agtMgr := agent.NewManagerWithClock(ads, nil, nc, clock)

	return ads, agtMgr, nc, cleanupFn
}

func createAgentInADS(t *testing.T, agentID string, ads agent.Store, agentPb string) {
	info := new(agentpb.Agent)
	if err := proto.UnmarshalText(agentPb, info); err != nil {
		t.Fatalf("Cannot Unmarshal protobuf for %s", agentID)
	}
	agUUID, err := uuid.FromString(agentID)
	if err != nil {
		t.Fatalf("Could not convert uuid from string")
	}
	err = ads.CreateAgent(agUUID, info)

	// Add schema info.
	schema := new(storepb.TableInfo)
	if err := proto.UnmarshalText(testutils.SchemaInfoPB, schema); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	err = ads.UpdateSchemas(agUUID, []*storepb.TableInfo{schema})
	if err != nil {
		t.Fatalf("Could not add schema for agent")
	}
}

func TestRegisterAgent(t *testing.T) {
	ads, agtMgr, _, cleanup := setupManager(t)
	defer cleanup()

	u, err := uuid.FromString(testutils.NewAgentUUID)
	if err != nil {
		t.Fatal("Could not generate UUID.")
	}
	upb := utils.ProtoFromUUID(u)

	agentInfo := &agentpb.Agent{
		Info: &agentpb.AgentInfo{
			HostInfo: &agentpb.HostInfo{
				Hostname: "localhost",
				HostIP:   "127.0.0.4",
			},
			AgentID: upb,
			Capabilities: &agentpb.AgentCapabilities{
				CollectsData: true,
			},
		},
		LastHeartbeatNS: 1,
		CreateTimeNS:    4,
	}

	id, err := agtMgr.RegisterAgent(agentInfo)
	assert.Equal(t, nil, err)
	assert.Equal(t, uint32(1), id)

	// Check that agent exists now.
	agt, err := ads.GetAgent(u)
	assert.Nil(t, err)
	assert.NotNil(t, agt)

	assert.Equal(t, int64(testutils.ClockNowNS), agt.LastHeartbeatNS)
	assert.Equal(t, int64(testutils.ClockNowNS), agt.CreateTimeNS)
	uid, err := utils.UUIDFromProto(agt.Info.AgentID)
	assert.Equal(t, nil, err)
	assert.Equal(t, testutils.NewAgentUUID, uid.String())
	assert.Equal(t, "localhost", agt.Info.HostInfo.Hostname)
	assert.Equal(t, uint32(1), agt.ASID)

	hostnameID, err := ads.GetAgentIDForHostnamePair(&agent.HostnameIPPair{"", "127.0.0.4"})
	assert.Nil(t, err)
	assert.Equal(t, testutils.NewAgentUUID, hostnameID)
}

func TestRegisterKelvinAgent(t *testing.T) {
	ads, agtMgr, _, cleanup := setupManager(t)
	defer cleanup()

	u, err := uuid.FromString(testutils.KelvinAgentUUID)
	if err != nil {
		t.Fatal("Could not generate UUID.")
	}
	upb := utils.ProtoFromUUID(u)

	agentInfo := &agentpb.Agent{
		Info: &agentpb.AgentInfo{
			HostInfo: &agentpb.HostInfo{
				Hostname: "test",
				HostIP:   "127.0.0.3",
			},
			AgentID: upb,
			Capabilities: &agentpb.AgentCapabilities{
				CollectsData: false,
			},
		},
		LastHeartbeatNS: 1,
		CreateTimeNS:    4,
	}

	id, err := agtMgr.RegisterAgent(agentInfo)
	assert.Equal(t, nil, err)
	assert.Equal(t, uint32(1), id)

	// Check that agent exists now.
	agt, err := ads.GetAgent(u)
	assert.Nil(t, err)
	assert.NotNil(t, agt)

	assert.Equal(t, int64(testutils.ClockNowNS), agt.LastHeartbeatNS)
	assert.Equal(t, int64(testutils.ClockNowNS), agt.CreateTimeNS)
	uid, err := utils.UUIDFromProto(agt.Info.AgentID)
	assert.Equal(t, nil, err)
	assert.Equal(t, testutils.KelvinAgentUUID, uid.String())
	assert.Equal(t, "test", agt.Info.HostInfo.Hostname)
	assert.Equal(t, uint32(1), agt.ASID)

	hostnameID, err := ads.GetAgentIDForHostnamePair(&agent.HostnameIPPair{"test", "127.0.0.3"})
	assert.Nil(t, err)
	assert.Equal(t, testutils.KelvinAgentUUID, hostnameID)
}

func TestRegisterExistingAgent(t *testing.T) {
	ads, agtMgr, _, cleanup := setupManager(t)
	defer cleanup()

	u, err := uuid.FromString(testutils.ExistingAgentUUID)
	if err != nil {
		t.Fatal("Could not generate UUID.")
	}
	upb := utils.ProtoFromUUID(u)

	agentInfo := &agentpb.Agent{
		Info: &agentpb.AgentInfo{
			HostInfo: &agentpb.HostInfo{
				Hostname: "localhost",
				HostIP:   "127.0.0.1",
			},
			AgentID: upb,
		},
		LastHeartbeatNS: 1,
		CreateTimeNS:    4,
	}
	id, err := agtMgr.RegisterAgent(agentInfo)
	assert.Nil(t, err)
	assert.Equal(t, uint32(123), id)

	// Check that correct agent info is in ads.
	agt, err := ads.GetAgent(u)
	assert.Nil(t, err)
	assert.NotNil(t, agt)
	assert.Equal(t, int64(testutils.HealthyAgentLastHeartbeatNS), agt.LastHeartbeatNS) // 70 seconds in NS.
	assert.Equal(t, int64(0), agt.CreateTimeNS)
	uid, err := utils.UUIDFromProto(agt.Info.AgentID)
	assert.Equal(t, nil, err)
	assert.Equal(t, testutils.ExistingAgentUUID, uid.String())
	assert.Equal(t, "testhost", agt.Info.HostInfo.Hostname)
	assert.Equal(t, uint32(123), agt.ASID)
}

func TestUpdateHeartbeat(t *testing.T) {
	ads, agtMgr, _, cleanup := setupManager(t)
	defer cleanup()

	u, err := uuid.FromString(testutils.ExistingAgentUUID)
	if err != nil {
		t.Fatal("Could not generate UUID.")
	}

	err = agtMgr.UpdateHeartbeat(u)
	assert.Nil(t, err)

	// Check that correct agent info is in etcd.
	agt, err := ads.GetAgent(u)
	assert.Nil(t, err)
	assert.NotNil(t, agt)

	assert.Equal(t, int64(testutils.ClockNowNS), agt.LastHeartbeatNS)
	assert.Equal(t, int64(0), agt.CreateTimeNS)
	uid, err := utils.UUIDFromProto(agt.Info.AgentID)
	assert.Equal(t, nil, err)
	assert.Equal(t, testutils.ExistingAgentUUID, uid.String())
	assert.Equal(t, "testhost", agt.Info.HostInfo.Hostname)
}

func TestUpdateHeartbeatForNonExistingAgent(t *testing.T) {
	_, agtMgr, _, cleanup := setupManager(t)
	defer cleanup()

	u, err := uuid.FromString(testutils.NewAgentUUID)
	if err != nil {
		t.Fatal("Could not generate UUID.")
	}

	err = agtMgr.UpdateHeartbeat(u)
	assert.NotNil(t, err)
}

func TestUpdateAgentDelete(t *testing.T) {
	ads, agtMgr, _, cleanup := setupManager(t)
	defer cleanup()

	u, err := uuid.FromString(testutils.UnhealthyAgentUUID)
	if err != nil {
		t.Fatal("Could not generate UUID.")
	}
	u2, err := uuid.FromString(testutils.UnhealthyKelvinAgentUUID)
	if err != nil {
		t.Fatal("Could not generate UUID.")
	}

	err = agtMgr.DeleteAgent(u)
	assert.Nil(t, err)
	err = agtMgr.DeleteAgent(u2)
	assert.Nil(t, err)

	agents, err := ads.GetAgents()
	assert.Equal(t, 1, len(agents))

	agt, err := ads.GetAgent(u)
	assert.Nil(t, err)
	assert.Nil(t, agt)

	hostnameID, err := ads.GetAgentIDForHostnamePair(&agent.HostnameIPPair{"", "127.0.0.2"})
	assert.Nil(t, err)
	assert.Equal(t, "", hostnameID)
}

func TestGetActiveAgents(t *testing.T) {
	_, agtMgr, _, cleanup := setupManager(t)
	defer cleanup()

	agents, err := agtMgr.GetActiveAgents()
	assert.Nil(t, err)

	assert.Equal(t, 3, len(agents))

	agent0Info := &agentpb.Agent{
		LastHeartbeatNS: 0,
		CreateTimeNS:    0,
		Info: &agentpb.AgentInfo{
			AgentID: &uuidpb.UUID{Data: []byte("5ba7b8109dad11d180b400c04fd430c8")},
			HostInfo: &agentpb.HostInfo{
				Hostname: "abcd",
				HostIP:   "127.0.0.3",
			},
			Capabilities: &agentpb.AgentCapabilities{
				CollectsData: false,
			},
		},
		ASID: 789,
	}
	assert.Equal(t, agent0Info, agents[0])

	agent1Info := &agentpb.Agent{
		LastHeartbeatNS: testutils.HealthyAgentLastHeartbeatNS,
		CreateTimeNS:    0,
		Info: &agentpb.AgentInfo{
			AgentID: &uuidpb.UUID{Data: []byte("7ba7b8109dad11d180b400c04fd430c8")},
			HostInfo: &agentpb.HostInfo{
				PodName:  "pem-existing",
				Hostname: "testhost",
				HostIP:   "127.0.0.1",
			},
			Capabilities: &agentpb.AgentCapabilities{
				CollectsData: true,
			},
		},
		ASID: 123,
	}
	assert.Equal(t, agent1Info, agents[1])

	agent2Info := &agentpb.Agent{
		LastHeartbeatNS: 0,
		CreateTimeNS:    0,
		Info: &agentpb.AgentInfo{
			AgentID: &uuidpb.UUID{Data: []byte("8ba7b8109dad11d180b400c04fd430c8")},
			HostInfo: &agentpb.HostInfo{
				Hostname: "anotherhost",
				HostIP:   "127.0.0.2",
			},
			Capabilities: &agentpb.AgentCapabilities{
				CollectsData: true,
			},
		},
		ASID: 456,
	}
	assert.Equal(t, agent2Info, agents[2])
}

func TestApplyUpdates(t *testing.T) {
	ads, agtMgr, _, cleanup := setupManager(t)
	defer cleanup()

	u, err := uuid.FromString(testutils.ExistingAgentUUID)
	if err != nil {
		t.Fatal("Could not parse UUID from string.")
	}

	schemas := make([]*storepb.TableInfo, 1)

	schema1 := new(storepb.TableInfo)
	if err := proto.UnmarshalText(testutils.SchemaInfoPB, schema1); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	schemas[0] = schema1

	createdProcesses := make([]*k8s_metadatapb.ProcessCreated, 2)

	cp1 := new(k8s_metadatapb.ProcessCreated)
	if err := proto.UnmarshalText(testutils.ProcessCreated1PB, cp1); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	createdProcesses[0] = cp1
	cp2 := new(k8s_metadatapb.ProcessCreated)
	if err := proto.UnmarshalText(testutils.ProcessCreated2PB, cp2); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	createdProcesses[1] = cp2

	cProcessInfo := make([]*k8s_metadatapb.ProcessInfo, 2)

	cpi1 := new(k8s_metadatapb.ProcessInfo)
	if err := proto.UnmarshalText(testutils.ProcessInfo1PB, cpi1); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	cProcessInfo[0] = cpi1
	cpi2 := new(k8s_metadatapb.ProcessInfo)
	if err := proto.UnmarshalText(testutils.ProcessInfo2PB, cpi2); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	cProcessInfo[1] = cpi2

	expectedDataInfo := &messagespb.AgentDataInfo{
		MetadataInfo: &distributedpb.MetadataInfo{
			MetadataFields: []metadatapb.MetadataType{
				metadatapb.CONTAINER_ID,
				metadatapb.POD_NAME,
			},
			Filter: &distributedpb.MetadataInfo_XXHash64BloomFilter{
				XXHash64BloomFilter: &bloomfilterpb.XXHash64BloomFilter{
					Data:      []byte("1234"),
					NumHashes: 4,
				},
			},
		},
	}

	update := &messagespb.AgentUpdateInfo{
		Schema:         schemas,
		ProcessCreated: createdProcesses,
		Data:           expectedDataInfo,
	}

	agentUpdate := agent.Update{
		UpdateInfo: update,
		AgentID:    u,
	}

	agtMgr.ApplyAgentUpdate(&agentUpdate)

	upid1 := &types.UInt128{
		Low:  uint64(89101),
		High: uint64(528280977975),
	}

	upid2 := &types.UInt128{
		Low:  uint64(468),
		High: uint64(528280977975),
	}

	pInfos, err := ads.GetProcesses([]*types.UInt128{upid1, upid2})
	assert.Nil(t, err)
	assert.Equal(t, 2, len(pInfos))

	assert.Equal(t, cProcessInfo[0], pInfos[0])
	assert.Equal(t, cProcessInfo[1], pInfos[1])

	dataInfos, err := ads.GetAgentsDataInfo()
	assert.Nil(t, err)
	assert.NotNil(t, dataInfos)
	dataInfo, present := dataInfos[u]
	assert.True(t, present)
	assert.Equal(t, dataInfo, expectedDataInfo)
}

func TestApplyUpdatesDeleted(t *testing.T) {
	ads, agtMgr, _, cleanup := setupManager(t)
	defer cleanup()

	u, err := uuid.FromString(testutils.NewAgentUUID)
	if err != nil {
		t.Fatal("Could not parse UUID from string.")
	}

	schemas := make([]*storepb.TableInfo, 1)

	schema1 := new(storepb.TableInfo)
	if err := proto.UnmarshalText(testutils.SchemaInfoPB, schema1); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	schemas[0] = schema1

	createdProcesses := make([]*k8s_metadatapb.ProcessCreated, 2)

	cp1 := new(k8s_metadatapb.ProcessCreated)
	if err := proto.UnmarshalText(testutils.ProcessCreated1PB, cp1); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	createdProcesses[0] = cp1
	cp2 := new(k8s_metadatapb.ProcessCreated)
	if err := proto.UnmarshalText(testutils.ProcessCreated2PB, cp2); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	createdProcesses[1] = cp2

	cProcessInfo := make([]*k8s_metadatapb.ProcessInfo, 2)

	cpi1 := new(k8s_metadatapb.ProcessInfo)
	if err := proto.UnmarshalText(testutils.ProcessInfo1PB, cpi1); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	cProcessInfo[0] = cpi1
	cpi2 := new(k8s_metadatapb.ProcessInfo)
	if err := proto.UnmarshalText(testutils.ProcessInfo2PB, cpi2); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	cProcessInfo[1] = cpi2

	expectedDataInfo := &messagespb.AgentDataInfo{
		MetadataInfo: &distributedpb.MetadataInfo{
			MetadataFields: []metadatapb.MetadataType{
				metadatapb.CONTAINER_ID,
				metadatapb.POD_NAME,
			},
			Filter: &distributedpb.MetadataInfo_XXHash64BloomFilter{
				XXHash64BloomFilter: &bloomfilterpb.XXHash64BloomFilter{
					Data:      []byte("1234"),
					NumHashes: 4,
				},
			},
		},
	}

	update := &messagespb.AgentUpdateInfo{
		Schema:         schemas,
		ProcessCreated: createdProcesses,
		Data:           expectedDataInfo,
	}

	agentUpdate := agent.Update{
		UpdateInfo: update,
		AgentID:    u,
	}

	agtMgr.ApplyAgentUpdate(&agentUpdate)

	upid1 := &types.UInt128{
		Low:  uint64(89101),
		High: uint64(528280977975),
	}

	upid2 := &types.UInt128{
		Low:  uint64(468),
		High: uint64(528280977975),
	}

	pInfos, err := ads.GetProcesses([]*types.UInt128{upid1, upid2})
	assert.Nil(t, err)
	assert.Equal(t, 2, len(pInfos))

	assert.Nil(t, pInfos[0])
	assert.Nil(t, pInfos[1])

	dataInfos, err := ads.GetAgentsDataInfo()
	assert.Nil(t, err)
	assert.NotNil(t, dataInfos)
	_, present := dataInfos[u]
	assert.False(t, present)
}

func TestAgentTerminatedProcesses(t *testing.T) {
	ads, agtMgr, _, cleanup := setupManager(t)
	defer cleanup()

	u, err := uuid.FromString(testutils.ExistingAgentUUID)
	if err != nil {
		t.Fatal("Could not parse UUID from string.")
	}

	schemas := make([]*storepb.TableInfo, 1)

	schema1 := new(storepb.TableInfo)
	if err := proto.UnmarshalText(testutils.SchemaInfoPB, schema1); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	schemas[0] = schema1

	terminatedProcesses := make([]*k8s_metadatapb.ProcessTerminated, 2)

	tp1 := new(k8s_metadatapb.ProcessTerminated)
	if err := proto.UnmarshalText(testutils.ProcessTerminated1PB, tp1); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	terminatedProcesses[0] = tp1
	tp2 := new(k8s_metadatapb.ProcessTerminated)
	if err := proto.UnmarshalText(testutils.ProcessTerminated2PB, tp2); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	terminatedProcesses[1] = tp2

	createdProcesses := make([]*k8s_metadatapb.ProcessCreated, 2)

	cp1 := new(k8s_metadatapb.ProcessCreated)
	if err := proto.UnmarshalText(testutils.ProcessCreated1PB, cp1); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	createdProcesses[0] = cp1
	cp2 := new(k8s_metadatapb.ProcessCreated)
	if err := proto.UnmarshalText(testutils.ProcessCreated2PB, cp2); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	createdProcesses[1] = cp2

	updatedInfo := make([]*k8s_metadatapb.ProcessInfo, 2)
	upi1 := new(k8s_metadatapb.ProcessInfo)
	if err := proto.UnmarshalText(testutils.ProcessInfo1PB, upi1); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	upi1.StopTimestampNS = 6
	updatedInfo[0] = upi1
	upi2 := new(k8s_metadatapb.ProcessInfo)
	if err := proto.UnmarshalText(testutils.ProcessInfo2PB, upi2); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}
	upi2.StopTimestampNS = 10
	updatedInfo[1] = upi2

	update := &messagespb.AgentUpdateInfo{
		Schema:         schemas,
		ProcessCreated: createdProcesses,
	}

	agentUpdate := agent.Update{
		UpdateInfo: update,
		AgentID:    u,
	}

	agtMgr.ApplyAgentUpdate(&agentUpdate)

	update = &messagespb.AgentUpdateInfo{
		Schema:            schemas,
		ProcessTerminated: terminatedProcesses,
	}

	agentUpdate = agent.Update{
		UpdateInfo: update,
		AgentID:    u,
	}

	agtMgr.ApplyAgentUpdate(&agentUpdate)

	upid1 := &types.UInt128{
		Low:  uint64(89101),
		High: uint64(528280977975),
	}

	upid2 := &types.UInt128{
		Low:  uint64(468),
		High: uint64(528280977975),
	}

	pInfos, err := ads.GetProcesses([]*types.UInt128{upid1, upid2})
	assert.Nil(t, err)
	assert.Equal(t, 2, len(pInfos))

	assert.Equal(t, updatedInfo[0], pInfos[0])
	assert.Equal(t, updatedInfo[1], pInfos[1])
}

func TestAgent_GetAgentUpdate(t *testing.T) {
	_, agtMgr, _, cleanup := setupManager(t)
	defer cleanup()

	agUUID0, err := uuid.FromString(testutils.UnhealthyKelvinAgentUUID)
	assert.Nil(t, err)
	agUUID1, err := uuid.FromString(testutils.UnhealthyAgentUUID)
	assert.Nil(t, err)
	agUUID2, err := uuid.FromString(testutils.ExistingAgentUUID)
	assert.Nil(t, err)
	agUUID3, err := uuid.FromString(testutils.NewAgentUUID)
	assert.Nil(t, err)

	// Read the initial agent state.
	cursor := agtMgr.NewAgentUpdateCursor()
	updates, schema, err := agtMgr.GetAgentUpdates(cursor)
	assert.Nil(t, err)
	assert.Equal(t, 3, len(updates))
	assert.Equal(t, agUUID0, utils.UUIDFromProtoOrNil(updates[0].AgentID))
	assert.NotNil(t, updates[0].GetAgent())
	assert.Equal(t, agUUID2, utils.UUIDFromProtoOrNil(updates[1].AgentID))
	assert.NotNil(t, updates[1].GetAgent())
	assert.Equal(t, agUUID1, utils.UUIDFromProtoOrNil(updates[2].AgentID))
	assert.NotNil(t, updates[2].GetAgent())
	assert.Equal(t, 1, len(schema.Tables))
	assert.Equal(t, 1, len(schema.TableNameToAgentIDs))
	assert.Equal(t, 3, len(schema.TableNameToAgentIDs["a_table"].AgentID))

	newAgentInfo := &agentpb.Agent{
		Info: &agentpb.AgentInfo{
			HostInfo: &agentpb.HostInfo{
				Hostname: "localhost",
				HostIP:   "127.0.0.7",
			},
			AgentID: utils.ProtoFromUUID(agUUID3),
			Capabilities: &agentpb.AgentCapabilities{
				CollectsData: true,
			},
		},
	}

	oldAgentDataInfo := &messagespb.AgentDataInfo{
		MetadataInfo: &distributedpb.MetadataInfo{
			MetadataFields: []metadatapb.MetadataType{
				metadatapb.CONTAINER_ID,
				metadatapb.POD_NAME,
			},
			Filter: &distributedpb.MetadataInfo_XXHash64BloomFilter{
				XXHash64BloomFilter: &bloomfilterpb.XXHash64BloomFilter{
					Data:      []byte("1234"),
					NumHashes: 4,
				},
			},
		},
	}

	// Register a new agt.
	_, err = agtMgr.RegisterAgent(newAgentInfo)
	assert.Equal(t, nil, err)

	schema2 := new(storepb.TableInfo)
	if err := proto.UnmarshalText(testutils.SchemaInfo2PB, schema2); err != nil {
		t.Fatal("Cannot Unmarshal protobuf.")
	}

	// Update data info on agent #2.
	agentUpdate := agent.Update{
		UpdateInfo: &messagespb.AgentUpdateInfo{
			Schema:           []*storepb.TableInfo{schema2},
			ProcessCreated:   []*k8s_metadatapb.ProcessCreated{},
			Data:             oldAgentDataInfo,
			DoesUpdateSchema: true,
		},
		AgentID: agUUID2,
	}
	agtMgr.ApplyAgentUpdate(&agentUpdate)

	// Check results of first call to GetAgentUpdates.
	updates, schema, err = agtMgr.GetAgentUpdates(cursor)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(updates))
	assert.Equal(t, agUUID3, utils.UUIDFromProtoOrNil(updates[0].AgentID))
	assert.Equal(t, newAgentInfo.Info, updates[0].GetAgent().Info)
	assert.Equal(t, agUUID2, utils.UUIDFromProtoOrNil(updates[1].AgentID))
	assert.Equal(t, oldAgentDataInfo, updates[1].GetDataInfo())
	assert.Equal(t, 2, len(schema.Tables))

	// Update the heartbeat of an agt.
	err = agtMgr.UpdateHeartbeat(agUUID2)
	assert.Nil(t, err)

	// Now expire it
	err = agtMgr.DeleteAgent(agUUID0)
	assert.Nil(t, err)
	err = agtMgr.DeleteAgent(agUUID1)
	assert.Nil(t, err)

	// Check results of second call to GetAgentUpdates.
	updates, schema, err = agtMgr.GetAgentUpdates(cursor)
	assert.Nil(t, err)
	assert.Nil(t, schema)
	assert.Equal(t, 3, len(updates))
	assert.Equal(t, agUUID2, utils.UUIDFromProtoOrNil(updates[0].AgentID))
	assert.NotNil(t, updates[0].GetAgent())
	assert.Equal(t, agUUID0, utils.UUIDFromProtoOrNil(updates[1].AgentID))
	assert.True(t, updates[1].GetDeleted())
	assert.Equal(t, agUUID1, utils.UUIDFromProtoOrNil(updates[2].AgentID))
	assert.True(t, updates[2].GetDeleted())

	agtMgr.DeleteAgentUpdateCursor(cursor)
	// This should throw an error because the cursor has been deleted.
	updates, schema, err = agtMgr.GetAgentUpdates(cursor)
	assert.NotNil(t, err)
}

func TestAgent_UpdateConfig(t *testing.T) {
	_, agtMgr, nc, cleanup := setupManager(t)
	defer cleanup()

	var wg sync.WaitGroup
	wg.Add(1)

	adsub, err := nc.Subscribe("Agent/"+testutils.ExistingAgentUUID, func(msg *nats.Msg) {
		vzMsg := &messagespb.VizierMessage{}
		proto.Unmarshal(msg.Data, vzMsg)
		req := vzMsg.GetConfigUpdateMessage().GetConfigUpdateRequest()
		assert.NotNil(t, req)
		assert.Equal(t, "gprof", req.Key)
		assert.Equal(t, "true", req.Value)
		wg.Done()
	})
	assert.Nil(t, err)
	defer adsub.Unsubscribe()

	err = agtMgr.UpdateConfig("pl", "pem-existing", "gprof", "true")
	assert.Nil(t, err)

	defer wg.Wait()
}