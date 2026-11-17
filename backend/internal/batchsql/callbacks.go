package batchsql

import (
	"errors"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

var installMu sync.Mutex

const statementStartKey = "zboard:batchsql:started"

type statementStart struct {
	collector *Collector
	at        time.Time
	kind      string
}

// Install is startup-only. Existing complete registrations are reused. Partial
// registrations remain unavailable rather than mutating active callback lists.
// Failure never changes db.Error or prevents business work from running.
func Install(db *gorm.DB) (installed bool) {
	defer func() {
		if recover() != nil {
			callbackFailures.Add(1)
			installed = false
		}
	}()
	if db == nil {
		return false
	}
	installMu.Lock()
	defer installMu.Unlock()
	checks := []bool{
		db.Callback().Query().Get("zboard:batchsql:before:query") != nil, db.Callback().Query().Get("zboard:batchsql:after:query") != nil,
		db.Callback().Row().Get("zboard:batchsql:before:row") != nil, db.Callback().Row().Get("zboard:batchsql:after:row") != nil,
		db.Callback().Raw().Get("zboard:batchsql:before:raw") != nil, db.Callback().Raw().Get("zboard:batchsql:after:raw") != nil,
		db.Callback().Create().Get("zboard:batchsql:before:create") != nil, db.Callback().Create().Get("zboard:batchsql:after:create") != nil,
		db.Callback().Update().Get("zboard:batchsql:before:update") != nil, db.Callback().Update().Get("zboard:batchsql:after:update") != nil,
		db.Callback().Delete().Get("zboard:batchsql:before:delete") != nil, db.Callback().Delete().Get("zboard:batchsql:after:delete") != nil,
	}
	found := 0
	for _, v := range checks {
		if v {
			found++
		}
	}
	if found != 0 {
		return found == len(checks)
	}
	// No map keyed by DB/subject and no callback registration per batch.
	registrations := []func() error{
		func() error {
			return db.Callback().Query().Before("gorm:query").Register("zboard:batchsql:before:query", before("query"))
		},
		func() error {
			return db.Callback().Query().After("gorm:query").Register("zboard:batchsql:after:query", after("query"))
		},
		func() error {
			return db.Callback().Row().Before("gorm:row").Register("zboard:batchsql:before:row", before("row"))
		},
		func() error {
			return db.Callback().Row().After("gorm:row").Register("zboard:batchsql:after:row", after("row"))
		},
		func() error {
			return db.Callback().Raw().Before("gorm:raw").Register("zboard:batchsql:before:raw", before("raw"))
		},
		func() error {
			return db.Callback().Raw().After("gorm:raw").Register("zboard:batchsql:after:raw", after("raw"))
		},
		func() error {
			return db.Callback().Create().Before("gorm:create").Register("zboard:batchsql:before:create", before("create"))
		},
		func() error {
			return db.Callback().Create().After("gorm:create").Register("zboard:batchsql:after:create", after("create"))
		},
		func() error {
			return db.Callback().Update().Before("gorm:update").Register("zboard:batchsql:before:update", before("update"))
		},
		func() error {
			return db.Callback().Update().After("gorm:update").Register("zboard:batchsql:after:update", after("update"))
		},
		func() error {
			return db.Callback().Delete().Before("gorm:delete").Register("zboard:batchsql:before:delete", before("delete"))
		},
		func() error {
			return db.Callback().Delete().After("gorm:delete").Register("zboard:batchsql:after:delete", after("delete"))
		},
	}
	for _, register := range registrations {
		if register() != nil {
			return false
		}
	}
	return true
}
func collector(tx *gorm.DB) *Collector {
	if tx == nil || tx.Statement == nil || tx.Statement.Context == nil {
		return nil
	}
	c, _ := tx.Statement.Context.Value(batchContextKey).(*Collector)
	return c
}
func before(kind string) func(*gorm.DB) {
	return func(tx *gorm.DB) {
		var c *Collector
		defer func() {
			if recover() != nil {
				callbackFailures.Add(1)
				if c != nil {
					c.invalidate()
				}
			}
		}()
		c = collector(tx)
		if c == nil {
			return
		}
		if tx.DryRun || tx.Error != nil {
			tx.Statement.Settings.Delete(statementStartKey)
			c.invalidate()
			return
		}
		if !c.start() {
			return
		}
		tx.Statement.Settings.Store(statementStartKey, statementStart{collector: c, at: time.Now(), kind: kind})
	}
}
func after(kind string) func(*gorm.DB) {
	return func(tx *gorm.DB) {
		var c *Collector
		defer func() {
			if recover() != nil {
				callbackFailures.Add(1)
				if c != nil {
					c.invalidate()
				}
			}
		}()
		c = collector(tx)
		if c == nil {
			return
		}
		if tx.DryRun {
			c.invalidate()
			return
		}
		value, ok := tx.Statement.Settings.LoadAndDelete(statementStartKey)
		started, valid := value.(statementStart)
		if !ok || !valid || started.collector != c || started.kind != kind {
			c.invalidate()
			return
		}
		count := Counts{Statements: 1, CallbackNS: uint64(max(time.Since(started.at), 0))}
		if tx.Statement.SQL.Len() == 0 {
			c.invalidate()
			c.observe(Counts{})
			return
		}
		write, read := false, false
		switch kind {
		case "query":
			read = true
		case "create", "update", "delete":
			write = true
		default:
			// Inspect at most32 bytes of the EXISTING unformatted SQL solely for its
			// operation keyword; never call Explain/logger.fc or retain text/values.
			sql := tx.Statement.SQL.String()
			if len(sql) > 32 {
				sql = sql[:32]
			}
			sql = strings.TrimLeft(sql, " \t\r\n")
			end := 0
			for end < len(sql) && ((sql[end] >= 'a' && sql[end] <= 'z') || (sql[end] >= 'A' && sql[end] <= 'Z')) {
				end++
			}
			word := sql[:end]
			write = strings.EqualFold(word, "INSERT") || strings.EqualFold(word, "UPDATE") || strings.EqualFold(word, "DELETE") || strings.EqualFold(word, "REPLACE")
			read = strings.EqualFold(word, "SELECT")
		}
		if write {
			count.Writes = 1
		} else if read {
			count.Reads = 1
		} else {
			count.UnknownStatements = 1
		}
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			count.ExpectedReadMisses = 1
		} else if tx.Error != nil {
			count.StatementErrors = 1
		}
		if kind == "row" {
			count.UnknownResults = 1
		}
		if write {
			if tx.RowsAffected < 0 || kind == "row" || tx.Error != nil {
				count.UnknownWriteRows = 1
			} else {
				count.ObservedAffectedWriteRows = uint64(tx.RowsAffected)
			}
			for _, argument := range tx.Statement.Vars {
				switch v := argument.(type) {
				case string:
					if !saturating(&count.TextBlobArgumentBytes, uint64(len(v))) {
						c.invalidate()
					}
				case []byte:
					if !saturating(&count.TextBlobArgumentBytes, uint64(len(v))) {
						c.invalidate()
					}
				default:
					if !saturating(&count.OtherArguments, 1) {
						c.invalidate()
					}
				}
			}
		}
		c.observe(count)
	}
}
