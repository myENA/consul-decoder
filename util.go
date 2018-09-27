package decoder

import (
	"encoding/json"
	"fmt"
	"github.com/hashicorp/consul/api"
	"net"
	"reflect"
	"strconv"
	"time"
)

// Unmarshal - uses the default decoder with default settings to decode
// the values from kvps at pathPrefix into v.
func Unmarshal(pathPrefix string, kvps api.KVPairs, v interface{}) error {
	return defaultDecoder.Unmarshal(pathPrefix, kvps, v)
}

func defaultNameResolver(field, tag string) string {
	if tag != "" {
		return tag
	}
	return field
}

func typeKey(t reflect.Type) string {
	pp := t.PkgPath()
	pn := t.Name()
	if len(pp) > 0 && len(pn) > 0 {
		return pp + "." + pn
	}
	return pn
}

func isByteSlice(t reflect.Type) bool {
	k := t.Kind()
	if k != reflect.Slice {
		return false
	}
	t = t.Elem()
	return t.Kind() == reflect.Uint8
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

	case typeStruct:
		v := reflect.New(ttype)
		err := json.Unmarshal(data, v.Interface())
		tval = v.Elem()
		if err != nil {
			return tval, err
		}

	default:
		// TODO: mention this...
		//return tval, fmt.Errorf("no support for %s types in this context", ttype)
	}

	return tval, nil
}
