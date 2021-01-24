package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/kr/pretty"
)

func copyRows(pgx *sqlx.Tx, tableName string, kind interface{}, unique string) (err error) {
	typ := reflect.TypeOf(kind)
	if typ.Kind() != reflect.Struct {
		return errors.New("kind given to copyRows is not a struct")
	}

	nfields := typ.NumField()
	columnnames := make([]string, nfields)
	valuelabels := make([]string, nfields)
	values := make([]interface{}, nfields)
	for i := 0; i < nfields; i++ {
		valuelabels[i] = fmt.Sprintf("$%d", i+1)
		columnnames[i] = typ.Field(i).Tag.Get("db")
	}

	rows, err := lite.Queryx(`SELECT * FROM ` + tableName)
	if err != nil {
		fmt.Println("error selecting "+tableName, err)
		return err
	}

	uniqueStmt := ""
	if unique != "" {
		uniqueStmt = `ON CONFLICT (` + unique + `) DO NOTHING`
	}

	for rows.Next() {
		vpointer := reflect.New(typ).Interface()
		err := rows.StructScan(vpointer)
		if err != nil {
			pretty.Log(vpointer)
			fmt.Println("error scanning "+tableName+" row", err)
			return err
		}

		for i := 0; i < nfields; i++ {
			values[i] = reflect.Indirect(reflect.ValueOf(vpointer)).Field(i).Interface()
		}

		_, err = pgx.Exec(`
INSERT INTO `+tableName+` (`+strings.Join(columnnames, ",")+`)
VALUES (`+strings.Join(valuelabels, ",")+`)
`+uniqueStmt,
			values...)
		if err != nil {
			pretty.Log(vpointer)
			pretty.Log(err)
			fmt.Println("error inserting on '" + tableName + "': " + err.Error())
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

	nextval := maxval + 1
	_, err = pg.Exec(`SELECT setval('`+sequenceName+`', $1)`, nextval)
	if err != nil {
		fmt.Println("error setting sequence", sequenceName, err)
		return
	}

	return nil
}
