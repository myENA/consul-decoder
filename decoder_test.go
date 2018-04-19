package decoder

import (
	"encoding/json"
	"fmt"
	consulapi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/stretchr/testify/suite"
	"net"
	"testing"
	"time"
)

const prefix = "testing/decoder"

type CBConfig struct {
	BackupReportLogsExpiry int
	BucketName             string `decoder:"bucket-name"`
	Password               string `decoder:"password"`
}

type RadosConfig struct {
	UserPrefix string `cupate:"userPrefix"`
}

type TestStruct struct {
	Field1 string `decoder:"field1" json:"field1"`
	Field2 string `decoder:"field2" json:"field2"`
}
type tbConfig struct {
	ApiPort              int               `decoder:"apiPort"`
	Couchbase            *CBConfig         `decoder:"couchbase"`
	PrivateHostName      string            `decoder:"privateHostName"`
	PrivatePort          int               `decoder:"privatePort"`
	PoductName           string            `decoder:"productName"`
	PublishedPrivatePort int               `decoder:"publishedPrivatePort"`
	PublishedTunnelPort  int               `decoder:"publishedTunnelPort"`
	Rados                map[string]string `decoder:"radosgw"`
	TestArray            []*TestStruct     `decoder:"testarray"`
	TestInlineArray      []*TestStruct     `decoder:"testInlineArray,json"`
	TestByteSlice        []byte            `decoder:"testbyteslice"`
	TestDuration         time.Duration     `decoder:"testDuration"`
	TestIP               net.IP            `decoder:"testIP"`
	TestMask             net.IPMask        `decoder:"testMask"`
	TestSliceString      []string          `decoder:"testslicestring,json"`
	NoTag                string
	IgnoreMe             string `decoder:"-"`
	ImSpecial            string
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
	})

	kvs, _, err := client.KV().List(prefix, nil)
	es.Assert().Nil(err, "Unable to list keys: \"%s\"", err)

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
	es.T().Log(string(payload))
	es.T().Logf("netmask %s", tbc.TestMask.String())
}
