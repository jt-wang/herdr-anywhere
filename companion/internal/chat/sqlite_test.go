package chat

import "testing"

// SQLite reads a file: URI's authority as a host, so a Windows drive must come after a third slash.
func TestSQLiteFileURI(t *testing.T) {
	for in, want := range map[string]string{
		"/tmp/agent data/a.db":      "file:///tmp/agent%20data/a.db",
		"C:/agent state/a.db":       "file:///C:/agent%20state/a.db",
		"//server/share/state/a.db": "file:////server/share/state/a.db",
	} {
		if got := SQLiteFileURI(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}
