// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !darwin

package aikeychain

import "errors"

type Store struct{ Service string }

func (Store) Load(string) (string, error) {
	return "", errors.New("当前平台不支持 macOS 钥匙串")
}
func (Store) Save(string, string) error { return errors.New("当前平台不支持 macOS 钥匙串") }
func (Store) Delete(string) error       { return errors.New("当前平台不支持 macOS 钥匙串") }
