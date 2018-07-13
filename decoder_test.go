package decoder

import (
	"encoding/json"
	"fmt"
	consulapi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/stretchr/testify/suite"
	"net"
	"strings"
	"testing"
	"time"
)

const prefix = "testing"

type TestStruct struct {
	Field1 string `decoder:"field1" json:"field1"`
	Field2 string `decoder:"field2" json:"field2"`
}

type TestLevel1 struct {
	Uint   uint
	Int    int
	Level2 *TestLevel2
}

type TestLevel2 struct {
	Uint   uint64
	Int    int64
	Level3 *TestLevel3
}

type TestLevel3 struct {
	Uint uint32
	Int  int32
}

type TestTextUnmarshaler struct {
	Field1 string
	Field2 string
}

func (ttu *TestTextUnmarshaler) UnmarshalText(text []byte) error {
	bits := strings.Split(string(text), ":")
	if len(bits) != 2 {
		return fmt.Errorf("invalid field %s", string(text))
	}
	ttu.Field1 = bits[0]
	ttu.Field2 = bits[1]
	return nil
}

type tbConfig struct {
	TestInlineArray     []*TestStruct  `decoder:"testInlineArray"`
	TestInlineArray2    *[]*TestStruct `decoder:"testInlineArray2"`
	TestMapStringStruct map[string]*TestStruct
	TestMapStringString map[string]string

	TestByteSlice   []byte        `decoder:"testbyteslice"`
	TestDuration    time.Duration `decoder:"testDuration"`
	TestIP          net.IP        `decoder:"testIP"`
	TestMask        net.IPMask    `decoder:"testMask"`
	TestSliceString []string      `decoder:"testslicestring,json"`
	Duration        time.Duration
	IPV4            net.IP
	IPV6            net.IP
	TestNestedValue string `decoder:"im/several/levels/deep/testnestedvalue"`

	TestTextUnmarshaler *TestTextUnmarshaler

	TestBool bool

	L1 *TestLevel1

	NoTag     string
	IgnoreMe  string `decoder:"-"`
	ImSpecial string
}

func makeServer(t *testing.T, cb testutil.ServerConfigCallback) *testutil.TestServer {
	server, err := testutil.NewTestServerConfig(cb)
	if nil != err {
		t.Fatalf("Unable to initialize Consul agent server: %v", err)
	}

	return server
}

func makeClientConfig(_ *testing.T, server *testutil.TestServer) *consulapi.Config {
	return &consulapi.Config{Address: server.HTTPAddr}
}

func makeServerAndClientConfig(t *testing.T, cb testutil.ServerConfigCallback) (*testutil.TestServer, *consulapi.Config) {
	server := makeServer(t, cb)
	return server, makeClientConfig(t, server)
}

type encoderTestSuite struct {
	suite.Suite
	server       *testutil.TestServer
	clientConfig *consulapi.Config
}

func TestEncoder(t *testing.T) {
	suite.Run(t, new(encoderTestSuite))
}

func (es *encoderTestSuite) SetupTest() {
	// This will cause test to fail internally, no need to explicitly test.

	es.server, es.clientConfig = makeServerAndClientConfig(es.T(), nil)
}

func (es *encoderTestSuite) TearDownTest() {
	if es.server != nil {
		es.server.Stop()
		es.server = nil
	}
	if es.clientConfig != nil {
		es.clientConfig = nil
	}
}

func (es *encoderTestSuite) seed(client *consulapi.Client, key, value string) {
	var err error
	_, err = client.KV().Put(&consulapi.KVPair{Key: fmt.Sprintf("%s/%s", prefix, key), Value: []byte(value)}, nil)
	es.Assert().Nil(err, "unable to put key \"%s\": %s", key, err)
}

func (es *encoderTestSuite) TestUnmarshal() {

	client, err := consulapi.NewClient(es.clientConfig)
	es.Assert().Nil(err, "Unable to create consul client: %s", err)

	es.T().Run("Seed", func(t *testing.T) {

		es.seed(client, "notag", "i should exist")
		es.seed(client, "ignoreme", "i should not exist")
		es.seed(client, "im-special", "super duper special")
		es.seed(client, "testslicestring", "[\"foo\",\"bar\",\"baz\"]")
		es.seed(client, "testInlineArray/one/field1", "field1rec1")
		es.seed(client, "testInlineArray/one/field2", "field2rec1")
		es.seed(client, "testInlineArray/two/field1", "field1rec2")
		es.seed(client, "testInlineArray/two/field2", "field2rec2")
		es.seed(client, "testInlineArray2/one/field1", "field1p2rec1")
		es.seed(client, "testInlineArray2/one/field2", "field2p2rec1")
		es.seed(client, "testInlineArray2/two/field1", "field1p2rec2")
		es.seed(client, "testInlineArray2/two/field2", "field2p2rec2")
		es.seed(client, "testMapStringStruct/key1/field1", "msskey1field1val")
		es.seed(client, "testMapStringStruct/key1/field2", "msskey1field2val")
		es.seed(client, "testMapStringStruct/key2/field1", "msskey2field1val")
		es.seed(client, "testMapStringStruct/key2/field2", "msskey2field2val")
		es.seed(client, "testMapStringStruct/key3/field1", "msskey3field1val")
		es.seed(client, "testMapStringStruct/key3/field2", "msskey3field2val")
		es.seed(client, "testmapstringstring/key1", "value1")
		es.seed(client, "testmapstringstring/key2", "value2")
		es.seed(client, "testtextunmarshaler", "val1:val2")
		es.seed(client, "duration", "30s")
		es.seed(client, "ipv4", "1.2.3.4")
		es.seed(client, "ipv6", "::1")

		es.seed(client, "im/several/levels/deep/testnestedvalue", "nestisthebest")

		es.seed(client, "testbool", "true")

		es.seed(client, "l1/uint", "1")
		es.seed(client, "l1/int", "-2")
		es.seed(client, "l1/level2/uint", "3")
		es.seed(client, "l1/level2/int", "-4")
		es.seed(client, "l1/level2/level3/uint", "5")
		es.seed(client, "l1/level2/level3/int", "-6")

	})

	kvs, _, err := client.KV().List(prefix, nil)
	es.Assert().Nil(err, "Unable to list keys: \"%s\"", err)

	es.T().Log("kvs")
	for _, kv := range kvs {
		es.T().Logf("%s => %s", kv.Key, string(kv.Value))

	}

	decoder := &Decoder{
		NameResolver: func(f, t string) string {
			if f == "ImSpecial" {
				return "im-special"
			} else if t != "" {
				return t
			} else {
				return f
			}
		},
	}

	tbc := &tbConfig{}
	err = decoder.Unmarshal(prefix, kvs, tbc)
	es.Assert().Nil(err, "unable to unmarshal: %s", err)

	es.T().Log("keys ------")
	for k := range typeCache.typeNameMetaMap {
		es.T().Logf("%s\n", k)
	}

	payload, err := json.MarshalIndent(tbc, "", "    ")
	es.Assert().Nil(err, "error from marshal: %s", err)

	es.Assert().Equal("", tbc.IgnoreMe)
	es.Assert().Equal("super duper special", tbc.ImSpecial)
	es.Assert().Equal("i should exist", tbc.NoTag)
	es.Assert().Len(tbc.TestSliceString, 3, "length not correct: %#v", tbc.TestSliceString)
	es.Assert().Equal(tbc.TestMapStringString["key1"], "value1")
	es.Assert().Equal(tbc.TestMapStringString["key2"], "value2")
	es.Assert().Len(tbc.TestInlineArray, 2)
	es.Assert().Equal(tbc.TestInlineArray[0].Field1, "field1rec1")
	es.Assert().Equal(tbc.TestInlineArray[0].Field2, "field2rec1")
	es.Assert().Equal(tbc.TestInlineArray[1].Field1, "field1rec2")
	es.Assert().Equal(tbc.TestInlineArray[1].Field2, "field2rec2")
	es.Assert().Len(*tbc.TestInlineArray2, 2)
	es.Assert().Equal((*tbc.TestInlineArray2)[0].Field1, "field1p2rec1")
	es.Assert().Equal((*tbc.TestInlineArray2)[0].Field2, "field2p2rec1")
	es.Assert().Equal((*tbc.TestInlineArray2)[1].Field1, "field1p2rec2")
	es.Assert().Equal((*tbc.TestInlineArray2)[1].Field2, "field2p2rec2")
	es.Assert().Len(tbc.TestMapStringStruct, 3)
	es.Assert().Equal(tbc.TestMapStringStruct["key1"].Field1, "msskey1field1val")
	es.Assert().Equal(tbc.TestMapStringStruct["key1"].Field2, "msskey1field2val")
	es.Assert().Equal(tbc.TestMapStringStruct["key2"].Field1, "msskey2field1val")
	es.Assert().Equal(tbc.TestMapStringStruct["key2"].Field2, "msskey2field2val")
	es.Assert().Equal(tbc.TestMapStringStruct["key3"].Field1, "msskey3field1val")
	es.Assert().Equal(tbc.TestMapStringStruct["key3"].Field2, "msskey3field2val")

	es.Assert().Equal(tbc.TestTextUnmarshaler.Field1, "val1")
	es.Assert().Equal(tbc.TestTextUnmarshaler.Field2, "val2")

	es.Assert().Equal(tbc.Duration, time.Second*30)
	es.Assert().True(tbc.TestBool)
	ipv4 := net.ParseIP("1.2.3.4")
	ipv6 := net.ParseIP("::1")

	es.Assert().True(ipv4.Equal(tbc.IPV4))
	es.Assert().True(ipv6.Equal(tbc.IPV6))

	es.Assert().Equal("nestisthebest", tbc.TestNestedValue)
	es.Assert().Equal(tbc.L1.Uint, uint(1))
	es.Assert().Equal(tbc.L1.Int, int(-2))
	es.Assert().Equal(tbc.L1.Level2.Uint, uint64(3))
	es.Assert().Equal(tbc.L1.Level2.Int, int64(-4))
	es.Assert().Equal(tbc.L1.Level2.Level3.Uint, uint32(5))
	es.Assert().Equal(tbc.L1.Level2.Level3.Int, int32(-6))

	es.T().Log(string(payload))
	es.T().Logf("netmask %s", tbc.TestMask.String())
}
