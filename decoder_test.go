package decoder

import (
	"bytes"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
)

const prefix = "testing"

type (
	TestStruct struct {
		Field1 string `decoder:"field1" json:"field1"`
		Field2 string `decoder:"field2" json:"field2"`
	}

	TestLevel1 struct {
		Uint   uint
		Int    int
		Level2 *TestLevel2
	}

	TestLevel2 struct {
		Uint   uint64
		Int    int64
		Level3 *TestLevel3
	}

	TestLevel3 struct {
		Uint uint32
		Int  int32
	}
)

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

type (
	TestNestedJSONStructValue struct {
		Field1 string
		Field2 map[string]interface{}
	}
	TestNestedJSONStruct struct {
		String string
		Value  TestNestedJSONStructValue `decoder:"Value,json"`
	}
)

type (
	TestReusedStruct struct {
		Field string
	}

	TestReusesStruct struct {
		S1 TestReusedStruct
		S2 TestReusedStruct
	}
)

type tbConfig struct {
	TestInlineArray     []*TestStruct  `decoder:"testInlineArray"`
	TestInlineArray2    *[]*TestStruct `decoder:"testInlineArray2"`
	TestMapStringStruct map[string]*TestStruct
	TestMapStringString map[string]string

	TestByteSlice   []byte        `decoder:"testbyteslice"`
	TestDuration    time.Duration `decoder:"testDuration"`
	TestSliceString []string      `decoder:"testslicestring,json"`
	Duration        time.Duration
	IPV4            net.IP
	IPV6            net.IP
	TestMask        net.IPMask `decoder:"testMask"`
	TestNestedValue string     `decoder:"im/several/levels/deep/testnestedvalue"`

	TestTextUnmarshaler *TestTextUnmarshaler

	TestBool bool

	L1 *TestLevel1

	NoTag           string
	IgnoreMe        string `decoder:"-"`
	ImSpecial       string
	TestSpaceSepStr []string   `decoder:",ssv"`
	TestCommaSepStr *[]*string `decoder:",csv"`
	TestSpaceSepInt []int      `decoder:",ssv"`
	TestCommaSepInt []int      `decoder:",csv"`

	JSONStruct TestNestedJSONStruct `decoder:"testJsonStruct"`

	ReusesStruct TestReusesStruct `decoder:"testReusesStruct"`
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

func seedKV(t *testing.T, client *consulapi.Client, key, value string) {
	if _, err := client.KV().Put(&consulapi.KVPair{Key: fmt.Sprintf("%s/%s", prefix, key), Value: []byte(value)}, nil); err != nil {
		t.Logf("Unable to put key %q: %s", key, err)
	}
}

// todo: make smarter

type assertThis interface {
	Assert(t *testing.T, actual interface{}) error
}

type valueIs struct {
	expected interface{}
}

func (vi *valueIs) Assert(t *testing.T, actual interface{}) error {
	// todo: probably needs improvement
	if vi.expected == nil {
		if actual == nil || reflect.ValueOf(actual).IsNil() {
			return nil
		}
		return fmt.Errorf("failed to assert exected nil matched actual: %v", actual)
	}

	et := reflect.TypeOf(vi.expected)
	at := reflect.TypeOf(actual)

	if !et.Comparable() {
		t.Fatalf("Provided non-comparable \"expected\" value: %+v", vi.expected)
	}

	if et.Kind() != at.Kind() {
		return fmt.Errorf("failed to assert expected %v matches actual: %v", vi.expected, actual)
	}

	if reflect.ValueOf(vi.expected).Interface() != reflect.ValueOf(actual).Interface() {
		return fmt.Errorf("failed to assert expected %v matches actual: %v", vi.expected, actual)
	}

	return nil
}

type lenIs struct {
	expected int
}

func (li *lenIs) Assert(t *testing.T, actual interface{}) error {
	// todo: handle pointers
	at := reflect.TypeOf(actual)

	if at.Kind() == reflect.Ptr {
		at = reflect.TypeOf(at.Elem())
	}

	switch at.Kind() {
	case reflect.Array:
		al := at.Len()
		if li.expected != al {
			return fmt.Errorf("failed to assert expected len %d matched actual: %d", li.expected, al)
		}
	case reflect.Slice:
		al := reflect.ValueOf(actual).Len()
		if li.expected != al {
			return fmt.Errorf("failed to assert expected len %d matched actual: %d", li.expected, al)
		}
	case reflect.Map:
		al := len(reflect.ValueOf(actual).MapKeys())
		if li.expected != al {
			return fmt.Errorf("failed to assert expected len %d matched actual: %d", li.expected, al)
		}
	default:
		return fmt.Errorf("failed to assert expected len %d on actual: %+v", li.expected, actual)
	}

	return nil
}

type isTrue uint8

func (isTrue) Assert(t *testing.T, actual interface{}) error {
	if b, ok := actual.(bool); !ok || !b {
		return fmt.Errorf("failed to assert expected value %t matched actual: %+v", true, actual)
	}
	return nil
}

type isType struct {
	expected interface{}
}

func (it *isType) Assert(t *testing.T, actual interface{}) error {
	// todo: probably needs improvement
	if it.expected == nil {
		if actual == nil || reflect.ValueOf(actual).IsNil() {
			return nil
		}
		return fmt.Errorf("failed to assert exected nil matched actual: %v", actual)
	}
	et := reflect.TypeOf(it.expected)
	at := reflect.TypeOf(actual)
	if et != at {
		return fmt.Errorf("failed to assert that expected type %s matched actual: %s", et, at)
	}
	return nil
}

func TestUnmarshal(t *testing.T) {
	server, clientConfig := makeServerAndClientConfig(t, nil)

	client, err := consulapi.NewClient(clientConfig)
	if err != nil {
		t.Logf("Unable to create consul client: %s", err)
		t.FailNow()
	}

	var (
		kvs consulapi.KVPairs
		dec = &Decoder{
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
	)

	t.Run("Seed", func(t *testing.T) {
		seeds := []struct {
			key   string
			value string
		}{
			{"notag", "i should exist"},
			{"ignoreme", "i should not exist"},
			{"im-special", "super duper special"},
			{"testslicestring", "[\"foo\",\"bar\",\"baz\"]"},
			{"testInlineArray/one/field1", "field1rec1"},
			{"testInlineArray/one/field2", "field2rec1"},
			{"testInlineArray/two/field1", "field1rec2"},
			{"testInlineArray/two/field2", "field2rec2"},
			{"testInlineArray2/one/field1", "field1p2rec1"},
			{"testInlineArray2/one/field2", "field2p2rec1"},
			{"testInlineArray2/two/field1", "field1p2rec2"},
			{"testInlineArray2/two/field2", "field2p2rec2"},
			{"testMapStringStruct/key1/field1", "msskey1field1val"},
			{"testMapStringStruct/key1/field2", "msskey1field2val"},
			{"testMapStringStruct/key2/field1", "msskey2field1val"},
			{"testMapStringStruct/key2/field2", "msskey2field2val"},
			{"testMapStringStruct/key3/field1", "msskey3field1val"},
			{"testMapStringStruct/key3/field2", "msskey3field2val"},
			{"testmapstringstring/key1", "value1"},
			{"testmapstringstring/key2", "value2"},
			{"testtextunmarshaler", "val1:val2"},
			{"duration", "30s"},
			{"ipv4", "1.2.3.4"},
			{"ipv6", "::1"},
			{"testMask", "255.255.255.0"},

			{"im/several/levels/deep/testnestedvalue", "nestisthebest"},

			{"testbool", "true"},

			{"l1/uint", "1"},
			{"l1/int", "-2"},
			{"l1/level2/uint", "3"},
			{"l1/level2/int", "-4"},
			{"l1/level2/level3/uint", "5"},
			{"l1/level2/level3/int", "-6"},
			{"testspacesepstr", "one two three"},
			{"testcommasepstr", "\"three, with embedded comma \"\"and quotes\"\"\",four,five"},
			{"testspacesepint", "1 2 3"},
			{"testcommasepint", "6,7,8"},

			{"testJsonStruct/string", "string"},
			{"testJsonStruct/Value", `{"field1":"value","field2": {"map1":"value1","map2":["value2"]}}`},

			{"testReusesStruct/s1/field", "value1"},
			{"testReusesStruct/s2/field", "value2"},
		}

		for _, seed := range seeds {
			seedKV(t, client, seed.key, seed.value)
		}
	})

	t.Run("Fetch", func(t *testing.T) {
		var err error
		if kvs, _, err = client.KV().List(prefix, nil); err != nil {
			t.Logf("Unable to list keys: %s", err)
			t.FailNow()
		} else {
			t.Log("kvs")
			for _, kv := range kvs {
				t.Logf("%s => %s", kv.Key, string(kv.Value))
			}
		}
	})

	t.Run("Unmarshal", func(t *testing.T) {
		tbc := &tbConfig{}
		err = dec.Unmarshal(prefix, kvs, tbc)
		if err != nil {
			t.Fatalf("shit: %s", err)
		}

		ipv4 := net.ParseIP("1.2.3.4")
		ipv6 := net.ParseIP("::1")

		netmask := net.IPMask(net.ParseIP("255.255.255.0"))

		tests := []struct {
			asserter assertThis
			value    interface{}
			msg      []interface{}
		}{
			{&valueIs{""}, tbc.IgnoreMe, nil},
			{&valueIs{"super duper special"}, tbc.ImSpecial, nil},
			{&valueIs{"i should exist"}, tbc.NoTag, nil},
			{&lenIs{3}, tbc.TestSliceString, nil},
			{&valueIs{"value1"}, tbc.TestMapStringString["key1"], nil},
			{&valueIs{"value2"}, tbc.TestMapStringString["key2"], nil},
			{&lenIs{2}, tbc.TestInlineArray, nil},
			{&valueIs{"field1rec1"}, tbc.TestInlineArray[0].Field1, nil},
			{&valueIs{"field2rec1"}, tbc.TestInlineArray[0].Field2, nil},
			{&valueIs{"field1rec2"}, tbc.TestInlineArray[1].Field1, nil},
			{&valueIs{"field2rec2"}, tbc.TestInlineArray[1].Field2, nil},
			{&lenIs{2}, *tbc.TestInlineArray2, nil},
			{&valueIs{"field1p2rec1"}, (*tbc.TestInlineArray2)[0].Field1, nil},
			{&valueIs{"field2p2rec1"}, (*tbc.TestInlineArray2)[0].Field2, nil},
			{&valueIs{"field1p2rec2"}, (*tbc.TestInlineArray2)[1].Field1, nil},
			{&valueIs{"field2p2rec2"}, (*tbc.TestInlineArray2)[1].Field2, nil},
			{&lenIs{3}, tbc.TestMapStringStruct, nil},
			{&valueIs{"msskey1field1val"}, tbc.TestMapStringStruct["key1"].Field1, nil},
			{&valueIs{"msskey1field2val"}, tbc.TestMapStringStruct["key1"].Field2, nil},
			{&valueIs{"msskey2field1val"}, tbc.TestMapStringStruct["key2"].Field1, nil},
			{&valueIs{"msskey2field2val"}, tbc.TestMapStringStruct["key2"].Field2, nil},
			{&valueIs{"msskey3field1val"}, tbc.TestMapStringStruct["key3"].Field1, nil},
			{&valueIs{"msskey3field2val"}, tbc.TestMapStringStruct["key3"].Field2, nil},
			{&valueIs{"val1"}, tbc.TestTextUnmarshaler.Field1, nil},
			{&valueIs{"val2"}, tbc.TestTextUnmarshaler.Field2, nil},
			{&valueIs{time.Second * 30}, tbc.Duration, nil},
			{new(isTrue), tbc.TestBool, nil},
			{new(isTrue), ipv4.Equal(tbc.IPV4), nil},
			{new(isTrue), ipv6.Equal(tbc.IPV6), nil},
			{new(isTrue), bytes.Equal(netmask, tbc.TestMask), []interface{}{netmask, tbc.TestMask}},
			{&valueIs{"nestisthebest"}, tbc.TestNestedValue, nil},
			{&valueIs{uint(1)}, tbc.L1.Uint, nil},
			{&valueIs{int(-2)}, tbc.L1.Int, nil},
			{&valueIs{uint64(3)}, tbc.L1.Level2.Uint, nil},
			{&valueIs{int64(-4)}, tbc.L1.Level2.Int, nil},
			{&valueIs{uint32(5)}, tbc.L1.Level2.Level3.Uint, nil},
			{&valueIs{int32(-6)}, tbc.L1.Level2.Level3.Int, nil},
			{&lenIs{3}, *tbc.TestCommaSepStr, nil},
			{&valueIs{`three, with embedded comma "and quotes"`}, *((*tbc.TestCommaSepStr)[0]), nil},
			{&lenIs{3}, tbc.TestSpaceSepStr, nil},
			{&lenIs{3}, tbc.TestSpaceSepInt, nil},
			{&lenIs{3}, tbc.TestCommaSepInt, nil},
			{&valueIs{"string"}, tbc.JSONStruct.String, nil},
			{&valueIs{"value"}, tbc.JSONStruct.Value.Field1, nil},
			{&isType{make(map[string]interface{})}, tbc.JSONStruct.Value.Field2, nil},
		}

		for _, test := range tests {
			if err := test.asserter.Assert(t, test.value); err != nil {
				t.Log(err.Error())
				if len(test.msg) > 0 {
					t.Log(test.msg...)
				}
				t.Fail()
			}
		}

	})

	server.Stop()

}
