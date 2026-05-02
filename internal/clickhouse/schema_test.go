package clickhouse

import (
	"fmt"
	"strings"
	"testing"
)

// --- identifier validation ---

func TestBuildDDL_RejectsInvalidIdentifiers(t *testing.T) {
	cases := []string{
		"",
		"ion logs",
		"ion-logs",
		"1table",
		"'; DROP TABLE ion_logs; --",
		"ion_logs; DROP TABLE",
		"ion_logs/*comment*/",
	}
	for _, name := range cases {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("BuildDDL(%q) should panic on invalid identifier, but did not", name)
				}
			}()
			_ = BuildDDL(name)
		})
	}
}

func TestBuildDDL_AcceptsValidIdentifiers(t *testing.T) {
	for _, name := range []string{"ion_logs", "_private", "MyTable", "DB123", "db.table", "my_db.my_logs"} {
		t.Run(name, func(t *testing.T) {
			ddl := BuildDDL(name) // must not panic
			if !strings.Contains(ddl, name) {
				t.Errorf("DDL missing identifier %q", name)
			}
		})
	}
}
