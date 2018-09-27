package decoder

import (
	"bytes"
	"encoding"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"strings"

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
	sJSON
	sCSV
	sSSV
)
const (
	tagJSON = "json"
	tagCSV  = "csv"
	tagSSV  = "ssv"
	defTag  = "decoder"
)

var (
	// InvalidValueErr - this is returned if we don't pass an appropriate
	// type to Decode() or Unmarshal()
	InvalidValueErr = errors.New("invalid value passed: must be a non-nil pointer to a struct")

	textUnmarshalerType = reflect.TypeOf(new(encoding.TextUnmarshaler)).Elem()
	typeCache           = typeCacheManager{typeNameMetaMap: make(map[string]*tMeta)}
	defaultDecoder      = &Decoder{CaseSensitive: false, NameResolver: defaultNameResolver, Tag: defTag}
)

type (
	// NameResolverFunc - this allows us to define a custom
	// name resolution to override the default.
	NameResolverFunc func(field, tag string) (key string)

	// Decoder - define one of these if you want to override
	// default behavior.  Otherwise just use Unmarshal()
	Decoder struct {
		// If true, then field names must match key exactly.
		CaseSensitive bool
		// Be responsible, don't change this after being set.
		NameResolver NameResolverFunc
		// The struct tag to parse.  defaults to "decoder"
		Tag string
	}
)

// This does our first pass over the struct type to gather metadata.
func (d *Decoder) parseStruct(st reflect.Type) (*tMeta, error) {

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
				debugPrintf("Unmarshal() - Key: %s, tfm: %+v", k, tfm)
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

func (d *Decoder) allocAssign(tfm *tFieldMeta, thisPair *api.KVPair, rest *api.KVPairs, val reflect.Value, prefix string) error {
	tval := val
	for _, loc := range tfm.locators {
		debugPrintf("allocAssign() - Locator:  %+v", loc)
		if tval.NumField() <= loc.ind {
			logPrintf("allocAssign() - Panic about to happen: Type %q only has %d fields, asking for field index %d", tval.Type(), tval.NumField(), loc.ind)
		}
		fv := tval.Field(loc.ind)
		if loc.isSlice || loc.isMap {
			var st reflect.Value // st will hold a reference to loc.ttype
			if tfm.computedType == typeStruct || !tfm.isNotSpecial() {
				st = reflect.New(loc.ttype)
				newprefix := path.Join(prefix, tfm.fieldName) + "/"
				key := thisPair.Key
				if !d.CaseSensitive {
					key = strings.ToLower(key)
					newprefix = strings.ToLower(newprefix)
				}
				ind := strings.TrimPrefix(key, newprefix)
				pathparts := strings.Split(ind, "/")
				newprefix = path.Join(newprefix, pathparts[0]) + "/"
				if tfm.isJSON() {
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

			if loc.collPtrCt == 0 && tfm.isNotSpecial() {
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
			if tfm.isJSON() {
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
