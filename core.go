package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/jmoiron/sqlx"
)

func copyRows(pgx *sqlx.Tx, tableName string, kind interface{}, unique string) (err error) {
	typ := reflect.TypeOf(kind)
	if typ.Kind() != reflect.Struct {
		return errors.New("kind given to copyRows is not a struct")
	}

	nfields := typ.NumField()
	valuelabels := make([]string, nfields)
	for i := 0; i < nfields; i++ {
		valuelabels[i] = ":" + typ.Field(i).Tag.Get("db")
	}

	rows, err := lite.Queryx(`SELECT * FROM ` + tableName)
	if err != nil {
		fmt.Println("error selecting "+tableName, err)
		return err
	}

	for rows.Next() {
		vpointer := reflect.New(typ).Interface()
		err := rows.StructScan(vpointer)
		if err != nil {
			fmt.Println(vpointer, "error scanning "+tableName+" row", err)
			return err
		}

		_, err = pgx.NamedExec(`
INSERT INTO `+tableName+`
VALUES (`+strings.Join(valuelabels, ",")+`)
ON CONFLICT (`+unique+`) DO NOTHING
            `, vpointer)
		if err != nil {
			fmt.Println(vpointer, "error inserting "+tableName, err)
			return err
		}
	}
	return nil
}

func setSequence(sequenceName string) (err error) {
	parts := strings.Split(sequenceName, "_")
	tableName := strings.Join(parts[0:len(parts)-2], "_")
	column := parts[len(parts)-2]

	var maxval int
	err = lite.Get(&maxval, `SELECT coalesce(max(`+column+`), 0) FROM `+tableName)
	if err != nil {
		fmt.Println("error fetching maximum value for", sequenceName, tableName, column, err)
		return
	}

	if maxval == 0 {
		// all is fine
		return
	}

	_, err = pg.Exec(`SELECT setval('`+sequenceName+`', $1)`, maxval)
	if err != nil {
		fmt.Println("error setting sequence", sequenceName, err)
		return
	}

	return nil
}
