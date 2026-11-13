// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package aikeychain

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type Store struct{ Service string }

var security struct {
	once             sync.Once
	err              error
	cf, sec          uintptr
	stringCreate     func(uintptr, *byte, int64, uint32, uint8) uintptr
	dataCreate       func(uintptr, *byte, int64) uintptr
	dataLength       func(uintptr) int64
	dataBytes        func(uintptr) unsafe.Pointer
	symbolPointer    func(uintptr, string) unsafe.Pointer
	dictionaryCreate func(uintptr, int64, uintptr, uintptr) uintptr
	dictionarySet    func(uintptr, uintptr, uintptr)
	release          func(uintptr)
	copyItem         func(uintptr, *uintptr) int32
	addItem          func(uintptr, *uintptr) int32
	updateItem       func(uintptr, uintptr) int32
	deleteItem       func(uintptr) int32
}

func initialize() error {
	security.once.Do(func() {
		security.cf, security.err = purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if security.err != nil {
			return
		}
		security.sec, security.err = purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if security.err != nil {
			return
		}
		purego.RegisterLibFunc(&security.stringCreate, security.cf, "CFStringCreateWithBytes")
		purego.RegisterLibFunc(&security.dataCreate, security.cf, "CFDataCreate")
		purego.RegisterLibFunc(&security.dataLength, security.cf, "CFDataGetLength")
		purego.RegisterLibFunc(&security.dataBytes, security.cf, "CFDataGetBytePtr")
		purego.RegisterLibFunc(&security.symbolPointer, purego.RTLD_DEFAULT, "dlsym")
		purego.RegisterLibFunc(&security.dictionaryCreate, security.cf, "CFDictionaryCreateMutable")
		purego.RegisterLibFunc(&security.dictionarySet, security.cf, "CFDictionarySetValue")
		purego.RegisterLibFunc(&security.release, security.cf, "CFRelease")
		purego.RegisterLibFunc(&security.copyItem, security.sec, "SecItemCopyMatching")
		purego.RegisterLibFunc(&security.addItem, security.sec, "SecItemAdd")
		purego.RegisterLibFunc(&security.updateItem, security.sec, "SecItemUpdate")
		purego.RegisterLibFunc(&security.deleteItem, security.sec, "SecItemDelete")
	})
	return security.err
}

func symbol(library uintptr, name string) uintptr {
	address, err := purego.Dlsym(library, name)
	if err != nil {
		panic(err)
	}
	return address
}

func constant(name string) uintptr {
	return *(*uintptr)(security.symbolPointer(security.sec, name))
}

func dictionary() uintptr {
	return security.dictionaryCreate(0, 0, symbol(security.cf, "kCFTypeDictionaryKeyCallBacks"), symbol(security.cf, "kCFTypeDictionaryValueCallBacks"))
}

func setString(query uintptr, name, value string) {
	data := []byte(value)
	text := security.stringCreate(0, unsafe.SliceData(data), int64(len(data)), 0x08000100, 0)
	defer security.release(text)
	security.dictionarySet(query, constant(name), text)
}

func (store Store) query(account string) (uintptr, error) {
	if err := initialize(); err != nil {
		return 0, errors.New("无法访问 macOS 钥匙串")
	}
	if store.Service == "" || account == "" {
		return 0, errors.New("钥匙串标识不能为空")
	}
	query := dictionary()
	security.dictionarySet(query, constant("kSecClass"), constant("kSecClassGenericPassword"))
	setString(query, "kSecAttrService", store.Service)
	setString(query, "kSecAttrAccount", account)
	return query, nil
}

func keychainError(status int32) error {
	return fmt.Errorf("钥匙串操作失败（OSStatus %d）", status)
}

func (store Store) Load(account string) (string, error) {
	query, err := store.query(account)
	if err != nil {
		return "", err
	}
	defer security.release(query)
	truth := *(*uintptr)(security.symbolPointer(security.cf, "kCFBooleanTrue"))
	security.dictionarySet(query, constant("kSecReturnData"), truth)
	security.dictionarySet(query, constant("kSecMatchLimit"), constant("kSecMatchLimitOne"))
	var result uintptr
	status := security.copyItem(query, &result)
	if status == -25300 {
		return "", nil
	}
	if status != 0 {
		return "", keychainError(status)
	}
	defer security.release(result)
	size := security.dataLength(result)
	if size < 0 || size > 65536 {
		return "", errors.New("钥匙串密钥长度无效")
	}
	return string(unsafe.Slice((*byte)(security.dataBytes(result)), int(size))), nil
}

func (store Store) Save(account, value string) error {
	query, err := store.query(account)
	if err != nil {
		return err
	}
	defer security.release(query)
	if value == "" || len(value) > 65536 {
		return errors.New("API 密钥长度无效")
	}
	bytes := []byte(value)
	data := security.dataCreate(0, unsafe.SliceData(bytes), int64(len(bytes)))
	defer security.release(data)
	values := dictionary()
	defer security.release(values)
	security.dictionarySet(values, constant("kSecValueData"), data)
	status := security.updateItem(query, values)
	if status == -25300 {
		security.dictionarySet(query, constant("kSecValueData"), data)
		status = security.addItem(query, nil)
	}
	if status != 0 {
		return keychainError(status)
	}
	return nil
}

func (store Store) Delete(account string) error {
	query, err := store.query(account)
	if err != nil {
		return err
	}
	defer security.release(query)
	status := security.deleteItem(query)
	if status != 0 && status != -25300 {
		return keychainError(status)
	}
	return nil
}
