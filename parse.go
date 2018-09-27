package decoder

import (
	"fmt"
	"path"
	"reflect"
	"strings"
)

func parseStructField(d *Decoder, rootField reflect.StructField, ft reflect.Type, tfm *tFieldMeta, tfl tFieldLocator) error {
	if ft == nil {
		ft = rootField.Type
	}
	if ft.Implements(textUnmarshalerType) {
		tfm.computedType = typeTextUnmarshaler
	}
	switch ft.Kind() {
	case reflect.Ptr:
		debugPrintf("parseStruct() - %s is a pointer", rootField.Name)
		if tfl.isMap || tfl.isSlice {
			tfl.collPtrCt++
			if tfl.collPtrCt == 0 {
				// overflow
				return fmt.Errorf("collection pointer count overflow detected")
			}
		} else {
			tfl.ptrCt++
			if tfl.ptrCt == 0 {
				// overflow
				return fmt.Errorf("pointer depth overflow detected")
			}
		}
		return parseStructField(d, rootField, ft.Elem(), tfm, tfl)
	case reflect.Array, reflect.Slice:
		debugPrintf("parseStruct() - %s is an array or slice", rootField.Name)
		if isByteSlice(ft) {
			switch typeKey(ft) {
			case "net.IP":
				tfm.computedType = typeNetIP
			case "net.IPMask":
				tfm.computedType = typeNetMask
			default:
				tfm.computedType = typeByteSlice
			}
			tfm.locators = []tFieldLocator{tfl}

			// TODO - refactor this, don't do this all over the loop
			tm.tFieldsMetaMap[tfm.fieldName] = tfm
			break Outer
		}
		if tfl.isSlice {
			return nil, fmt.Errorf("slices of slices not supported, except [][]byte")
		}
		tfl.isSlice = true
		if tfm.isJSON() {
			tfm.locators = []tFieldLocator{tfl}
			tm.tFieldsMetaMap[tfm.fieldName] = tfm
			break Outer
		}
		t = t.Elem()
	case reflect.Map:
		debugPrintf("parseStruct() - %s is a map", f.Name)
		if tfm.isJSON() {
			tfm.locators = []tFieldLocator{tfl}
			tm.tFieldsMetaMap[tfm.fieldName] = tfm
			break Outer
		}
		if tfl.isMap {
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
		tfl.isMap = true
		t = t.Elem()

	case reflect.Struct:
		debugPrintf("parseStruct() - %s is a struct", f.Name)
		if tfm.isCSV() || tfm.isSSV() {
			return nil, fmt.Errorf("cannot use a struct type with isSSV or isCSV")
		}
		if tfm.computedType != typeTextUnmarshaler {
			tfm.computedType = typeStruct
		}
		if tfm.isJSON() || tfl.isMap || tfl.isSlice || tfm.computedType == typeTextUnmarshaler {
			tfm.locators = []tFieldLocator{tfl}
			tm.tFieldsMetaMap[tfm.fieldName] = tfm
			break Outer
		}
		embedded, err := typeCache.tMeta(d, t, false)
		debugPrintf("parseStruct() - %s meta: %+v", f.Name, embedded)
		if err != nil {
			return nil, err
		}
		if tfm.isJSON() || tfl.isSlice || tfl.isMap {
			break Outer
		}
		locs := []tFieldLocator{tfl}
		for k, etfm := range embedded.tFieldsMetaMap {
			nk := path.Join(tfm.fieldName, k)
			debugPrintf("parseStruct() - nk: %s", nk)
			tm.tFieldsMetaMap[nk] = etfm
			tm.tFieldsMetaMap[nk].locators = append(locs, etfm.locators...)
		}

		break Outer
	case reflect.String,
		reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8,
		reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8,
		reflect.Float64, reflect.Float32, reflect.Bool:
		debugPrintf("parseStruct() - %s is a literal", f.Name)

		if tfm.computedType != typeTextUnmarshaler {
			if (tfm.isCSV() || tfm.isSSV()) && !tfl.isSlice {
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
		tfm.locators = []tFieldLocator{tfl}
		tm.tFieldsMetaMap[tfm.fieldName] = tfm

		break Outer
	default:
		debugPrintf("parseStruct() - %s is a an unknown type", f.Name)
		if tfm.computedType == typeTextUnmarshaler {
			tfm.locators = []tFieldLocator{tfl}
			tm.tFieldsMetaMap[tfm.fieldName] = tfm
		}
		break Outer
	}
}

func buildStructFieldMeta(d *Decoder, index int, f reflect.StructField) (*tFieldMeta, error) {
	tfm := &tFieldMeta{}

	var fullTag, fieldName, tagName string
	var tagBits []string
	var tagLen int

	tagLabel := defTag
	if d.Tag != "" {
		tagLabel = d.Tag
	}

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
		return nil, nil
	}

	if tagLen > 1 {
		for _, tv := range tagBits[1:] {
			switch tv {
			case tagJSON:
				tfm.special = sJSON
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

	tfl := tFieldLocator{
		ind:   index,
		ttype: f.Type,
	}

	return tfm, nil
}

func parseStruct(d *Decoder, st reflect.Type) (*tMeta, error) {
	if st.Kind() != reflect.Struct {
		return nil, fmt.Errorf("type is not struct: %s", st.Kind().String())
	}

	tm := &tMeta{tFieldsMetaMap: make(map[string]*tFieldMeta)}

	for i, numFields := 0, st.NumField(); i < numFields; i++ {
		f := st.Field(i)
		debugPrintf("parseStruct() - %s - %d - %s (%s)", st.Name(), i, f.Name, f.Type)

		// Skip unexported fields.  See
		// http://golang.org/pkg/reflect/#StructField for why this works.
		// also https://github.com/golang/go/issues/12367
		if f.PkgPath != "" && !f.Anonymous {
			continue
		}

		tfm, err := buildStructFieldMeta(d, i, f)
		if err != nil {
			return nil, err
		}

		debugPrintf("parseStruct() - Final TFM: %+v", tfm)
	}

	return tm, nil
}
