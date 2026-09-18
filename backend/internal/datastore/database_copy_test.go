package datastore

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestCopyTablePreservesBinaryStorageAndTextComparisons(t *testing.T) {
	source, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	sourcePool, _ := source.DB()
	defer sourcePool.Close()
	target, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	targetPool, _ := target.DB()
	defer targetPool.Close()
	schema := "CREATE TABLE copy_probe (id INTEGER PRIMARY KEY, name TEXT, payload BLOB)"
	if err := source.Exec(schema).Error; err != nil {
		t.Fatal(err)
	}
	if err := target.Exec(schema).Error; err != nil {
		t.Fatal(err)
	}
	payload := []byte{0, 0xff, 0x80, 10, 42}
	if err := source.Exec("INSERT INTO copy_probe(id,name,payload) VALUES (1,?,?)", "文字", payload).Error; err != nil {
		t.Fatal(err)
	}
	if err := copyTableRows(source, target, "copy_probe"); err != nil {
		t.Fatal(err)
	}
	var row struct {
		Name    string
		Payload []byte
		Storage string
	}
	if err := target.Raw("SELECT name,payload,typeof(payload) AS storage FROM copy_probe WHERE name = ?", "文字").Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Name != "文字" || row.Storage != "blob" || !bytes.Equal(row.Payload, payload) {
		t.Fatal("copy changed text or binary representation", row)
	}
}

func TestCopyApplicationDataRejectsSourceInventoryFailure(t *testing.T) {
	source, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "closed-source.db"))
	if err != nil {
		t.Fatal(err)
	}
	sourcePool, _ := source.DB()
	if err := sourcePool.Close(); err != nil {
		t.Fatal(err)
	}
	target, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	targetPool, _ := target.DB()
	defer targetPool.Close()
	if err := CopyApplicationData(source, target); err == nil {
		t.Fatal("source inspection failure treated as missing legacy tables")
	}
}
