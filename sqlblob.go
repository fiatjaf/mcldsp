package main

import (
	"database/sql/driver"
	"encoding/hex"
	"errors"
)

type sqlblob []byte

func (b *sqlblob) Scan(value interface{}) error {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		*b = sqlblob(v)
		return nil
	case string:
		*b = sqlblob(v)
		return nil
	default:
		return errors.New("sqlblob: value is not binary")
	}
}

func (b sqlblob) Value() (driver.Value, error) {
	if b == nil {
		return nil, nil
	}

	return []byte(b), nil
}

func (b sqlblob) String() string {
	hexvalue := hex.EncodeToString(b)
	return `\x` + hexvalue
}
