package template

// engine is what a template needs to know about one: the daemon's
// internal/datastore/engine.go is the original, and rules_test.go holds
// the two together.
type engine struct {
	name        string
	versions    []string // newest first
	stem        string
	port        int
	hasDatabase bool
	fixedUser   string
	refused     []string
}

var engines = []engine{
	{name: "postgres", versions: []string{"18", "17", "16", "15"}, stem: "DATABASE", port: 5432, hasDatabase: true},
	{name: "mysql", versions: []string{"8.4", "8.0"}, stem: "DATABASE", port: 3306, hasDatabase: true, refused: []string{"root"}},
	{name: "mariadb", versions: []string{"11.4", "10.11"}, stem: "DATABASE", port: 3306, hasDatabase: true, refused: []string{"root"}},
	{name: "redis", versions: []string{"7.4", "7.2"}, stem: "REDIS", port: 6379, fixedUser: "default"},
	{name: "mongodb", versions: []string{"8.0", "7.0"}, stem: "MONGO", port: 27017, hasDatabase: true},
}

func findEngine(name string) *engine {
	for i := range engines {
		if engines[i].name == name {
			return &engines[i]
		}
	}
	return nil
}

func databaseVarNames(e *engine, prefix string) []string {
	stem := prefix + e.stem
	names := []string{stem + "_URL", stem + "_HOST", stem + "_PORT", stem + "_USER", stem + "_PASSWORD"}
	if e.hasDatabase {
		names = append(names, stem+"_NAME")
	}
	return names
}

func storeVarNames(prefix string) []string {
	stem := prefix + "S3"
	return []string{stem + "_ENDPOINT", stem + "_REGION", stem + "_BUCKET",
		stem + "_ACCESS_KEY_ID", stem + "_SECRET_ACCESS_KEY", stem + "_PATH_STYLE"}
}
