package decoder

import (
	"bytes"
	"encoding"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
)

type (
	computedType int
	special      int
)

const (
	typeStruct computedType = iota
	typeInt
	typeDuration
	typeUint
	typeFloat
	typeString
	typeBool
	typeByteSlice
	typeNetIP
	typeNetMask
	typeTextUnmarshaler
)

// reset iota
const (
	sNone special = iota
	sCSV
	sSSV
)
const (
	tagJSON = "json"
	tagCSV  = "csv"
	tagSSV  = "ssv"
	defTag  = "decoder"
)

var textUnmarshalerType = reflect.TypeOf(new(encoding.TextUnmarshaler)).Elem()

var typeCache = typeCacheManager{typeNameMetaMap: make(map[string]*tMeta)}

type typeCacheManager struct {
	lck             sync.RWMutex
	typeNameMetaMap map[string]*tMeta
}

type tMeta struct {
	tFieldsMetaMap map[string]*tFieldMeta
}

type tFieldMeta struct {
	// locators gives us a way to locate
	// fields, even if they're nested several levels
	// deep within a structure.
	locators []tFieldLocator

	fieldName string

	// computedType distills the type that the locators refers to,
	// will be one of the type* constants defined above.
	computedType computedType

	// This is used to capture "special" considerations, currently CSV
	// and SSV (space separated values).
	special special
}

func (tfm *tFieldMeta) isCSV() bool {
	return tfm.special == sCSV
}

func (tfm *tFieldMeta) isSSV() bool {
	return tfm.special == sSSV
}

func (tfm *tFieldMeta) isNotSpecial() bool {
	return tfm.special == sNone
}

func (tfm *tFieldMeta) isSpecial() bool {
	return !tfm.isNotSpecial()
}

// NameResolverFunc - this allows us to define a custom
// name resolution to override the default.
type NameResolverFunc func(field, tag string) (key string)

// Decoder - define one of these if you want to override
// default behavior.  Otherwise just use Unmarshal()
type Decoder struct {
	// If true, then field names must match key exactly.
	CaseSensitive bool
	// Be responsible, don't change this after being set.
	NameResolver NameResolverFunc
	// The struct tag to parse.  defaults to "decoder"
	Tag string
}

func defaultNameResolver(field, tag string) string {
	if tag != "" {
		return tag
	}
	return field
}

var defaultDecoder = &Decoder{CaseSensitive: false, NameResolver: defaultNameResolver, Tag: defTag}

type tFieldLocator struct {
	// ind is passed to type.Field()
	ind int

	// ptrCt is the number of pointers to the type
	ptrCt uint8

	// collPtrCt is the number of pointers on the other side
	// of the [] in a slice or a map[string] in a map
	collPtrCt uint8

	isSlice bool
	isMap   bool
	isJSON  bool

	// The actual type of the thing, after all pointers
	// are derefed.
	ttype reflect.Type
}

func (tcm *typeCacheManager) tMeta(d *Decoder, t reflect.Type, lock bool) (*tMeta, error) {
	// TODO this probably shouldn't lock the world.
	if lock {
		tcm.lck.Lock()
		defer tcm.lck.Unlock()
	}
	tk := typeKey(t)
	if tk == "" {
		return nil, fmt.Errorf("type cannot be determined")
	}
	if tm, ok := tcm.typeNameMetaMap[tk]; ok {
		return tm, nil
	}
	tm, err := d.parseStruct(t)
	if err != nil {
		return nil, err
	}
	tcm.typeNameMetaMap[tk] = tm
	return tm, nil
}

func typeKey(t reflect.Type) string {
	pp := t.PkgPath()
	pn := t.Name()
	if len(pp) > 0 && len(pn) > 0 {
		return pp + "." + pn
	}
	return pn
}

// This does our first pass over the struct type to gather metadata.
func (d *Decoder) parseStruct(st reflect.Type) (*tMeta, error) {

	if st.Kind() != reflect.Struct {
		return nil, fmt.Errorf("type is not struct: %s", st.Kind().String())
	}

	var fullTag, fieldName, tagName string
	var tagBits []string
	var tagLen int

	tagLabel := defTag
	if d.Tag != "" {
		tagLabel = d.Tag
	}

	tm := &tMeta{tFieldsMetaMap: make(map[string]*tFieldMeta)}

fieldLoop:
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)

		// Skip unexported fields.  See
		// http://golang.org/pkg/reflect/#StructField for why this works.
		// also https://github.com/golang/go/issues/12367
		if f.PkgPath != "" && !f.Anonymous {
			continue
		}

		tfm := &tFieldMeta{
			locators: []tFieldLocator{{ind: i}},
		}

		// make a shortcut for referencing the locator at the top of the stack.
		topLoc := &(tfm.locators[0])

		// Decode tags.
		fullTag = f.Tag.Get(tagLabel)
		tagBits = strings.Split(fullTag, ",")
		tagLen = len(tagBits)

		fieldName = f.Name
		if tagLen > 0 {
			tagName = tagBits[0]
		} else {
			tagName = ""
		}

		if d.NameResolver == nil {
			tfm.fieldName = defaultNameResolver(fieldName, tagName)
		} else {
			tfm.fieldName = d.NameResolver(fieldName, tagName)
		}

		if tfm.fieldName == "-" || tfm.fieldName == "" {
			continue fieldLoop
		}

		if tagLen > 1 {
			for _, tv := range tagBits[1:] {
				switch tv {
				case tagJSON:
					topLoc.isJSON = true
				case tagCSV:
					tfm.special = sCSV
				case tagSSV:
					tfm.special = sSSV
				}
			}
		}

		if !d.CaseSensitive {
			tfm.fieldName = strings.ToLower(tfm.fieldName)
		}

		// Initialize t with the field type.
		t := f.Type

	Outer:
		for {
			// Reset ttype with each iteration of the loop.
			// Will change for pointers, slice types, map types
			topLoc.ttype = t
			if t.Implements(textUnmarshalerType) {
				tfm.computedType = typeTextUnmarshaler
			}
			switch t.Kind() {
			case reflect.Ptr:
				if topLoc.isMap || topLoc.isSlice {
					topLoc.collPtrCt++
					if topLoc.collPtrCt == 0 {
						// overflow
						return nil, fmt.Errorf("collection pointer count overflow detected")
					}
				} else {
					topLoc.ptrCt++
					if topLoc.ptrCt == 0 {
						// overflow
						return nil, fmt.Errorf("pointer depth overflow detected")
					}
				}
				t = t.Elem()
			case reflect.Array, reflect.Slice:
				if isByteSlice(t) {

					switch typeKey(t) {
					case "net.IP":
						tfm.computedType = typeNetIP
					case "net.IPMask":
						tfm.computedType = typeNetMask
					default:
						tfm.computedType = typeByteSlice
					}

					tm.tFieldsMetaMap[tfm.fieldName] = tfm
					break Outer
				}
				if topLoc.isSlice {
					return nil, fmt.Errorf("slices of slices not supported, except [][]byte")
				}
				topLoc.isSlice = true
				if topLoc.isJSON {
					tm.tFieldsMetaMap[tfm.fieldName] = tfm
					break Outer
				}
				t = t.Elem()
			case reflect.Map:
				if topLoc.isJSON {
					tm.tFieldsMetaMap[tfm.fieldName] = tfm
					break Outer
				}
				if topLoc.isMap {
					return nil, fmt.Errorf("maps to maps not supported")
				}
				if t.Key().Kind() != reflect.String {
					// Currently only support map[string]blah's
					return nil, fmt.Errorf(
						"invalid map key type %s for : %s only string map keys supported",
						t.Key().Kind().String(),
						tfm.fieldName,
					)
				}
				topLoc.isMap = true
				t = t.Elem()

			case reflect.Struct:
				if tfm.isCSV() || tfm.isSSV() {
					return nil, fmt.Errorf("cannot use a struct type with isSSV or isCSV")
				}
				if tfm.computedType != typeTextUnmarshaler {
					tfm.computedType = typeStruct
				}
				if topLoc.isMap || topLoc.isSlice || topLoc.isJSON || tfm.computedType == typeTextUnmarshaler {
					// no need to dive on these.  for maps and slices of structs,
					// they are handled later in the unmarshal phase.  For JSON or TextUnmarshalers,
					// we handle those with JSON and UnmarshalText() method calls respectively.
					tm.tFieldsMetaMap[tfm.fieldName] = tfm
					break Outer
				}

				// If we fall through here, recursively inspect the struct and
				// pull in its locators into our own, flattening the structure.
				embedded, err := typeCache.tMeta(d, t, false)
				if err != nil {
					return nil, err
				}

				for k, etfm := range embedded.tFieldsMetaMap {
					nk := path.Join(tfm.fieldName, k)

					// Make a shallow copy of etfm to isolate locators
					etfmcp := &tFieldMeta{}
					*etfmcp = *etfm

					// fix up copy's locators.
					etfmcp.locators = append(tfm.locators, etfm.locators...)

					tm.tFieldsMetaMap[nk] = etfmcp
				}

				break Outer
			case reflect.String,
				reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8,
				reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8,
				reflect.Float64, reflect.Float32, reflect.Bool:

				if tfm.computedType != typeTextUnmarshaler {
					if (tfm.isCSV() || tfm.isSSV()) && !topLoc.isSlice {
						return nil, fmt.Errorf("must use a slice of strings, ints, uints, floats or bools with isCSV or isSSV")
					}
					var cType computedType
					switch t.Kind() {
					case reflect.String:
						cType = typeString
					case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
						if typeKey(t) == "time.Duration" {
							cType = typeDuration
						} else {
							cType = typeInt
						}
					case reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8:
						cType = typeUint
					case reflect.Float64, reflect.Float32:
						cType = typeFloat
					case reflect.Bool:
						cType = typeBool
					}
					tfm.computedType = cType
				}
				tm.tFieldsMetaMap[tfm.fieldName] = tfm

				break Outer
			default:
				if tfm.computedType == typeTextUnmarshaler {
					tm.tFieldsMetaMap[tfm.fieldName] = tfm
				}
				break Outer
			}
		}
	}

	return tm, nil
}

// InvalidValueErr - this is returned if we don't pass an appropriate
// type to Decode() or Unmarshal()
var InvalidValueErr = errors.New("invalid value passed: must be a non-nil pointer to a struct")

// Unmarshal - uses the default decoder with default settings to decode
// the values from kvps at pathPrefix into v.
func Unmarshal(pathPrefix string, kvps api.KVPairs, v interface{}) error {
	return defaultDecoder.Unmarshal(pathPrefix, kvps, v)
}

// Unmarshal - this is the Unmarshal method on a custom decoder.  Same as above
// otherwise.
func (d *Decoder) Unmarshal(pathPrefix string, kvps api.KVPairs, v interface{}) error {
	valp := reflect.ValueOf(v)
	if valp.Kind() != reflect.Ptr {
		return InvalidValueErr
	}
	if valp.IsNil() {
		return InvalidValueErr
	}

	val := valp.Elem()
	if val.Kind() != reflect.Struct {
		return InvalidValueErr
	}

	meta, err := typeCache.tMeta(d, val.Type(), true)
	if err != nil {
		return err
	}

	if !strings.HasSuffix(pathPrefix, "/") {
		pathPrefix += "/"
	}

	for {
		if len(kvps) == 0 {
			break
		}

		kvp := kvps[0]
		kvps = kvps[1:]

		if strings.HasSuffix(kvp.Key, "/") {
			continue
		}

		key := kvp.Key
		if !d.CaseSensitive {
			key = strings.ToLower(key)
			pathPrefix = strings.ToLower(pathPrefix)
		}

		k := strings.TrimPrefix(key, pathPrefix)
		if pathPrefix != "" && k == key {
			continue // doesn't match what we're supposed to.  perhaps error?
		}

		for {
			if tfm, ok := meta.tFieldsMetaMap[k]; ok {
				err = d.allocAssign(tfm, kvp, &kvps, val, pathPrefix)
				if err != nil {
					return err
				}
				break
			}

			// Look for maps and slices
			k = path.Dir(k)
			if k == "." || k == "/" {
				break
			}
		}
	}

	return nil
}

func isByteSlice(t reflect.Type) bool {
	k := t.Kind()
	if k != reflect.Slice {
		return false
	}
	t = t.Elem()
	return t.Kind() == reflect.Uint8
}

func (d *Decoder) allocAssign(tfm *tFieldMeta, thisPair *api.KVPair, rest *api.KVPairs, val reflect.Value, prefix string) error {
	tval := val

	for _, loc := range tfm.locators {
		tk := typeKey(loc.ttype)
		_ = tk
		fv := tval.Field(loc.ind)
		if loc.isSlice || loc.isMap || loc.isJSON {
			var st reflect.Value // st will hold a reference to loc.ttype
			if tfm.computedType == typeStruct || tfm.isSpecial() {

				st = reflect.New(loc.ttype)
				newprefix := prefix
				if loc.isSlice || loc.isMap {
					newprefix = path.Join(prefix, tfm.fieldName) + "/"
				}
				key := thisPair.Key
				if !d.CaseSensitive {
					key = strings.ToLower(key)
					newprefix = strings.ToLower(newprefix)
				}
				ind := strings.TrimPrefix(key, newprefix)
				pathparts := strings.Split(ind, "/")
				newprefix = path.Join(newprefix, pathparts[0]) + "/"
				if loc.isJSON {
					err := json.Unmarshal(thisPair.Value, st.Interface())
					if err != nil {
						return err
					}
				} else if tfm.isCSV() || tfm.isSSV() {
					t := loc.ttype
					for i := uint8(0); i < loc.collPtrCt; i++ {
						t = reflect.PtrTo(t)
					}
					st = reflect.New(reflect.SliceOf(t))
				} else {
					// Process all the pairs related to this prefix.
					curatedPairs := api.KVPairs{thisPair}
					for i := 0; i < len(*rest); i++ {
						key := (*rest)[0].Key
						if !d.CaseSensitive {
							key = strings.ToLower(key)
							newprefix = strings.ToLower(newprefix)
						}
						if strings.HasPrefix(key, newprefix) {
							curatedPairs = append(curatedPairs, (*rest)[0])
							*rest = (*rest)[1:]
						} else {
							break
						}
					}
					err := d.Unmarshal(newprefix, curatedPairs, st.Interface())
					if err != nil {
						return err
					}
				}

			} else {
				var err error
				st, err = handleIntrinsicType(thisPair.Value, loc.ttype, tfm.computedType)
				if err != nil {
					return err
				}
				st = st.Addr()
			}

			// once here, st represents a pointer to a loc.ttype

			if loc.collPtrCt == 0 && !loc.isJSON && tfm.isNotSpecial() {
				// st is a pointer to stype, so we need to deref it.
				st = st.Elem()
			} else {
				// if collPtrCt > 1, as in []**Foo (who does that?)
				// create intermediate pointers to hold the pointers
				// to the pointers to the pointers to the pointers.
				for i := uint8(1); i < loc.collPtrCt; i++ {
					// st starts out a pointer, so st.Type() is *Type
					nst := reflect.New(st.Type())
					nst.Set(st.Addr())
					st = nst
				}
			}

			sfield := fv
			if loc.isJSON {
				if loc.ptrCt == 0 {
					st = st.Elem()
				}

				// if ptrCt > 1, process those.
				for i := uint8(1); i < loc.ptrCt; i++ {
					nst := reflect.New(st.Type())
					nst.Set(st.Addr())
					st = nst
				}
				sfield.Set(st)
				return nil
			}

			// if ptrCt is 0, this doesn't run.
			// otherwise we allocate sufficient pointers
			// to match the type.  as an example, ptrCt would
			// be 1 in the case of:
			// *[]Foo
			for i := uint8(0); i < loc.ptrCt; i++ {
				if sfield.IsNil() {
					// Create a new pointer to hold the address
					sfield.Set(reflect.New(sfield.Type().Elem()))
				}
				sfield = sfield.Elem()
			}
			if loc.isMap {
				if sfield.IsNil() {
					sfield.Set(reflect.MakeMap(sfield.Type()))
				}
				trimpath := path.Join(prefix, tfm.fieldName) + "/"
				key := thisPair.Key
				if !d.CaseSensitive {
					key = strings.ToLower(key)
					trimpath = strings.ToLower(trimpath)
				}

				key = strings.TrimPrefix(key, trimpath)

				splitKey := strings.Split(key, "/")

				sfield.SetMapIndex(reflect.ValueOf(splitKey[0]), st)
			} else { // slice
				handleFields := func(fields []string, loc tFieldLocator, tfm *tFieldMeta) ([]reflect.Value, error) {
					var vals []reflect.Value
					for _, field := range fields {
						v, err := handleIntrinsicType([]byte(field), loc.ttype, tfm.computedType)
						if err != nil {
							return nil, err
						}
						for i := uint8(0); i < loc.collPtrCt; i++ {
							vp := reflect.New(v.Type())
							vp.Elem().Set(v)
							v = vp
						}
						vals = append(vals, v)
					}
					return vals, nil
				}
				var (
					vals []reflect.Value
				)
				switch tfm.special {
				case sCSV:
					fields, err := csv.NewReader(bytes.NewReader(thisPair.Value)).Read()
					if err != nil {
						return err
					}
					vals, err = handleFields(fields, loc, tfm)
					if err != nil {
						return err
					}
				case sSSV:
					fields := strings.Fields(string(thisPair.Value))
					var err error
					vals, err = handleFields(fields, loc, tfm)
					if err != nil {
						return err
					}
				default:
					vals = []reflect.Value{st}
				}
				sfield.Set(reflect.Append(sfield, vals...))
			}
			return nil
		}

		// else not a map or slice.  burrow down
		// the chain creating intermediate stuff
		// as we go.
		for i := uint8(0); i < loc.ptrCt; i++ {
			if fv.IsNil() {
				fv.Set(reflect.New(fv.Type().Elem()))
			}
			fv = fv.Elem()
		}
		tval = fv
	}

	if tfm.computedType == typeTextUnmarshaler {
		tu := tval.Addr().Interface().(encoding.TextUnmarshaler)
		return tu.UnmarshalText(thisPair.Value)
	}

	v, err := handleIntrinsicType(thisPair.Value, tval.Type(), tfm.computedType)
	if err != nil {
		return err
	}
	tval.Set(v)

	return nil
}

func handleIntrinsicType(data []byte, ttype reflect.Type, cType computedType) (reflect.Value, error) {
	tval := reflect.New(ttype).Elem()
	switch cType {
	case typeInt:
		ival, err := strconv.ParseInt(string(data), 10, 64)
		if err != nil {
			return tval, err
		}
		tval.SetInt(ival)
	case typeUint:
		uival, err := strconv.ParseUint(string(data), 10, 64)
		if err != nil {
			return tval, err
		}
		tval.SetUint(uival)
	case typeFloat:
		fval, err := strconv.ParseFloat(string(data), 64)
		if err != nil {
			return tval, err
		}
		tval.SetFloat(fval)
	case typeString:
		tval.SetString(string(data))
	case typeByteSlice:
		tval.SetBytes(data)
	case typeBool:
		bval, err := strconv.ParseBool(string(data))
		if err != nil {
			return tval, err
		}
		tval.SetBool(bval)
	case typeDuration:
		dval, err := time.ParseDuration(string(data))
		if err != nil {
			return tval, err
		}
		tval.SetInt(int64(dval))
	case typeNetIP, typeNetMask:
		if len(data) == 0 {
			break
		}
		ipval := net.ParseIP(string(data))
		if ipval == nil {
			return tval, fmt.Errorf("invalid address: %s", string(data))
		}
		tval.SetBytes([]byte(ipval))

	default:
		// TODO: mention this...
		//return tval, fmt.Errorf("no support for %s types in this context", ttype)
	}

	return tval, nil
}
