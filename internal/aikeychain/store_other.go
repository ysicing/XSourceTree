// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !darwin

package aikeychain

import (
	"errors"

	"github.com/egoist/mygo"
)

type Store struct{ Service string }

func (store Store) secrets() (mygo.SecretsModule, error) {
	if store.Service == "" {
		return mygo.SecretsModule{}, errors.New("系统凭证存储的服务名不能为空")
	}
	return mygo.Secrets.Service(store.Service), nil
}

func (store Store) Load(account string) (string, error) {
	secrets, err := store.secrets()
	if err != nil {
		return "", err
	}
	value, err := secrets.Get(account)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return "", nil
	}
	return string(value), err
}

func (store Store) Save(account, value string) error {
	secrets, err := store.secrets()
	if err != nil {
		return err
	}
	return secrets.Set(account, []byte(value))
}

func (store Store) Delete(account string) error {
	secrets, err := store.secrets()
	if err != nil {
		return err
	}
	return secrets.Delete(account)
}
