package decoder

import (
	"fmt"
	"reflect"
	"sync"
)

type (
	tFieldLocator struct {
		// ind is passed to type.Field()
		ind int

		// ptrCt is the number of pointers to the type
		ptrCt uint8

		// collPtrCt is the number of pointers on the other side
		// of the [] in a slice or a map[string] in a map
		collPtrCt uint8

		isSlice bool
		isMap   bool

		// The actual type of the thing, after all pointers
		// are de-ref'd.
		ttype reflect.Type
	}

	tFieldMeta struct {
		// locators gives us a way to locate
		// fields, even if they're nested several levels
		// deep within a structure.
		locators []tFieldLocator

		fieldName string

		// computedType distills the type that the locators refers to,
		// will be one of the type* constants defined above.
		computedType computedType

		special special
	}
)

func (tfm *tFieldMeta) isJSON() bool {
	return tfm.special == sJSON
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

type (
	tMeta struct {
		tFieldsMetaMap map[string]*tFieldMeta
	}

	typeCacheManager struct {
		lck             sync.RWMutex
		typeNameMetaMap map[string]*tMeta
	}
)

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
