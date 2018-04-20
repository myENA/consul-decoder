// Package decoder - this unmarshals or decodes values from a consul KV store
// into a struct.
// The following types are supported:
//
//     integer (int/int8/int16/int32/int64)
//
//     unsigned (uint/uint8/uint16/uint32/uint64)
//
//     float (float64/float32)
//
//     bool
//
//     time.Duration
//
//     net.IP
//
//     net.IPMask
//
//     struct - nested struct by default implies a consul folder with the same name.
//              if the tag modifier "json" is encountered, then the value of in the KV
//              is unmarshaled as json using json.Unmarshal
//
//     slice - the type can be most of the supported types, except another slice.
//
//     map - the key must be a string, the value can be anything but another map.
//
//     encoding.TextUnmarshaler - any type that implements this will have its
//                                UnmarshalText() method called.
//
// Struct tags
//
// By default, the decoder packages looks for the struct tag "decoder".
// However, this can be overridden inside the Decoder struct as shown below.
// For the purposes of examples, we'll stick with the default "decoder" tag.
// By default, in the absence of a decoder tag, it will look for a consul
// key name with the same name as the struct field.  Only exported struct
// fields are considered.  The name comparison is case-insensitive by default,
// but this is configurable in the Decoder struct.  the tag "-" indicates to
// skip the field.  The modifier ",json" appended to the end
// signals that the value is to be interpreted as json and unmarshaled rather
// than interpreted.
//
//     struct Foo {
//
//         // populate the value from key "whatever" into FooField1
//         FooField1 string `decoder:"whatever"`
//
//         // skip populating FooField2, "-" signals skip
//         FooField2 string `decoder:"-"`
//
//         // this looks for a folder named FooField3
//         // and maps keys inside to the keys / values of the map.  string
//         // is the only valid key type, though the map value can be most any
//         // of the other types supported by this package.  Notable exception
//         // map, as nested maps are not allowed.  You can, however, have a
//         // map[string]SomeStruct.
//         FooField3 map[string]string
//
//         // this looks for a folder named FooField4 (case insensitive)
//         // this is similar to the map example above, but it ignores the keys
//         // inside and maps the values in-order into the slice.  nested slices
//         // are not allowed, i.e., [][]string.
//         FooField4 []string
//
//         // this interprets the value of foofield5 as json data and
//         // will send it to json.Unmarshal from encoding/json package.
//         FooField5 *SomeStruct `decoder:"foofield5,json"`
//
//         // this expects there to be a consul folder foofield6 and that the
//         // keys within will correspond to the fields inside SomeStruct type.
//         FooField6 *SomeStruct `decoder:"foofield6"`
//
//
//    }
package decoder
