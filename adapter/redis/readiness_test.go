package redis

import "testing"

func TestDatabaseOptionPreserved(t *testing.T) {
 if got := getOptions(&options{DB: 3}).DB; got != 3 { t.Fatalf("database = %d, want 3", got) }
}
